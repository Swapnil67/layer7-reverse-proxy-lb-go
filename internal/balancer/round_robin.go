package balancer

// ! Round Robin Load Balancer

import (
	"layer7-reverse-proxy-lb-go/internal/core"
	"net/http"
	"net/url"
	"sync/atomic"
)

// * RoundRobinBalancer implements the core.LoadBalancer interface.
// * It uses an atomic counter to distribute client requests sequentially across healthy backends.
type RoundRobinBalancer struct {
	pool  *core.ServerPool
	index uint64 // * Accessed atomically: monotonically increasing tick counter
}

// * NewRoundRobinBalancer acts as a constructor initializing a RoundRobinBalancer.
func NewRoundRobinBalancer(pool *core.ServerPool) *RoundRobinBalancer {
	if pool == nil {
		pool = core.NewServerPool()
	}
	return &RoundRobinBalancer{
		pool: pool,
	}
}

// * Next selects the next available healthy backend using lock-free atomic cycling.
/*
* Since `index` is a `uint64`, its maximum value is `18,446,744,073,709,551,615` (18.4 quintillion).
* Even if your proxy processes 1 million requests per second, it would take over `584,000 years` for this counter to overflow!
 */
func (rr *RoundRobinBalancer) Next(r *http.Request) (*core.Backend, error) {
	backends := rr.pool.GetBackends()
	n := uint64(len(backends))
	if n == 0 {
		return nil, core.ErrNoAliveBackends
	}

	// * CPU hardware guarantees that every single request gets a completely unique number,
	// * even if millions of requests hit the proxy simultaneously Subtracting 1 simply shifts that
	// * unique number back to Go's 0-based array indexing.
	// * Atomically increment the index counter to get a unique tick number across goroutines
	nextIdx := atomic.AddUint64(&rr.index, 1) - 1

	// * Traversal loop: Try every backend starting from (nextIdx % n) until a healthy one is found
	for i := uint64(0); i < n; i++ {
		idx := (nextIdx + i) % n
		b := backends[idx]
		if b.IsAlive() {
			return b, nil
		}
	}

	// * All backends in the pool are currently marked unhealthy
	return nil, core.ErrNoAliveBackends
}

// * UpdateHealth updates the health status of a backend matching the target URL.
func (rr *RoundRobinBalancer) UpdateHealth(target *url.URL, alive bool) error {
	backends := rr.pool.GetBackends()
	targetStr := target.String()
	for _, b := range backends {
		if b.GetURL().String() == targetStr {
			b.SetAlive(alive)
			return nil
		}
	}
	return core.ErrBackendNotFound
}

// * Register delegates adding a backend to the server pool.
func (rr *RoundRobinBalancer) Register(b *core.Backend) error {
	return rr.pool.Add(b)
}

// * Unregister delegates removing a backend from the server pool.
func (rr *RoundRobinBalancer) Unregister(target *url.URL) error {
	return rr.pool.Remove(target)
}

// * GetBackends returns a snapshot copy of all registered backends.
func (rr *RoundRobinBalancer) GetBackends() []*core.Backend {
	return rr.pool.GetBackends()
}
