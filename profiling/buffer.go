package profiling

import (
	"sync"
	"sync/atomic"
	"unsafe"
	"runtime"
)

type circularBufferQueue struct {
	arr      []unsafe.Pointer
	size     uint32
	mask     uint32
	acquired uint32
	written  uint32
	drainingPostCheck uint32
}

// Allocates and returns a circularBufferQueue.
func NewCircularBufferQueue(size uint32) (*circularBufferQueue) {
	return &circularBufferQueue{
		arr: make([]unsafe.Pointer, size),
		size: size,
		mask: size - 1,
		acquired: 0,
		written: 0,
	}
}

// Used by the drainer to block till all pushes to the queue are complete
// before returns. This condition is not met as long as acquired != written.
func (q *circularBufferQueue) drainWait() {
	for atomic.LoadUint32(&q.acquired) != atomic.LoadUint32(&q.written) {
		runtime.Gosched()
		continue
	}
}

// A circular buffer can store up to size elements (the most recent size
// elements, to be specific). A fixed size buffer is used so that there are no
// allocation costs at runtime.
//
// size, mask and written are unsigned because we do some bitwise operations
// with them. 32 bits because it's more than sufficient; we're not going to
// store more than 4e9 elements in the circular buffer.
type CircularBuffer struct {
	mu sync.Mutex
	// TODO: multiple queues to decrease write contention on acquired and written?
	qs []*circularBufferQueue
	qc uint32
}

// Allocates a circular buffer of size size and returns a reference to the
// struct. Only circular buffers of size 2^k are allowed (saves us from having
// to do expensive modulo operations).
func NewCircularBuffer(size uint32) *CircularBuffer {
	if size & (size - 1) != 0 {
		return nil
	}

	return &CircularBuffer{
		qs: []*circularBufferQueue{
			NewCircularBufferQueue(size),
			NewCircularBufferQueue(size),
		},
	}
}

// Pushes an element in to the circular buffer.
func (cb *CircularBuffer) Push(x interface{}) {
	qc := atomic.LoadUint32(&cb.qc)
	q := cb.qs[qc]

	acquired := atomic.AddUint32(&q.acquired, 1) - 1

	if atomic.LoadUint32(&q.drainingPostCheck) > 0 {
		// Between our qc load and acquired increment, a drainer began execution
		// and switched the queues. This is NOT okay because we don't know if
		// acquired was incremented before or after the drainer's check for
		// acquired == writer. And we can't find this out without expensive
		// operations, which we'd like to avoid. If the acquired increment was
		// after, we cannot write to this buffer as the drainer's collection may
		// have already started; we must write to the other queue.
		//
		// Reverse our increment and retry. Since there's no SubUint32 in atomic,
		// ^uint32(0) is used to denote -1.
		// atomic.AddUint32(&q.acquired, ^uint32(0))
		return
	}

	// At this point, we're definitely writing to the right queue. Either no
	// drainer is in execution or is waiting at the acquired == written barrier.
	// TODO: mask only if acquired >= size?
	index := acquired & q.mask
	addr := &q.arr[index]
	old := atomic.LoadPointer(addr)

	// Even though we just verified that we haven't been wrapped around by
	// someone else, we cannot use a simple atomic store on the array
	// position because we may have been wrapped around between the acquired
	// check and the atomic store by someone else.
	//
	// As a result, we need a compare and swap operation to check that the
	// previously read value of the queue index is still the same. If the
	// compare and swap fails, we could either be the wrapper or the wrappee;
	// in either case, we'll simply retry the acquired check. Any push that has
	// been wrapped will fail that check and exit.
	//
	// This also makes the program safe in situations with more than two pushes
	// happening concurrently. For example, consider a situation where there
	// are three pushes A, B, C all in execution at the same time. Somehow, all
	// three get the same index with acquired being q.size apart. That is,
	// without any loss of generality, assume that:
	//
	//	 acquired_C = acquired_B + q.size = acquired_A + 2*q.size
	//
	// such that index is the same for all three. Let's say A and B complete
	// the first three steps; that is, item_A and item_B have been loaded
	// (let's call this value x0, denoting the value that the buffer slot held
	// before either push started execution). Now both pushes will check that
	// its acquired counter matches the queue's overall counter. Naturally, A
	// will fail since its acquired is at least q.size lower than the queue's
	// acquired counter. As a result, A will increment the written counter and
	// exit since the value it was about to write is stale anyway. At this
	// point, conventionally, B is considered to be the wrappee, and should be
	// allowed to proceed with a store at that position.
	//
	// But consider a situation where a push C begins execution just before
	// B's write to the slot happens. C completes every step up to and
	// including the step below successfully because it is the latest push.
	// If a regular atomic store was used instead of a compare-and-swap, B's
	// store would proceed successfully, producing incorrect output. The data
	// B was about to write to the buffer is now stale since it has been
	// superseded by a more up-to-date value (C). It should fail even though
	// it passed the acquired counter check. This is facilitated by a
	// compare-and-swap with B's previously read value in the buffer slot
	// (x0); the compare-and-swap will see that the slot no longer matches
	// the x0 value and will not swap the value. CompareAndSwap will return
	// false and B will need to retry its acquired counter check, which will
	// now fail, thanks to C's successful write. As a result, B will
	// correctly exit with a simple increment to the written counter without
	// touching the buffer itself.
	atomic.CompareAndSwapPointer(addr, old, unsafe.Pointer(&x))
	atomic.AddUint32(&q.written, 1)
}

// Switches the current queue for future pushes to proceed to the other queue
// so that there's no blocking. Assumes mutual exclusion across all drainers,
// however; this mutual exclusion is guaranteed by the mutex obtained by Drain
// at the start of execution.
//
// Returns a reference to the old queue.
func (cb *CircularBuffer) switchQueues() (*circularBufferQueue) {
	if !atomic.CompareAndSwapUint32(&cb.qc, 0, 1) {
		atomic.CompareAndSwapUint32(&cb.qc, 1, 0)
		return cb.qs[1]
	} else {
		return cb.qs[0]
	}
}

// Allocates and returns an array of things pushed in to the circular buffer.
func (cb *CircularBuffer) Drain() (result []interface{}) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	q := cb.switchQueues()
	q.drainWait()
	atomic.StoreUint32(&q.drainingPostCheck, 1)

	if q.written < q.size {
		result = make([]interface{}, q.written)
		for i := uint32(0); i < q.written; i++ {
			result[i] = *(*interface{})(q.arr[i])
		}
	} else {
		result = make([]interface{}, q.size)
		cur := q.written & q.mask
		j := uint32(0)
		for i := cur; i < q.size; i, j = i+1, j+1 {
			result[j] = *(*interface{})(q.arr[i])
		}
		for i := uint32(0); i < cur; i, j = i+1, j+1 {
			result[j] = *(*interface{})(q.arr[i])
		}
	}

	atomic.StoreUint32(&q.drainingPostCheck, 0)
	q.acquired = 0
	q.written = 0

	return
}
