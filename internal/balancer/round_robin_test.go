package balancer_test

import (
	"layer7-reverse-proxy-lb-go/internal/balancer"
	"layer7-reverse-proxy-lb-go/internal/core"
	"net/http"
	"net/url"
	"sync"
	"testing"
)

func TestRoundRobinBalancer_Next(t *testing.T) {
	u1, _ := url.Parse("http://localhost:8081")
	u2, _ := url.Parse("http://localhost:8082")
	u3, _ := url.Parse("http://localhost:8083")

	b1 := core.NewBackend(u1)
	b2 := core.NewBackend(u2)
	b3 := core.NewBackend(u3)

	pool := core.NewServerPool()
	_ = pool.Add(b1)
	_ = pool.Add(b2)
	_ = pool.Add(b3)

	lb := balancer.NewRoundRobinBalancer(pool)
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	// * 1. Validate exact 0-based sequential cycling (0 -> 1 -> 2 -> 0 -> 1 -> 2)
	expectedOrder := []*core.Backend{b1, b2, b3, b1, b2, b3}
	for i, expected := range expectedOrder {
		selected, err := lb.Next(req)
		if err != nil {
			t.Fatalf("Iteration %d: unexpected error: %v", i, err)
		}
		if selected.GetURL().String() != expected.GetURL().String() {
			t.Errorf("Iteration %d: expected %s, got %s", i, expected.GetURL().String(), selected.GetURL().String())
		}
	}
}

func TestRoundRobinBalancer_SkipDeadBackends(t *testing.T) {
	u1, _ := url.Parse("http://localhost:8081")
	u2, _ := url.Parse("http://localhost:8082")
	u3, _ := url.Parse("http://localhost:8083")

	b1 := core.NewBackend(u1)
	b2 := core.NewBackend(u2)
	b3 := core.NewBackend(u3)

	pool := core.NewServerPool()
	_ = pool.Add(b1)
	_ = pool.Add(b2)
	_ = pool.Add(b3)

	lb := balancer.NewRoundRobinBalancer(pool)
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	// * Mark b2 as dead
	b2.SetAlive(false)

	// * Expect b1 -> b3 -> b1 -> b3 (b2 skipped)
	expectedOrder := []*core.Backend{b1, b3, b1, b3}
	for i, expected := range expectedOrder {
		selected, err := lb.Next(req)
		if err != nil {
			t.Fatalf("Iteration %d: unexpected error: %v", i, err)
		}
		if selected.GetURL().String() != expected.GetURL().String() {
			t.Errorf("Iteration %d: expected %s, got %s", i, expected.GetURL().String(), selected.GetURL().String())
		}
	}
}

func TestRoundRobinBalancer_AllDeadOrEmpty(t *testing.T) {
	pool := core.NewServerPool()
	lb := balancer.NewRoundRobinBalancer(pool)
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	// * Empty pool check
	_, err := lb.Next(req)
	if err != core.ErrNoAliveBackends {
		t.Errorf("expected ErrNoAliveBackends for empty pool, got %v", err)
	}

	// * Pool with all dead backends
	u1, _ := url.Parse("http://localhost:8081")
	b1 := core.NewBackend(u1)
	b1.SetAlive(false)
	_ = pool.Add(b1)

	_, err = lb.Next(req)
	if err != core.ErrNoAliveBackends {
		t.Errorf("expected ErrNoAliveBackends for dead pool, got %v", err)
	}
}

func TestRoundRobinBalancer_ConcurrentAccess(t *testing.T) {
	u1, _ := url.Parse("http://localhost:8081")
	u2, _ := url.Parse("http://localhost:8082")
	// u3, _ := url.Parse("http://localhost:8083")

	b1 := core.NewBackend(u1)
	b2 := core.NewBackend(u2)
	// b3 := core.NewBackend(u3)

	pool := core.NewServerPool()
	_ = pool.Add(b1)
	_ = pool.Add(b2)
	// _ = pool.Add(b3)

	lb := balancer.NewRoundRobinBalancer(pool)
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	// * Spawn 100 parallel goroutines making 100 requests each to test atomic counter race safety
	const gorouties = 100
	const requestPerGoroutine = 100
	var wg sync.WaitGroup

	for i := 0; i < gorouties; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestPerGoroutine; j++ {
				selected, err := lb.Next(req)
				if err != nil || selected == nil {
					t.Errorf("concurrent Next() failed: %v", err)
				}
			}
		}()
	}
	wg.Wait()

}
