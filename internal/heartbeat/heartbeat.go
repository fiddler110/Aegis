// Package heartbeat carries the run's stall-detector beat targets on a context.
//
// It exists as a leaf package — stdlib only, imported by everything and
// importing nothing of ours — because the three parties to the beat sit on
// opposite sides of the dependency graph. internal/engine owns the detector;
// internal/tool/builtin's agent tool legitimately blocks for longer than the
// bound and must report the progress it observes; internal/provider blocks in
// the admission queue before it can produce any stream event at all. Any home
// inside one of those three would be an import cycle for another
// (internal/tool already imports internal/provider), which is why the plumbing
// is here rather than staying private to the engine (P66.8 / ARCH-04).
package heartbeat

import (
	"context"
	"sync"
	"time"
)

// ctxKey is the context key for the beat chain.
type ctxKey struct{}

// link is one element of that chain: a beat function plus whatever chain the
// context already carried when it was attached.
//
// The chain is the whole point. The engine's stall detector used to ride the
// context as a bare value under a key of its own, so a sub-agent's engine —
// which runs under a context derived from its parent's tool context —
// installed its own detector over the same key and shadowed the parent's. Every
// beat the child produced landed on the child's watch and none reached the
// parent, so a parallel fan-out or a debate looked, from the parent's side,
// exactly like a wedged backend: 900s of total silence, aborted as
// ErrTurnStalled, which every drive reset ladder declines as fatal. A
// legitimately long fan-out was diagnosed as the one thing a reset cannot fix.
//
// Chaining rather than shadowing gives the correct semantics: the parent turn
// *is* still making progress while a child is streaming tokens.
type link struct {
	fn   func()
	prev *link
}

// With returns a context on which Beat reports activity to fn, and to every
// beat target already attached to ctx. A nil fn is ignored so callers need no
// conditional.
func With(ctx context.Context, fn func()) context.Context {
	if fn == nil {
		return ctx
	}
	prev, _ := ctx.Value(ctxKey{}).(*link)
	return context.WithValue(ctx, ctxKey{}, &link{fn: fn, prev: prev})
}

// Beat reports observable activity to every beat target attached to ctx. A
// no-op when none is attached, which is the case for every unit test and for
// any caller that never installed a stall detector.
func Beat(ctx context.Context) {
	l, _ := ctx.Value(ctxKey{}).(*link)
	for ; l != nil; l = l.prev {
		l.fn()
	}
}

// Attached reports whether ctx carries any beat target. Callers use it to skip
// building a ticker they would have nothing to report to.
func Attached(ctx context.Context) bool {
	_, ok := ctx.Value(ctxKey{}).(*link)
	return ok
}

// While beats every interval until stop is called, and is the shape for a wait
// that is *known* to be alive but produces nothing observable — the admission
// queue is the one such wait in this codebase. It is deliberately not offered
// to callers who are waiting on something that might be wedged: a blind ticker
// there would blind the stall detector to exactly what it exists to catch.
//
// The returned stop is idempotent and must be deferred.
func While(ctx context.Context, interval time.Duration) (stop func()) {
	if !Attached(ctx) || interval <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				Beat(ctx)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
