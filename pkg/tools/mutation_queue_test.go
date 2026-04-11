package tools

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMutationQueueReturnsError(t *testing.T) {
	want := errors.New("boom")
	got := WithFileMutationQueue("/tmp/one", func() error { return want })
	if !errors.Is(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMutationQueueSerializesSamePath(t *testing.T) {
	var active int32
	var maxActive int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = WithFileMutationQueue("/tmp/serial", func() error {
				n := atomic.AddInt32(&active, 1)
				for {
					m := atomic.LoadInt32(&maxActive)
					if n <= m || atomic.CompareAndSwapInt32(&maxActive, m, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return nil
			})
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&maxActive) > 1 {
		t.Errorf("saw concurrent execution (max active = %d)", maxActive)
	}
}

func TestMutationQueueDifferentPathsRunConcurrently(t *testing.T) {
	var active int32
	var maxActive int32
	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := "/tmp/parallel-" + string(rune('a'+i))
			_ = WithFileMutationQueue(path, func() error {
				n := atomic.AddInt32(&active, 1)
				for {
					m := atomic.LoadInt32(&maxActive)
					if n <= m || atomic.CompareAndSwapInt32(&maxActive, m, n) {
						break
					}
				}
				time.Sleep(15 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return nil
			})
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&maxActive) < 2 {
		t.Errorf("different paths should run concurrently (max active = %d)", maxActive)
	}
}
