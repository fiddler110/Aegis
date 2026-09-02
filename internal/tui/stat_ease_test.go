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
	m.streamState.streaming = true
	m.streamState.followBottom = true
	m.usage.inputTokens = 8000
	m.usage.outputTokens = 800

	next, _ := m.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
	if next.usage.displayedInputTokens == 0 || next.usage.displayedInputTokens >= next.usage.inputTokens {
		t.Errorf("expected displayedInputTokens to move partway toward %d, got %d", next.usage.inputTokens, next.usage.displayedInputTokens)
	}
	if next.usage.displayedOutputTokens == 0 || next.usage.displayedOutputTokens >= next.usage.outputTokens {
		t.Errorf("expected displayedOutputTokens to move partway toward %d, got %d", next.usage.outputTokens, next.usage.displayedOutputTokens)
	}

	// Repeated ticks converge exactly, never overshoot or oscillate.
	m2 := next
	for i := 0; i < 200; i++ {
		m2, _ = m2.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m2.sp.ID()})
	}
	if m2.usage.displayedInputTokens != m2.usage.inputTokens || m2.usage.displayedOutputTokens != m2.usage.outputTokens {
		t.Errorf("expected displayed counters to converge, got in=%d/%d out=%d/%d",
			m2.usage.displayedInputTokens, m2.usage.inputTokens, m2.usage.displayedOutputTokens, m2.usage.outputTokens)
	}
}

// TestReducedMotionSnapsStatCounters is the P74.10 interaction: under reduced
// motion the displayed counters must show the true number immediately rather
// than animating toward it.
func TestReducedMotionSnapsStatCounters(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir(), ReducedMotion: true})
	m.streamState.streaming = true
	m.streamState.followBottom = true
	m.usage.inputTokens = 8000
	m.usage.outputTokens = 800

	next, _ := m.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
	if next.usage.displayedInputTokens != 8000 || next.usage.displayedOutputTokens != 800 {
		t.Errorf("expected reduced motion to snap displayed counters immediately, got in=%d out=%d",
			next.usage.displayedInputTokens, next.usage.displayedOutputTokens)
	}
}
