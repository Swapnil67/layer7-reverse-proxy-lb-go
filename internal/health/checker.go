package health

import (
	"fmt"
	"io"
	"layer7-reverse-proxy-lb-go/internal/core"
	"net/http"
	"sync"
	"time"
)

type HealthCheck struct {
	httpClient *http.Client
	ticker     *time.Ticker
	interval   time.Duration // * ticker interval for health check
	timeout    time.Duration // * http request timeout
	doneChan   chan struct{}
	pool       *core.ServerPool
}

func NewHealthCheck(pool *core.ServerPool, timeout time.Duration, interval time.Duration) *HealthCheck {
	return &HealthCheck{
		httpClient: &http.Client{Timeout: timeout},
		ticker:     time.NewTicker(interval),
		interval:   interval,
		timeout:    timeout,
		doneChan:   make(chan struct{}),
		pool:       pool,
	}
}

func (hc *HealthCheck) Start() {
	// * 1. Run an immediate check on startup
	hc.checkAll()

	// * 2. Loop on ticker intervals
	for {
		select {
		case <-hc.doneChan:
			return
		case <-hc.ticker.C:
			hc.checkAll()
		}
	}
}

// * checkAll retrieves all registered backends from the pool and triggers
// * concurrent health check pings across all backends simultaneously.
func (hc *HealthCheck) checkAll() {
	// * 1. Fetch a thread-safe snapshot slice of all registered backends.
	backends := hc.pool.GetBackends()
	if len(backends) == 0 {
		return // * Nothing to check if the pool is empty
	}

	// * 2. Instantiate a sync.WaitGroup to synchronize all background ping goroutines.
	var wg sync.WaitGroup

	// * 3. Iterate over each backend in the pool snapshot.
	for _, b := range backends {
		fmt.Println(b.GetURL().String())
		// * Increment the WaitGroup counter BEFORE launching the goroutine.
		wg.Add(1)

		// * Pass 'b' explicitly into the goroutine closure as a parameter.
		// * This guarantees that each goroutine pins its own unique backend pointer.
		go func(b *core.Backend) {
			// * Ensure wg.Done() runs when the goroutine exits to decrement the WaitGroup.
			defer wg.Done()

			url := b.GetURL().String()

			// * Send an HTTP GET request using our shared, timeout-configured client.
			resp, err := hc.httpClient.Get(url)
			if err != nil {
				// * Network error, connection refused, or timeout: mark backend dead.
				fmt.Printf("HEALTH CHECK ERROR: %s Ping failed: %v\n", url, err)
				b.SetAlive(false)
				return
			}

			// * Defer body closure ONLY after confirming 'err == nil' (resp is not nil).
			// * This prevents a nil-pointer panic and avoids leaking open TCP sockets.
			defer resp.Body.Close()

			// * Check if the backend responded with a non-200 status (e.g. 500, 503).
			if resp.StatusCode != http.StatusOK {
				fmt.Printf("HEALTH CHECK ERROR: %s Unhealthy status: %d\n", url, resp.StatusCode)
				b.SetAlive(false)
				return
			}

			// * Server returned 200 OK: mark backend healthy.
			b.SetAlive(true)

			// * Read and discard remaining body bytes to allow Go's HTTP client
			// * to reuse the underlying TCP connection (Keep-Alive) across checks.
			_, _ = io.Copy(io.Discard, resp.Body)
		}(b)
	}

	// * 4. Block until all concurrent backend ping goroutines complete.
	wg.Wait()
}

func (hc *HealthCheck) Stop() {
	hc.ticker.Stop()
	close(hc.doneChan)
}
