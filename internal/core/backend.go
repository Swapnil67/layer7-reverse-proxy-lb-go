package core

import (
	"net/url"
	"sync/atomic"
)

// * Backend encapsulates the operational state of an upstream target server.

// * Fields are unexported to enforce thread-safe mutation via methods.
type Backend struct {
	url         *url.URL
	alive       uint32 // * Accessed atomically: 1 = alive, 0 = dead
	activeConns int64  // * Accessed atomically: current in-flight requests
}

// * NewBackend acts as a constructor initializing a thread-safe Backend entity.
func NewBackend(target *url.URL) *Backend {
	b := &Backend{
		url: target,
	}
	b.SetAlive(true) // * Default to alive upon registration
	return b
}

// * GetURL returns the parsed target URL of the upstream server.
func (b *Backend) GetURL() *url.URL {
	return b.url
}

// * SetAlive atomically updates the health status of the backend.
func (b *Backend) SetAlive(alive bool) {
	var v uint32
	if alive {
		v = 1
	}
	atomic.StoreUint32(&b.alive, v)
}

// * IsAlive atomically reads the health status of the backend.
func (b *Backend) IsAlive() bool {
	return atomic.LoadUint32(&b.alive) == 1
}

// * IncrActiveConns atomically increments the active in-flight request counter.
func (b *Backend) IncrActiveConns() int64 {
	return atomic.AddInt64(&b.activeConns, 1)
}

// * DecrActiveConns atomically decrements the active in-flight request counter.
func (b *Backend) DecrActiveConns() int64 {
	return atomic.AddInt64(&b.activeConns, -1)
}

// * GetActiveConns atomically retrieves the current in-flight request count.
func (b *Backend) GetActiveConns() int64 {
	return atomic.LoadInt64(&b.activeConns)
}
