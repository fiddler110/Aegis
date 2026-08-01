package drive

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// newTestPhaseState returns a State wired for the pure decision
// helpers (recoverPhase6Overflow / tryEscalateWindow): a discard errOut, a real
// logger, and an escalateWindow that records how many times it was invoked.
func newTestPhaseState(escalate func() (int, bool)) *State {
	return &State{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		ErrOut:         io.Discard,
		EscalateWindow: escalate,
	}
}

// TestRecoverPhase6Overflow is the P47.7 guard: a context overflow during a
// phase-6 verify-fix or quality turn is resumable, so it must classify as a
// bounded retry (escalating the window en route) rather than aborting the drive
// on the raw error — the exact regression the 2026-07-27 FirewallRiskRater run
// hit. A non-overflow error must still be surfaced, and the reset budget must
// terminate a model that overflows every attempt.
func TestRecoverPhase6Overflow(t *testing.T) {
	overflow := provider.NewContextTruncationError("ollama", "unexpected end of JSON input")
	if !provider.IsContextOverflowError(overflow) {
		t.Fatal("test premise: NewContextTruncationError must be an overflow error")
	}

	// A non-overflow error is not handled — the caller surfaces it as terminal.
	st := newTestPhaseState(nil)
	resets := 0
	if got := st.recoverPhase6Overflow(errors.New("boom"), "phase-6 verify fix", &resets); got != overflowNotHandled {
		t.Errorf("non-overflow error = %v, want overflowNotHandled", got)
	}
	if resets != 0 {
		t.Errorf("a non-overflow error must not consume a reset; resets = %d", resets)
	}

	// Overflows within budget retry and escalate; once the budget is exhausted
	// the verdict flips to a clean stop.
	escalations := 0
	st = newTestPhaseState(func() (int, bool) { escalations++; return 131072, true })
	resets = 0
	for i := 1; i <= maxPhase6OverflowResets; i++ {
		if got := st.recoverPhase6Overflow(overflow, "phase-6 quality pass", &resets); got != overflowRetry {
			t.Fatalf("overflow reset %d = %v, want overflowRetry", i, got)
		}
	}
	if escalations != maxPhase6OverflowResets {
		t.Errorf("each retry must attempt an escalation; escalations = %d, want %d", escalations, maxPhase6OverflowResets)
	}
	// One past the budget: stop cleanly instead of looping forever.
	if got := st.recoverPhase6Overflow(overflow, "phase-6 quality pass", &resets); got != overflowStop {
		t.Errorf("overflow past budget = %v, want overflowStop", got)
	}
}

// TestTryEscalateWindow_NilSafe: a drive whose provider can't escalate (nil
// escalateWindow) must treat tryEscalateWindow as a no-op, so the overflow paths
// still fall through to their fresh-context reset.
func TestTryEscalateWindow_NilSafe(t *testing.T) {
	st := newTestPhaseState(nil)
	st.tryEscalateWindow("phase") // must not panic
	called := false
	st = newTestPhaseState(func() (int, bool) { called = true; return 0, false })
	st.tryEscalateWindow("phase")
	if !called {
		t.Error("tryEscalateWindow must invoke escalateWindow when it is set")
	}
}

// TestNextDriveWindow is the P47.5(b) escalation-step guard: each step doubles
// toward the model max, clamps the overshoot to max, and stops (grew=false) once
// at or above the ceiling — which is what bounds the escalation to a finite
// number of steps.
func TestNextDriveWindow(t *testing.T) {
	cases := []struct {
		cur, max int
		wantNext int
		wantGrew bool
	}{
		{65536, 262144, 131072, true},   // plain doubling
		{131072, 262144, 262144, true},  // doubling would overshoot → clamp to max
		{200000, 262144, 262144, true},  // partial step lands exactly at max
		{262144, 262144, 262144, false}, // already at ceiling
		{300000, 262144, 300000, false}, // above ceiling: never shrink
		{65536, 0, 65536, false},        // unknown max: no escalation
		{0, 131072, 131072, true},       // zero start jumps to max
	}
	for _, c := range cases {
		next, grew := NextWindow(c.cur, c.max)
		if next != c.wantNext || grew != c.wantGrew {
			t.Errorf("NextWindow(%d, %d) = (%d, %v), want (%d, %v)", c.cur, c.max, next, grew, c.wantNext, c.wantGrew)
		}
	}
}
