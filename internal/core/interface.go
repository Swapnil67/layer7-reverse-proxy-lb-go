package core

import (
	"net/http"
	"net/url"
	"sync"
)

// * LoadBalancer establishes the contract for all traffic routing algorithms.
// * Any strategy (Round Robin, Least Connections) must implement this interface.

type LoadBalancer interface {
	// * Next selects a healthy backend for an incoming client request.
	Next(r *http.Request) (*Backend, error)

	// * UpdateHealth modifies the health status of a target backend by URL string.
	UpdateHealth(target *url.URL, alive bool) error

	// * Register adds a new backend server to the operational pool.
	Register(b *Backend) error

	// * Unregister removes an existing backend server from the pool.
	Unregister(target *url.URL) error

	// * GetBackends returns a thread-safe snapshot of all registered backends.
	GetBackends() []*Backend
}

// * ServerPool provides shared thread-safe slice operations for backend management.
type ServerPool struct {
	mux      sync.RWMutex
	backends []*Backend
}

// * NewServerPool creates an empty, initialized ServerPool.
func NewServerPool() *ServerPool {
	return &ServerPool{
		backends: make([]*Backend, 0),
	}
}

// * Add appends a backend to the pool if it is not already present.
func (sp *ServerPool) Add(b *Backend) error {
	sp.mux.Lock()
	defer sp.mux.Unlock()

	for _, existing := range sp.backends {
		if existing.GetURL().String() == b.GetURL().String() {
			return ErrDuplicateBackend
		}
	}

	sp.backends = append(sp.backends, b)
	return nil
}

// * Remove deletes a backend from the pool by matching its target URL.
func (sp *ServerPool) Remove(target *url.URL) error {
	sp.mux.Lock()
	defer sp.mux.Unlock()

	targetUrl := target.String()
	idx := -1
	for i, existing := range sp.backends {
		if existing.GetURL().String() == targetUrl {
			idx = i
			break
		}
	}

	if idx == -1 {
		return ErrBackendNotFound
	}

	// * Delete maintaining order
	sp.backends = append(sp.backends[:idx], sp.backends[idx+1:]...)
	return nil
}

// * GetBackends returns a defensive shallow copy of the backend pool snapshot.
func (sp *ServerPool) GetBackends() []*Backend {
	sp.mux.RLock()
	defer sp.mux.RUnlock()

	snapshot := make([]*Backend, len(sp.backends))
	copy(snapshot, sp.backends)
	return snapshot
}
