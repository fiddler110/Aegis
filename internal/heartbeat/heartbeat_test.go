package heartbeat

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestChainBeatsOutward pins the property the whole package exists for: a beat
// reaches every target attached at or above the context it is issued on, and
// none attached below it. The engine's own test states this in stall-detector
// terms; this one states it in the mechanism's terms, so a change here fails
// here rather than three packages away.
func TestChainBeatsOutward(t *testing.T) {
	var outer, inner int

	outerCtx := With(context.Background(), func() { outer++ })
	innerCtx := With(outerCtx, func() { inner++ })

	Beat(innerCtx)
	if outer != 1 || inner != 1 {
		t.Errorf("a beat on the inner context reached outer=%d inner=%d, want 1 and 1 — the inner link shadowed the outer instead of chaining", outer, inner)
	}

	Beat(outerCtx)
	if outer != 2 {
		t.Errorf("a beat on the outer context did not reach its own target: outer=%d, want 2", outer)
	}
	if inner != 1 {
		t.Errorf("a beat on the outer context reached a target attached below it: inner=%d, want 1 — the chain must run outward only", inner)
	}
}

// TestNilAndBareContexts pins that the free path stays free: the overwhelming
// majority of Beat calls happen with nothing attached.
func TestNilAndBareContexts(t *testing.T) {
	ctx := context.Background()
	if Attached(ctx) {
		t.Error("a bare context reported a beat target")
	}
	Beat(ctx) // must not panic

	if got := With(ctx, nil); got != ctx {
		t.Error("a nil beat function was still attached to the context")
	}
}

// TestWhileBeatsUntilStopped covers the ticker used by the provider admission
// queue — the one wait that is known to be alive while producing nothing.
//
// It asserts three things, because only the first is obvious: that it beats
// while running, that it *stops* when told (a leaked ticker would keep a real
// run's stall clock alive forever, which is worse than the bug it fixes), and
// that stop is safe to call twice, since the caller's error path and its happy
// path both reach it.
func TestWhileBeatsUntilStopped(t *testing.T) {
	var beats atomic.Int64
	ctx := With(context.Background(), func() { beats.Add(1) })

	stop := While(ctx, 5*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for beats.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := beats.Load(); got < 3 {
		t.Fatalf("the ticker produced %d beats in 2s at a 5ms interval — it is not beating", got)
	}

	stop()
	stop() // idempotent: both the error and the success path call it

	// Nothing may arrive after stop. Sample rather than assert instantly, so a
	// ticker that merely slowed down is still caught.
	settled := beats.Load()
	time.Sleep(50 * time.Millisecond)
	if got := beats.Load(); got != settled {
		t.Errorf("%d beats arrived after stop() — the ticker leaked, and a leaked beat keeps a stalled run alive indefinitely", got-settled)
	}
}

// TestWhileIsFreeWithoutATarget pins that no goroutine starts when there is
// nothing to report to, which is every unit test and every run with
// max_turn_stall disabled.
func TestWhileIsFreeWithoutATarget(t *testing.T) {
	stop := While(context.Background(), time.Millisecond)
	stop()

	// A non-positive interval is a caller bug, not a busy loop.
	var beats atomic.Int64
	ctx := With(context.Background(), func() { beats.Add(1) })
	stop = While(ctx, 0)
	time.Sleep(20 * time.Millisecond)
	stop()
	if got := beats.Load(); got != 0 {
		t.Errorf("a non-positive interval produced %d beats", got)
	}
}

// TestConcurrentBeats runs the chain under -race the way the real one runs it:
// a parallel tool round beats from several goroutines at once, onto watches
// whose own beat() takes a mutex.
func TestConcurrentBeats(t *testing.T) {
	var mu sync.Mutex
	var n int
	ctx := With(context.Background(), func() {
		mu.Lock()
		n++
		mu.Unlock()
	})
	ctx = With(ctx, func() {
		mu.Lock()
		n++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Beat(ctx)
		}()
	}
	wg.Wait()

	if n != 100 {
		t.Errorf("50 concurrent beats over a 2-link chain produced %d calls, want 100", n)
	}
}
