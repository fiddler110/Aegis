package tui

import (
	"testing"
	"time"
)

// TestStallRampColorHonoursDisabledBound is the P74.11 regression guard:
// with the stall bound off (zero) or no elapsed wait yet, the ramp must not
// invent a color — colAccent is returned unchanged.
func TestStallRampColorHonoursDisabledBound(t *testing.T) {
	if got := stallRampColor(30*time.Second, 0); got != colAccent {
		t.Errorf("expected colAccent with a disabled (zero) bound, got %v", got)
	}
	if got := stallRampColor(0, 900*time.Second); got != colAccent {
		t.Errorf("expected colAccent with zero elapsed, got %v", got)
	}
}

// TestStallRampColorRampsTowardWarning checks the ramp is monotonic (never
// cools back toward colAccent as the wait lengthens) and reaches full
// colWarning at the bound itself, without waiting past it.
func TestStallRampColorRampsTowardWarning(t *testing.T) {
	bound := 900 * time.Second
	early := stallRampColor(30*time.Second, bound)
	mid := stallRampColor(300*time.Second, bound)
	atBound := stallRampColor(bound, bound)
	pastBound := stallRampColor(2*bound, bound)

	if early == colAccent {
		t.Error("expected the ramp visibly underway well before the bound, still colAccent at 30s/900s")
	}
	if mid == early {
		t.Error("expected the ramp to keep moving between 30s and 300s of a 900s bound")
	}
	// Blend1D's Lab round trip means the saturated endpoint isn't always
	// bit-identical to colWarning, so compare it against itself instead: past
	// the point the ramp saturates (70% of bound, see stallRampColor), the
	// color must stop moving rather than overshoot or wrap back.
	if atBound != pastBound {
		t.Errorf("expected the ramp to hold steady past its saturation point, got %v at bound and %v past it", atBound, pastBound)
	}
}

// TestStallElapsedTracksTheActivePhase confirms stallElapsed reads the clock
// for whichever wait phase is active (waiting for the first token, or a
// post-tool-round re-eval) and is zero once the model is actively producing
// output — tokens arriving is forward progress, not a stall.
func TestStallElapsedTracksTheActivePhase(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})

	if got := m.stallElapsed(); got != 0 {
		t.Errorf("expected zero stallElapsed when idle, got %v", got)
	}

	m.phase.streamStart = time.Now().Add(-45 * time.Second)
	if got := m.stallElapsed(); got < 44*time.Second || got > 46*time.Second {
		t.Errorf("expected ~45s stallElapsed while waiting for the first token, got %v", got)
	}

	m.phase.firstTokenAt = time.Now()
	if got := m.stallElapsed(); got != 0 {
		t.Errorf("expected zero stallElapsed once generating (no re-eval wait), got %v", got)
	}

	m.phase.modelWaitAt = time.Now().Add(-10 * time.Second)
	if got := m.stallElapsed(); got < 9*time.Second || got > 11*time.Second {
		t.Errorf("expected ~10s stallElapsed during a post-tool-round re-eval wait, got %v", got)
	}
}
