package profiling

import (
	"testing"
	"sync"
	"fmt"
)

func TestCircularBufferSerial(t *testing.T) {
	var size, i uint32
	var result []interface{}

	size = 1 << 10
	cb := NewCircularBuffer(size)
	if cb == nil {
		t.Errorf("expected circular buffer to be allocated, got nil")
		return
	}

	for i = 0; i < size/2; i++ {
		cb.Push(i)
	}

	result = cb.Drain()

	if uint32(len(result)) != size/2 {
		t.Errorf("expected result size %d, got %d", size/2, len(result))
		return
	}

	for i = 0; i < uint32(len(result)); i++ {
		if result[i] != i {
			t.Errorf("expected result[%d] to be %d, got %d", i, i, result[i])
			return
		}
	}

	for i = 0; i < size/2; i++ {
		cb.Push(size + i)
	}

	result = cb.Drain()

	if uint32(len(result)) != size/2 {
		t.Errorf("expected second push set drain size to be %d, got %d", size/2, len(result))
		return
	}

	for i = 0; i < uint32(len(result)); i++ {
		if result[i] != size + i {
			t.Errorf("expected result[%d] to be %d, got %d", i, size + i, result[i])
			return
		}
	}
}

func TestCircularBufferOverflow(t *testing.T) {
	var size, i, expected uint32
	var result []interface{}

	size = 1 << 10
	cb := NewCircularBuffer(size)
	if cb == nil {
		t.Errorf("expected circular buffer to be allocated, got nil")
		return
	}

	for i = 0; i < size + size/2; i++ {
		cb.Push(2*size + i)
	}

	result = cb.Drain()

	if uint32(len(result)) != size {
		t.Errorf("expected drain size to be a full %d, got %d", size, len(result))
		return
	}

	expected = 2*size + size/2
	for i = 0; i < uint32(len(result)); i++ {
		if result[i] != expected {
			t.Errorf("expected result[%d] to be %d, got %d", i, expected, result[i])
			return
		}
		expected++
	}
}

func TestCircularBufferConcurrent(t *testing.T) {
	for tn := 0; tn < 2; tn++ {
		var size uint32
		var result []interface{}

		size = 1 << 16
		cb := NewCircularBuffer(size)
		if cb == nil {
			t.Errorf("expected circular buffer to be allocated, got nil")
			return
		}

		type item struct {
			R uint32
			N uint32
		}

		var wg sync.WaitGroup
		for r := uint32(0); r < 32; r++ {
			wg.Add(1)
			go func(r uint32) {
				for n := uint32(0); n < size/4; n++ {
					cb.Push(item{R: r, N: n})
				}
				wg.Done()
			}(r)
		}

		// Wait for all goroutines to finish only in one test. Draining
		// concurrently while pushes are still happening will test for races in the
		// draining lock.
		if tn == 0 {
			wg.Wait()
		}

		result = cb.Drain()

		// Can't expect the buffer to be full if the pushes aren't necessarily done.
		if tn == 0 {
			if uint32(len(result)) != size {
				t.Errorf("expected drain size to be a full %d, got %d", size, len(result))
				return
			}
		}

		// Make sure the numbers for each goroutine are monotonically increasing and
		// consecutive. If not, there is likely some data race/corruption.
		lastSeen := make(map[uint32]uint32)
		for i := 0; i < len(result); i++ {
			elem := result[i].(item)
			if v, ok := lastSeen[elem.R]; ok {
				if elem.N != v + 1 {
					t.Errorf("tn = %d, R = %d: result[%d].N = %d != %d + 1", tn, elem.R, i, elem.N, v)
					for k := i - 10; k < i + 10; k++ {
						tx := result[k].(item)
						t.Errorf("%d: %v", k, tx)
					}
					return
				}
			}
			lastSeen[elem.R] = elem.N
		}

		// Wait for all goroutines to complete before moving on to other tests. If
		// the benchmarks run after this, it might affect performance unfairly.
		wg.Wait()
	}
}

func BenchmarkCircularBuffer(b *testing.B) {
	var size, i uint32

	for size = 1 << 10; size <= 1 << 16; size <<= 2 {
		cb := NewCircularBuffer(size)
		if cb == nil {
			b.Errorf("expected circular buffer to be allocated, got nil")
			return
		}

		type DummyStruct struct {
			Int int
			Str string
		}

		b.Run(fmt.Sprintf("size:%d", size), func(b *testing.B) {
			var x uint64 = 1
			for i = 0; i < uint32(b.N); i++ {
				cb.Push(x)
			}
		})
	}
}
