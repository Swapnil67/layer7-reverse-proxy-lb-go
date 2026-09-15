package health_test

import (
	"layer7-reverse-proxy-lb-go/internal/core"
	"layer7-reverse-proxy-lb-go/internal/health"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthCheck_HealthyAndUnhealthyBackends(t *testing.T) {
	// * 1. Create a healthy backend (returns 200 OK)
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("PONG!"))
	}))
	healthyURL, _ := url.Parse(healthyServer.URL)

	// * 2. Create an unhealthy backend (returns 500 Internal Server Error)
	unHealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	unHealthyURL, _ := url.Parse(unHealthyServer.URL)

	b1 := core.NewBackend(healthyURL)   // * healthy upstream backend
	b2 := core.NewBackend(unHealthyURL) // * unhealthy upstream backend

	// * Set initial states opposite to what we expect
	b1.SetAlive(false) // * Should become true
	b2.SetAlive(true)  // * Should become false

	// * Create a pool of those upstream servers
	pool := core.NewServerPool()
	_ = pool.Add(b1)
	_ = pool.Add(b2)

	// * 3. Instantiate health check with short 50ms interval for fast testing
	hc := health.NewHealthCheck(pool, 1*time.Second, 50*time.Millisecond)

	// * Start background checker and ensure Stop() runs when test exits
	go hc.Start()

	// * 4. Give the health check goroutine time to run its first check tick
	time.Sleep(100 * time.Millisecond)

	// * 5. Assert backend alive states
	if !b1.IsAlive() {
		t.Errorf("expected healthy backend %s to be marked alive (true)", b1.GetURL().String())
	}

	if b2.IsAlive() {
		t.Errorf("expected unhealthy backend %s to be marked dead (false)", b2.GetURL().String())
	}

	// * Clean shutdown order: Stop health check goroutine FIRST, then close servers
	// ! prevents background goroutine teardown race condition
	defer hc.Stop()
	time.Sleep(10 * time.Millisecond) // * Allow in-flight checkAll() to return
	defer healthyServer.Close()
	defer unHealthyServer.Close()

}

func TestHealthCheck_BackendRecovery(t *testing.T) {
	// * 1. Declare isHealthy as an atomic boolean
	var isHealthy atomic.Bool

	// * Mock server that toggles from 500 Internal Error to 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isHealthy.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// * Create a backend struct from our server
	u, _ := url.Parse(server.URL)
	b := core.NewBackend(u)

	// * Create a pool of those upstream backend
	pool := core.NewServerPool()
	_ = pool.Add(b)

	hc := health.NewHealthCheck(pool, 1*time.Second, 30*time.Millisecond)
	go hc.Start()

	// * Wait for initial check (server should be marked dead)
	time.Sleep(50 * time.Millisecond)
	if b.IsAlive() {
		t.Errorf("expected backend to be marked dead initially")
	}

	// * Simulate server recovery
	isHealthy.Store(true)

	// * Wait for next ticker cycle
	time.Sleep(50 * time.Millisecond)
	if !b.IsAlive() {
		t.Errorf("expected backend to recover and be marked alive")
	}

	defer hc.Stop()
	time.Sleep(10 * time.Millisecond) // * Allow in-flight checkAll() to return
	defer server.Close()
}
