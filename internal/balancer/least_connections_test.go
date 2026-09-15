package balancer_test

import (
	"layer7-reverse-proxy-lb-go/internal/balancer"
	"layer7-reverse-proxy-lb-go/internal/core"
	"net/http"
	"net/url"
	"testing"
)

func TestLeastConnectionsBalancer_Selection(t *testing.T) {
	u1, _ := url.Parse("http://localhost:8081")
	u2, _ := url.Parse("http://localhost:8082")
	u3, _ := url.Parse("http://localhost:8083")

	b1 := core.NewBackend(u1)
	b2 := core.NewBackend(u2)
	b3 := core.NewBackend(u3)

	// * Simulate active workloads: b1=5, b2=2, b3=8
	for range 5 {
		b1.IncrActiveConns()
	}
	for range 2 {
		b2.IncrActiveConns()
	}
	for range 8 {
		b3.IncrActiveConns()
	}

	pool := core.NewServerPool()
	_ = pool.Add(b1)
	_ = pool.Add(b2)
	_ = pool.Add(b3)

	lb := balancer.NewLeastConnectionsBalancer(pool)
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	// * b2 has lowest active conns (2), should be selected
	selected, err := lb.Next(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.GetURL().String() != b2.GetURL().String() {
		t.Errorf("expected backend b2 (%s), got %s", b2.GetURL().String(), selected.GetURL().String())
	}

	for range 4 {
		b2.IncrActiveConns()
	}
	// * b2 has lowest active conns (2), should be selected
	selected, err = lb.Next(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.GetURL().String() != b1.GetURL().String() {
		t.Errorf("expected backend b1 (%s), got %s", b1.GetURL().String(), selected.GetURL().String())
	}
}

func TestLeastConnectionsBalancer_IgnoresDeadBackends(t *testing.T) {
	u1, _ := url.Parse("http://localhost:8081")
	u2, _ := url.Parse("http://localhost:8082")

	b1 := core.NewBackend(u1)
	b2 := core.NewBackend(u2)

	b1.SetAlive(false) // * Toggle this
	for range 5 {
		b2.IncrActiveConns()
	}

	pool := core.NewServerPool()
	_ = pool.Add(b1)
	_ = pool.Add(b2)

	lb := balancer.NewLeastConnectionsBalancer(pool)
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	// * b2 has lowest active conns (2), should be selected
	selected, err := lb.Next(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.GetURL().String() != b2.GetURL().String() {
		t.Errorf("expected backend b2 (%s), got %s", b2.GetURL().String(), selected.GetURL().String())
	}
}

// * go test -v -race ./...
