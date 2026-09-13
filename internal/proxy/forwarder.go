package proxy

import (
	"io"
	"layer7-reverse-proxy-lb-go/internal/core"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// * Standard Hop-by-hop headers specified in RFC 2616, section 13.5.1.
// * These headers belong to a single transport link and MUST NOT be forwarded by proxies.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te", // transfer-encodings the client is willing to accept.
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// * ProxyHandler implements http.Handler to manually rewrite client requests,
// * forward traffic to chosen backends, and stream responses back to clients.
type ProxyHandler struct {
	balancer   core.LoadBalancer
	transport  *http.Transport
	bufferPool *BufferPool
}

// * NewProxyHandler constructs a ProxyHandler using the LoadBalancer interface,
// * optional custom Transport settings, and a BufferPool.
func NewProxyHandler(lb core.LoadBalancer, transport *http.Transport, pool *BufferPool) *ProxyHandler {
	if transport == nil {
		// * Custom HTTP Transport tuned for high-concurrency connection pooling
		transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	if pool != nil {
		pool = NewBufferPool(32 * 1024) // * Default 32 KB byte buffer
	}

	return &ProxyHandler{
		balancer:   lb,
		transport:  transport,
		bufferPool: pool,
	}
}

// * ServeHTTP satisfies the standard http.Handler contract.
// * It handles the full proxy lifecycle.
func (ph *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// * 1. Fetch a healthy backend from LoadBalancer strategy
	backend, err := ph.balancer.Next(r)
	if err != nil {
		slog.Error("Failed to resolve healthy backend", "error", err)
		http.Error(w, "Service Unavailable: No healthy upstreams", http.StatusServiceUnavailable)
		return
	}

	// * 2. Atomically track active in-flight connections
	backend.IncrActiveConns()
	defer backend.DecrActiveConns()

	// * 3. Clone and rewrite incoming request for target backend
	outboundReq := ph.cloneRequest(r, backend.GetURL())

	// * 4. Dispatch request to upstream via tuned http.Transport
	resp, err := ph.transport.RoundTrip(outboundReq)
	if err != nil {
		slog.Error("Upstream round-trip failure", "target", backend.GetURL().String(), "error", err)
		// * Mark backend as unhealthy upon network dial failure
		_ = ph.balancer.UpdateHealth(backend.GetURL(), false)
		http.Error(w, "Bad Gateway: Upstream connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// * 5. Remove hop-by-hop headers from backend response
	ph.removeHopByHopHeaders(resp.Header)

	// * 6. Copy clean headers to client ResponseWriter
	ph.copyHeader(w.Header(), resp.Header)

	// * 7. Write HTTP status code to client
	w.WriteHeader(resp.StatusCode)

	// * 8. Stream response body using pre-allocated buffer from sync.Pool
	buf := ph.bufferPool.Get()
	defer ph.bufferPool.Put(buf) // * Return it back to the pool as soon as the response stream finishes

	// * io.CopyBuffer uses EXACT 32KB memory chunk to move bytes from backend -> client
	_, err = io.CopyBuffer(w, resp.Body, *buf)
	if err != nil && err != io.EOF {
		slog.Warn("Error streaming response body to client", "error", err)
	}

}

// * cloneRequest deep-copies *http.Request, updates target destination, removes
// * hop-by-hop headers, and appends proxy trace headers (X-Forwarded-*).
func (ph *ProxyHandler) cloneRequest(req *http.Request, targetURL *url.URL) *http.Request {
	outboundReq := req.Clone(req.Context()) // * Context Cancellation Propagation

	// * Target URL rewriting
	outboundReq.URL.Scheme = targetURL.Scheme
	outboundReq.URL.Host = targetURL.Host
	outboundReq.URL.Path = singleJoiningSlash(targetURL.Path, req.URL.Path)
	outboundReq.URL.RawQuery = req.URL.RawQuery
	outboundReq.Host = targetURL.Host // * Set Host header to match upstream target

	// * Remove hop-by-hop headers from outbound request
	ph.removeHopByHopHeaders(outboundReq.Header)

	// * Set standard X-Forwarded-* proxy headers
	clientIP, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		if prior := outboundReq.Header.Get("X-Forwarded-For"); prior != "" {
			clientIP = prior + ", " + clientIP
		}
		outboundReq.Header.Set("X-Forwarded-For", clientIP)
	}

	outboundReq.Header.Set("X-Forwarded-Host", req.Host)
	if req.TLS != nil {
		outboundReq.Header.Set("X-Forwarded-Proto", "https")
	} else {
		outboundReq.Header.Set("X-Forwarded-Proto", "http")
	}

	return outboundReq
}

// * removeHopByHopHeaders removes RFC hop-by-hop headers and any custom headers listed inside the Connection header value.
func (ph *ProxyHandler) removeHopByHopHeaders(header http.Header) {
	// * Parse custom hop-by-hop headers listed in "Connection" header value
	// * Connection: close, X-My-Proxy-Token, X-Forwarded-Internal
	if c := header.Get("Connection"); c != "" {
		for _, h := range strings.Split(c, ",") {
			if h = strings.TrimSpace(h); h != "" {
				header.Del(h)
			}
		}
	}

	// * Del standard RFC hop-by-hop headers
	for _, h := range hopByHopHeaders {
		header.Del(h)
	}
}

// * copyHeader copies HTTP headers from source to destination.
func (ph *ProxyHandler) copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// * singleJoiningSlash safely joins path segments while avoiding double slashes.
func singleJoiningSlash(a, b string) string {
	asSlash := strings.HasSuffix(a, "/")
	bsSlash := strings.HasPrefix(b, "/")
	switch {
	case asSlash && bsSlash:
		return a + b[1:]
	case !asSlash && !bsSlash:
		return a + "/" + b
	}
	return a + b
}
