package profiling

import (
	"testing"
	"sync"
	"fmt"
	"time"
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

		size = 1 << 6
		cb := NewCircularBuffer(size)
		if cb == nil {
			t.Errorf("expected circular buffer to be allocated, got nil")
			return
		}

		type item struct {
			R uint32
			N uint32
			T time.Time
		}

		var wg sync.WaitGroup
		for r := uint32(0); r < 1024; r++ {
			wg.Add(1)
			go func(r uint32) {
				for n := uint32(0); n < size; n++ {
					cb.Push(item{R: r, N: n, T: time.Now()})
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

		// Make sure the numbers for each goroutine are monotonically increasing. A
		// stronger requirement would be to enforce that they're consecutive, but
		// if there's enough contention and wrapping around in the circular buffer,
		// we cannot guarantee that the value wouldn't be overwritten by a later
		// value (correctly so).
		lastSeen := make(map[uint32]uint32)
		lastSeenId := make(map[uint32]uint32)
		for i := 0; i < len(result); i++ {
			elem := result[i].(item)
			if v, ok := lastSeen[elem.R]; ok {
				if elem.N != v + 1 {
					if elem.N <= v {
						t.Errorf("tn = %d, R = %d: result[%d].N = %d <= %d + 1", tn, elem.R, i, elem.N, v)
						t.Errorf("lastSeenId[%d] = %d", elem.R, lastSeenId[elem.R])
						t.Errorf("diff = %v %v", (result[0].(item)).T, (result[len(result)-1].(item)).T)
						for k := i - 10; k < i + 10; k++ {
							if k >= 0 && k < len(result) {
								tx := result[k].(item)
								t.Errorf("%d: %v", k, tx)
							}
						}
					} else {
						t.Logf("tn = %d, R = %d: result[%d].N = %d > %d, but not consecutive", tn, elem.R, i, elem.N, v)
						t.Logf("lastSeenId[%d] = %d", elem.R, lastSeenId[elem.R])
					}
					return
				}
			}
			lastSeen[elem.R] = elem.N
			lastSeenId[elem.R] = uint32(i)
		}

		// Wait for all goroutines to complete before moving on to other tests. If
		// the benchmarks run after this, it might affect performance unfairly.
		wg.Wait()
	}
}

func BenchmarkCircularBuffer(b *testing.B) {
	for size := 1 << 16; size <= 1 << 20; size <<= 1 {
		for routines := 1; routines <= 1 << 8; routines <<= 2 {
			cb := NewCircularBuffer(uint32(size))
			if cb == nil {
				b.Errorf("expected circular buffer to be allocated, got nil")
				return
			}

			b.Run(fmt.Sprintf("routines:%d/size:%d", routines, size), func(b *testing.B) {
				perRoutine := b.N / routines
				var wg sync.WaitGroup
				for r := 0; r < routines; r++ {
					wg.Add(1)
					go func() {
						for i := 0; i < perRoutine; i++ {
							cb.Push(i)
						}
						wg.Done()
					}()
				}
				wg.Wait()
			})
		}
	}
}
