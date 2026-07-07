package swarm

import (
	"context"
	"sync"
	"time"
)

// AdaptiveLimiterFloor is the minimum concurrent-spawn cap an AdaptiveLimiter
// will ever throttle down to (P17, explicit request 2026-07-07). A 'parallel'
// workflow call should never be forced fully serial just because a recent
// batch looked saturated.
const AdaptiveLimiterFloor = 2

// AdaptiveLimiter bounds how many sub-agents may run concurrently under a
// single 'parallel' workflow dispatch, starting conservative and adjusting
// from observed batch behavior (an AIMD scheme driven by measured wall-clock
// speedup) rather than from static config or host/hardware introspection —
// see research/roadmap.md P17 for the rationale. One instance is shared
// process-wide by the daemon's agent tool; it is in-memory only and does not
// persist across restarts.
type AdaptiveLimiter struct {
	mu      sync.Mutex
	cond    *sync.Cond
	inUse   int
	cap     int
	ceiling int
}

// NewAdaptiveLimiter builds a limiter starting at the floor (2) with the
// given ceiling (the hard fan-out cap on how many agents one call may
// request, e.g. maxParallelAgents).
func NewAdaptiveLimiter(ceiling int) *AdaptiveLimiter {
	l := &AdaptiveLimiter{cap: AdaptiveLimiterFloor, ceiling: ceiling}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// Cap returns the current effective concurrency cap.
func (l *AdaptiveLimiter) Cap() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cap
}

// Acquire blocks until a concurrency slot is available (or ctx is done) and
// takes it. Every successful Acquire must be paired with a Release.
func (l *AdaptiveLimiter) Acquire(ctx context.Context) error {
	// sync.Cond.Wait has no ctx support; a watcher goroutine broadcasts on
	// cancellation so a blocked Acquire wakes up and observes ctx.Err().
	stop := make(chan struct{})
	if done := ctx.Done(); done != nil {
		go func() {
			select {
			case <-done:
				l.mu.Lock()
				l.cond.Broadcast()
				l.mu.Unlock()
			case <-stop:
			}
		}()
		defer close(stop)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for l.inUse >= l.cap {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.inUse++
	return nil
}

// Release frees a concurrency slot taken by Acquire.
func (l *AdaptiveLimiter) Release() {
	l.mu.Lock()
	l.inUse--
	l.cond.Broadcast()
	l.mu.Unlock()
}

// RecordBatch reports the outcome of one completed 'parallel' batch of n
// agents: sumIndividual is the sum of each agent's own spawn-to-completion
// duration, wallClock is the elapsed time for the whole batch. The ratio
// approximates how much the host actually overlapped the requests:
//   - speedup ≈ n means the host truly ran them concurrently — raise the cap
//     by 1 (additive increase), up to the ceiling.
//   - speedup ≈ 1 means the host serialized them regardless of n — halve the
//     cap (multiplicative decrease), down to the floor.
//
// Batches that didn't stress the current cap (n < current cap) carry no
// signal either way and are ignored, since a small batch finishing fast says
// nothing about whether a larger one would have contended.
func (l *AdaptiveLimiter) RecordBatch(n int, sumIndividual, wallClock time.Duration) {
	if wallClock <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n < l.cap {
		return
	}
	speedup := float64(sumIndividual) / float64(wallClock)
	// Threshold at the midpoint between fully serialized (speedup 1) and
	// fully concurrent (speedup n), rather than a fraction of n directly —
	// a fraction like n/2 degenerates at small n (n=2 gives a threshold of
	// 1, indistinguishable from a fully serialized batch).
	threshold := (1 + float64(n)) / 2
	switch {
	case speedup >= threshold:
		l.raiseLocked()
	default:
		l.lowerLocked()
	}
}

// RecordExhaustion reports a spawn failure consistent with resource
// exhaustion (timeout, connection refused) during a parallel batch — treated
// the same as an observed low-speedup batch: multiplicative decrease.
func (l *AdaptiveLimiter) RecordExhaustion() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lowerLocked()
}

func (l *AdaptiveLimiter) raiseLocked() {
	if l.cap < l.ceiling {
		l.cap++
		l.cond.Broadcast()
	}
}

func (l *AdaptiveLimiter) lowerLocked() {
	l.cap = max(l.cap/2, AdaptiveLimiterFloor)
}
