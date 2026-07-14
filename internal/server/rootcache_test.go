package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRootCacheGetOrCreate(t *testing.T) {
	var c rootCache[int]
	var calls int32

	v, err := c.getOrCreate("/a", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 1, nil
	})
	if err != nil || v != 1 {
		t.Fatalf("got (%d, %v), want (1, nil)", v, err)
	}

	// Second call for the same root is a cache hit: create must not run again.
	v, err = c.getOrCreate("/a", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 999, nil
	})
	if err != nil || v != 1 {
		t.Fatalf("cache hit got (%d, %v), want (1, nil)", v, err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("create called %d times, want 1", got)
	}

	// A different root creates independently.
	v, err = c.getOrCreate("/b", func() (int, error) { return 2, nil })
	if err != nil || v != 2 {
		t.Fatalf("got (%d, %v), want (2, nil)", v, err)
	}
}

func TestRootCacheCreateErrorNotCached(t *testing.T) {
	var c rootCache[int]
	var calls int32

	_, err := c.getOrCreate("/x", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error")
	}

	v, err := c.getOrCreate("/x", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 7, nil
	})
	if err != nil || v != 7 {
		t.Fatalf("retry got (%d, %v), want (7, nil)", v, err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("create called %d times, want 2 (failed attempt not cached)", got)
	}
}

func TestRootCacheConcurrentCreateOnce(t *testing.T) {
	var c rootCache[int]
	var calls int32
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = c.getOrCreate("/same", func() (int, error) {
				atomic.AddInt32(&calls, 1)
				return 42, nil
			})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("create called %d times under concurrency, want 1", got)
	}
}

func TestRootCacheValues(t *testing.T) {
	var c rootCache[string]
	if len(c.values()) != 0 {
		t.Fatal("expected empty cache to have no values")
	}
	_, _ = c.getOrCreate("/a", func() (string, error) { return "a", nil })
	_, _ = c.getOrCreate("/b", func() (string, error) { return "b", nil })

	vals := c.values()
	if len(vals) != 2 {
		t.Fatalf("got %d values, want 2", len(vals))
	}
}
