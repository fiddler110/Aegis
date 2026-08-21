package tui

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
)

// TestStatCountersEaseTowardTarget is the P74.12 regression guard: a spinner
// tick during a run must move the displayed token counters toward the real
// ones by a partial step, not snap them, so the status bar climbs instead of
// jumping in chunk-sized increments.
func TestStatCountersEaseTowardTarget(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.streaming = true
	m.followBottom = true
	m.inputTokens = 8000
	m.outputTokens = 800

	next, _ := m.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
	if next.displayedInputTokens == 0 || next.displayedInputTokens >= next.inputTokens {
		t.Errorf("expected displayedInputTokens to move partway toward %d, got %d", next.inputTokens, next.displayedInputTokens)
	}
	if next.displayedOutputTokens == 0 || next.displayedOutputTokens >= next.outputTokens {
		t.Errorf("expected displayedOutputTokens to move partway toward %d, got %d", next.outputTokens, next.displayedOutputTokens)
	}

	// Repeated ticks converge exactly, never overshoot or oscillate.
	m2 := next
	for i := 0; i < 200; i++ {
		m2, _ = m2.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m2.sp.ID()})
	}
	if m2.displayedInputTokens != m2.inputTokens || m2.displayedOutputTokens != m2.outputTokens {
		t.Errorf("expected displayed counters to converge, got in=%d/%d out=%d/%d",
			m2.displayedInputTokens, m2.inputTokens, m2.displayedOutputTokens, m2.outputTokens)
	}
}

// TestReducedMotionSnapsStatCounters is the P74.10 interaction: under reduced
// motion the displayed counters must show the true number immediately rather
// than animating toward it.
func TestReducedMotionSnapsStatCounters(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir(), ReducedMotion: true})
	m.streaming = true
	m.followBottom = true
	m.inputTokens = 8000
	m.outputTokens = 800

	next, _ := m.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
	if next.displayedInputTokens != 8000 || next.displayedOutputTokens != 800 {
		t.Errorf("expected reduced motion to snap displayed counters immediately, got in=%d out=%d",
			next.displayedInputTokens, next.displayedOutputTokens)
	}
}
