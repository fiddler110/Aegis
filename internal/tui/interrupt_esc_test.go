package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// streamingModel returns a model parked mid-run with a recording cancel func
// and a queued message, so the interrupt path's two side effects (cancel the
// run, discard the TQ8 queue) are both observable.
func streamingModel(t *testing.T) (model, *bool) {
	t.Helper()
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	canceled := false
	m.streamState.streaming = true
	m.streamState.cancel = func() { canceled = true }
	m.composer.queued = []string{"next message"}
	return m, &canceled
}

// TestEsc_Streaming_SinglePressInterrupts is the P33.5 change: with an empty
// input box, one Esc cancels the run outright rather than arming a double-tap.
func TestEsc_Streaming_SinglePressInterrupts(t *testing.T) {
	m, canceled := streamingModel(t)

	m = driveUpdate(t, m, escKeyMsg())
	if !*canceled {
		t.Error("expected a single Esc while streaming to cancel the run")
	}
	if m.composer.backtrackArmed {
		t.Error("the streaming interrupt should not arm backtrackArmed")
	}
	if m.composer.queued != nil {
		t.Errorf("expected the interrupt to discard the queue, got %v", m.composer.queued)
	}
}

// TestEsc_StreamingWithDraft_ClearsThenInterrupts checks P33.5 preserved the
// "Esc clears the input box" behavior: with text typed, the first press only
// clears and the run keeps streaming; the second press interrupts.
func TestEsc_StreamingWithDraft_ClearsThenInterrupts(t *testing.T) {
	m, canceled := streamingModel(t)
	m.ta.SetValue("draft text")

	m = driveUpdate(t, m, escKeyMsg())
	if m.ta.Value() != "" {
		t.Errorf("expected the first Esc to clear the input, got %q", m.ta.Value())
	}
	if *canceled {
		t.Fatal("the clearing Esc should not have canceled the run")
	}
	if m.composer.queued == nil {
		t.Error("the clearing Esc should not have discarded the queue")
	}

	m = driveUpdate(t, m, escKeyMsg())
	if !*canceled {
		t.Error("expected the second Esc to cancel the run")
	}
	if m.composer.queued != nil {
		t.Errorf("expected the interrupt to discard the queue, got %v", m.composer.queued)
	}
}

// TestAltEsc_StreamingWithDraft_ClearsAndInterrupts covers the same-frame
// decoder quirk on the has-text path: a double-tap coalesced into one
// "alt+esc" event must both clear the box and interrupt, not just clear.
func TestAltEsc_StreamingWithDraft_ClearsAndInterrupts(t *testing.T) {
	m, canceled := streamingModel(t)
	m.ta.SetValue("draft text")

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape, Mod: tea.ModAlt})
	if m.ta.Value() != "" {
		t.Errorf("expected alt+esc to clear the input, got %q", m.ta.Value())
	}
	if !*canceled {
		t.Error("expected alt+esc (same-frame double-tap) to also cancel the run")
	}
	if m.composer.queued != nil {
		t.Errorf("expected the interrupt to discard the queue, got %v", m.composer.queued)
	}
}
