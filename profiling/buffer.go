package profiling

import (
	"sync/atomic"
	"sync"
	"unsafe"
)

// A circular buffer can store up to size elements (the most recent size
// elements, to be specific). A fixed size buffer is used so that there are no
// allocation costs at runtime.
//
// size, mask and written are unsigned because we do some bitwise operations
// with them. 32 bits because it's more than sufficient; we're not going to
// store more than 4e9 elements in the circular buffer.
type CircularBuffer struct {
	size uint32
	mask uint32
	written uint32
	arr []interface{}
	draining sync.RWMutex
}

// Allocates the memory necessary for a circular buffer of size size and returns
// the a reference to the struct. Only circular buffers of size 2^k are
// allowed (saves us from having to do an expensive modulo operation).
func NewCircularBuffer(size uint32) *CircularBuffer {
	if size & (size - 1) != 0 {
		return nil
	}

	return &CircularBuffer{
		size: size,
		mask: size - 1,
		written: 0,
		arr: make([]interface{}, size),
	}
}

// Pushes an element in to the circular buffer.
func (cb *CircularBuffer) Push(x interface{}) {
	cb.draining.RLock()

	cur := (atomic.AddUint32(&cb.written, 1) - 1) & cb.mask
	cb.arr[cur] = atomic.

	// Do not defer RUnlock for better performance. Saves the cost of pushing and
	// popping things on to and from the stack. On testing, deferring the unlock
	// cost an additional 15ns/op.
	cb.draining.RUnlock()
}


// Allocates and returns an array of things pushed in to the circular buffer.
func (cb *CircularBuffer) Drain() []interface{} {
	cb.draining.Lock()

	var result []interface{}
	if cb.written < cb.size {
		result = make([]interface{}, cb.written)
		copy(result, cb.arr)
	} else {
		result = make([]interface{}, cb.size)
		cur := cb.written & cb.mask
		diff := cb.size - cur
		copy(result[:diff], cb.arr[cur:])
		copy(result[diff:], cb.arr[:cur])
	}

	cb.written = 0

	defer cb.draining.Unlock()
	return result
}
