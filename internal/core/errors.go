package core

import "errors"

// * Domain-specific sentinel errors for upstream server pool operations.
var (
	// * ErrNoAliveBackends is returned when all registered backends fail health checks.
	ErrNoAliveBackends = errors.New("core: no alive backends available in pool")

	// * ErrBackendNotFound is returned when attempting to update or unregister a non-existent URL.
	ErrBackendNotFound = errors.New("core: backend target URL not found")

	// * ErrDuplicateBackend is returned when registering a target URL that already exists in the pool.
	ErrDuplicateBackend = errors.New("core: backend target URL is already registered")
)
