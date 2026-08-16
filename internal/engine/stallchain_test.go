package engine

import (
	"context"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/heartbeat"
)

// TestChildStallWatchDoesNotHideItsParent is the other half of P66.8 / ARCH-04.
//
// The timeout table in internal/tool/builtin bounds how long a single wait may
// last; this pins the mechanism that makes a long *composite* wait safe. A
// sub-agent's engine runs under a context derived from its parent's tool
// context and installs a stall watch of its own. When that was a bare
// context.WithValue on a private key, the child's value shadowed the parent's,
// beat() read only the innermost one, and every beat the child produced —
// every token it streamed, every tool it ran — landed on the child's watch.
// From the parent's side a healthy 40-minute fan-out was 40 minutes of silence,
// and at 900s it died as a fatal ErrTurnStalled: "the turn is hung, not slow",
// the one diagnosis a drive reset ladder refuses to recover from.
//
// The property is directional and both directions matter, so both are asserted:
// a child's beat must reach the parent, and the parent must not be the only
// recipient — the child still needs its own beats or its own stall detection
// stops working.
func TestChildStallWatchDoesNotHideItsParent(t *testing.T) {
	// Long limits: this test is about who receives a beat, not about timing.
	// Nothing here is allowed to fire.
	parent := &stallWatch{limit: time.Hour, last: time.Now(), stop: make(chan struct{})}
	child := &stallWatch{limit: time.Hour, last: time.Now(), stop: make(chan struct{})}

	parentCtx := withStallBeat(context.Background(), parent)
	childCtx := withStallBeat(parentCtx, child)

	// Rewind both watches so a beat is observable as a jump in `last`.
	rewind := func(s *stallWatch) time.Time {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.last = time.Now().Add(-30 * time.Minute)
		return s.last
	}
	lastOf := func(s *stallWatch) time.Time {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.last
	}

	pBefore, cBefore := rewind(parent), rewind(child)
	beat(childCtx)

	if !lastOf(parent).After(pBefore) {
		t.Error("a beat on the child's context did not reach the parent's watch — the child shadowed it, " +
			"so a fan-out or debate is silence to the parent run and dies as a fatal ErrTurnStalled")
	}
	if !lastOf(child).After(cBefore) {
		t.Error("a beat on the child's context did not reach the child's own watch — chaining must add a recipient, not replace one")
	}

	// The reverse must not hold: the parent's own context knows nothing about a
	// watch installed below it, or a child that outlives its parent's turn
	// would keep the parent's clock alive from outside the parent's turn.
	pBefore, cBefore = rewind(parent), rewind(child)
	beat(parentCtx)

	if !lastOf(parent).After(pBefore) {
		t.Error("a beat on the parent's own context did not reach the parent's watch")
	}
	if lastOf(child).After(cBefore) {
		t.Error("a beat on the parent's context reached a watch installed below it — the chain must run outward only")
	}

	// Disabled watches must not appear in the chain at all, so a run with
	// max_turn_stall: 0 nested under one with a bound stays disabled.
	off := &stallWatch{limit: 0}
	offCtx := withStallBeat(childCtx, off)
	if offCtx != childCtx {
		t.Error("a disabled stall watch was still attached to the context")
	}
}

// TestBeatWithoutAWatchIsANoop pins that the shared plumbing stays free for the
// overwhelmingly common caller: a unit test, or any engine built with
// max_turn_stall disabled, beats on every stream event and every tool edge.
func TestBeatWithoutAWatchIsANoop(t *testing.T) {
	ctx := context.Background()
	if heartbeat.Attached(ctx) {
		t.Fatal("a bare context reported a beat target")
	}
	beat(ctx) // must not panic
	if got := withStallBeat(ctx, nil); got != ctx {
		t.Error("a nil watch was attached to the context")
	}
}
