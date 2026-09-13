package proxy

import "sync"

// * A thread-safe byte buffer pool to recycle fixed-size memory slices during response body streaming.

// * BufferPool encapsulates a sync.Pool of byte slices used for zero-allocation
// * response streaming across concurrent client requests.
type BufferPool struct {
	pool       sync.Pool
	bufferSize int
}

// * NewBufferPool initializes a BufferPool with a designated slice size (e.g., 32KB).
func NewBufferPool(bufferSize int) *BufferPool {
	return &BufferPool{
		bufferSize: bufferSize,
		pool: sync.Pool{
			New: func() any {
				// * Allocate a fixed-size byte slice pointer on heap
				b := make([]byte, bufferSize)
				return &b
			},
		},
	}
}

// * Get retrieves a pre-allocated byte slice pointer from the pool.
func (bp *BufferPool) Get() *[]byte {
	return bp.pool.Get().(*[]byte)
}

// * Put cleans and returns a byte slice pointer back to the pool for reuse.
func (bp *BufferPool) Put(b *[]byte) {
	if b == nil || len(*b) != bp.bufferSize {
		return
	}
	bp.pool.Put(b)
}
