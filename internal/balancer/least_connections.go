package balancer

// ! Strategy Pattern
// ! Least Connections Load Balancer

import (
	"layer7-reverse-proxy-lb-go/internal/core"
	"net/http"
	"net/url"
)

// * LeastConnectionsBalancer implements the core.LoadBalancer interface.
// * It dynamically routes incoming HTTP requests to the healthy backend
// * currently processing the fewest active in-flight requests.
type LeastConnectionsBalancer struct {
	pool *core.ServerPool
}

// * NewLeastConnectionsBalancer constructs a LeastConnectionsBalancer instance.
func NewLeastConnectionsBalancer(pool *core.ServerPool) *LeastConnectionsBalancer {
	if pool == nil {
		pool = core.NewServerPool()
	}
	return &LeastConnectionsBalancer{
		pool: pool,
	}
}

// * Next inspects all registered backends and returns the healthy backend
// * with the lowest active connection count.
func (lc *LeastConnectionsBalancer) Next(r *http.Request) (*core.Backend, error) {
	backends := lc.pool.GetBackends()
	if len(backends) == 0 {
		return nil, core.ErrNoAliveBackends
	}

	var leastBackend *core.Backend
	var minConns int64 = -1

	// * Iterate through pool snapshot to locate the healthiest backend with minimum activeConns
	for _, b := range backends {
		if !b.IsAlive() {
			continue // * Skip dead backends
		}

		conns := b.GetActiveConns() // * Lock-free atomic load
		if minConns == -1 || conns < minConns {
			minConns = conns
			leastBackend = b
		}
	}

	if leastBackend == nil {
		return nil, core.ErrNoAliveBackends
	}

	return leastBackend, nil
}

// * UpdateHealth updates the health status of a target backend by matching its URL string.
func (lc *LeastConnectionsBalancer) UpdateHealth(target *url.URL, alive bool) error {
	backends := lc.pool.GetBackends()
	targetStr := target.String()
	for _, b := range backends {
		if b.GetURL().String() == targetStr {
			b.SetAlive(alive)
			return nil
		}
	}
	return core.ErrBackendNotFound
}

// * Register delegates backend registration to the thread-safe ServerPool.
func (lc *LeastConnectionsBalancer) Register(b *core.Backend) error {
	return lc.pool.Add(b)
}

// * Unregister delegates backend removal to the thread-safe ServerPool.
func (lc *LeastConnectionsBalancer) Unregister(target *url.URL) error {
	return lc.pool.Remove(target)
}

// * GetBackends returns a defensive snapshot copy of all registered backends.
func (lc *LeastConnectionsBalancer) GetBackends() []*core.Backend {
	return lc.pool.GetBackends()
}
