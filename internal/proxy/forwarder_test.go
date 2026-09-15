package proxy_test

import (
	"io"
	"layer7-reverse-proxy-lb-go/internal/balancer"
	"layer7-reverse-proxy-lb-go/internal/core"
	"layer7-reverse-proxy-lb-go/internal/proxy"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestProxyHandler_EndToEndForwarding(t *testing.T) {
	// * 1. Create a mock backend HTTP server using httptest.Server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// * Assert RFC hop-by-hop headers were stripped
		if r.Header.Get("Connection") != "" {
			t.Errorf("expected Connection header to be stripped, got %s", r.Header.Get("Connection"))
		}

		// * Assert X-Forwarded-For was injected
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Error("expected X-Forwarded-For header to be injected")
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello from upstream backend!"))
	}))

	defer backendServer.Close()

	backendURL, _ := url.Parse(backendServer.URL)
	backend := core.NewBackend(backendURL)

	pool := core.NewServerPool()
	_ = pool.Add(backend)

	lb := balancer.NewRoundRobinBalancer(pool)

	proxyHandler := proxy.NewProxyHandler(lb, nil, nil)

	// * 2. Simulate client request hitting the ProxyHandler
	req := httptest.NewRequest("GET", "http://localhost:8080/api/v1/test", nil)
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("User-Agent", "Go-Proxy-Test")

	rec := httptest.NewRecorder()
	proxyHandler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	// * 3. Verify response status code and streamed body payload
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 OK, got (%d)", res.StatusCode)
	}

	body, _ := io.ReadAll(res.Body)
	if string(body) != "Hello from upstream backend!" {
		t.Errorf("expected body 'Hello from upstream backend!', got '%s'", string(body))
	}
}

func TestBufferPool_GetAndPut(t *testing.T) {
	bp := proxy.NewBufferPool(32 * 1024)

	buf := bp.Get()
	if buf == nil || len(*buf) != 32*1024 {
		t.Fatalf("expected 32KB byte slice from BufferPool")
	}

	// * Put valid buffer back
	bp.Put(buf)

	// * Put invalid nil or wrong size slice (should be safely ignored by guard clause)
	bp.Put(nil)
	wrongSize := make([]byte, 1024)
	bp.Put(&wrongSize)
}
