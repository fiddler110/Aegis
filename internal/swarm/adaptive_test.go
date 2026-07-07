package swarm

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveLimiterStartsAtFloor(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	if got := l.Cap(); got != AdaptiveLimiterFloor {
		t.Errorf("starting cap = %d, want %d", got, AdaptiveLimiterFloor)
	}
}

func TestAdaptiveLimiterRaisesOnHighSpeedup(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	// n == cap (2), sumIndividual ≈ n * wallClock -> true concurrency.
	l.RecordBatch(2, 200*time.Millisecond, 100*time.Millisecond)
	if got := l.Cap(); got != AdaptiveLimiterFloor+1 {
		t.Errorf("cap after high-speedup batch = %d, want %d", got, AdaptiveLimiterFloor+1)
	}
}

func TestAdaptiveLimiterRaisesRepeatedlyUpToCeiling(t *testing.T) {
	l := NewAdaptiveLimiter(4)
	for i := 0; i < 10; i++ {
		n := l.Cap()
		l.RecordBatch(n, time.Duration(n)*100*time.Millisecond, 100*time.Millisecond)
	}
	if got := l.Cap(); got != 4 {
		t.Errorf("cap = %d, want clamped to ceiling 4", got)
	}
}

func TestAdaptiveLimiterLowersOnLowSpeedup(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	for i := 0; i < 5; i++ {
		n := l.Cap()
		// speedup == n: a genuinely concurrent batch, always raises.
		l.RecordBatch(n, time.Duration(n)*100*time.Millisecond, 100*time.Millisecond)
	}
	got := l.Cap()
	if got != AdaptiveLimiterFloor+5 {
		t.Fatalf("setup: cap = %d, want %d after 5 raises from floor 2", got, AdaptiveLimiterFloor+5)
	}
	// speedup ≈ 1 (serialized): sumIndividual ≈ wallClock despite n == cap.
	l.RecordBatch(got, 100*time.Millisecond, 100*time.Millisecond)
	if newCap := l.Cap(); newCap != got/2 {
		t.Errorf("cap after low-speedup batch = %d, want %d (halved)", newCap, got/2)
	}
}

func TestAdaptiveLimiterLowerClampsToFloor(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	l.RecordBatch(2, 100*time.Millisecond, 100*time.Millisecond)
	if got := l.Cap(); got != AdaptiveLimiterFloor {
		t.Errorf("cap = %d, want floor %d (already at floor, must not drop below it)", got, AdaptiveLimiterFloor)
	}
}

func TestAdaptiveLimiterIgnoresBatchSmallerThanCap(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	l.RecordBatch(2, 200*time.Millisecond, 100*time.Millisecond) // speedup 2, cap 2 -> 3
	before := l.Cap()
	// n(1) < cap(3): must be ignored regardless of the (here, low) speedup.
	l.RecordBatch(1, 100*time.Millisecond, 100*time.Millisecond)
	if got := l.Cap(); got != before {
		t.Errorf("cap changed on a batch smaller than the cap: %d -> %d", before, got)
	}
}

func TestAdaptiveLimiterRecordExhaustionLowers(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	for i := 0; i < 5; i++ {
		n := l.Cap()
		l.RecordBatch(n, time.Duration(n)*100*time.Millisecond, 100*time.Millisecond)
	}
	before := l.Cap()
	l.RecordExhaustion()
	if got := l.Cap(); got != before/2 {
		t.Errorf("cap after exhaustion = %d, want %d (halved)", got, before/2)
	}
}

func TestAdaptiveLimiterIgnoresZeroWallClock(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	l.RecordBatch(2, 100*time.Millisecond, 0)
	if got := l.Cap(); got != AdaptiveLimiterFloor {
		t.Errorf("cap changed on a zero-wall-clock batch: %d", got)
	}
}

// TestAdaptiveLimiterGatesConcurrency verifies Acquire actually blocks once
// the cap is reached, using channel synchronization (not sleeps) so the
// assertion is deterministic regardless of scheduling speed.
func TestAdaptiveLimiterGatesConcurrency(t *testing.T) {
	l := NewAdaptiveLimiter(8) // starting cap: floor 2

	acquired := make(chan struct{}, 3)
	release := make(chan struct{})
	done := make(chan struct{}, 3)

	for i := 0; i < 3; i++ {
		go func() {
			if err := l.Acquire(context.Background()); err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			acquired <- struct{}{}
			<-release
			l.Release()
			done <- struct{}{}
		}()
	}

	// Exactly 2 (the floor cap) should be able to acquire immediately.
	<-acquired
	<-acquired
	select {
	case <-acquired:
		t.Fatal("a third goroutine acquired a slot while cap was 2")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	<-acquired // the third unblocks once a slot frees up
	<-done
	<-done
	<-done
}

func TestAdaptiveLimiterAcquireRespectsCtxCancellation(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	// Cap (2) is now fully held; a third Acquire on a cancelled ctx must
	// return promptly instead of blocking forever.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Acquire(ctx); err == nil {
		t.Error("Acquire on cancelled ctx returned nil error, want ctx.Err()")
	}
}
