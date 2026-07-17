package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestTransientResultOpensPanelNotTranscript is the core P33.11 guarantee:
// informational slash output (Transient: true) opens a dismissable overlay
// panel and never touches the transcript, so housekeeping doesn't leave stale
// blocks behind.
func TestTransientResultOpensPanelNotTranscript(t *testing.T) {
	m := idleModel(t)
	before := m.transcript.Len()

	m = driveUpdate(t, m, slashResultMsg{
		Transient:      true,
		TransientTitle: "/status",
		Output:         "Daemon: ok\nProvider: ollama · Model: test",
	})

	if m.transientPanel == nil {
		t.Fatal("expected a transient panel to open")
	}
	if got := m.transcript.Len(); got != before {
		t.Errorf("transcript grew by %d items; transient output must not enter the transcript", got-before)
	}
	view := plainView(m)
	if !strings.Contains(view, "/status") {
		t.Errorf("expected the panel title on screen, got:\n%s", view)
	}
	if !strings.Contains(view, "Daemon: ok") {
		t.Errorf("expected the panel body on screen, got:\n%s", view)
	}

	// esc dismisses it and restores the composer, leaving the transcript
	// untouched.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.transientPanel != nil {
		t.Error("expected esc to dismiss the transient panel")
	}
	if got := m.transcript.Len(); got != before {
		t.Errorf("transcript changed after dismissing the panel: %d vs %d", got, before)
	}
}

// TestNonTransientResultStaysInTranscript confirms the flag is what routes the
// output: an action confirmation (Transient: false) still appends to the
// transcript and opens no panel.
func TestNonTransientResultStaysInTranscript(t *testing.T) {
	m := idleModel(t)
	before := m.transcript.Len()

	m = driveUpdate(t, m, slashResultMsg{Output: "Switched to build mode."})

	if m.transientPanel != nil {
		t.Error("a non-transient result must not open a panel")
	}
	if got := m.transcript.Len(); got <= before {
		t.Error("expected a non-transient result to append to the transcript")
	}
}

// TestTransientPanelScrollsTallOutput exercises the scrolling story P33.11
// calls out for tall output (/help, /models): the window scrolls with the
// arrow/page keys and clamps at both ends.
func TestTransientPanelScrollsTallOutput(t *testing.T) {
	m := idleModel(t)

	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	m = driveUpdate(t, m, slashResultMsg{Transient: true, TransientTitle: "/help", Output: b.String()})
	if m.transientPanel == nil {
		t.Fatal("expected a panel")
	}
	if !m.transientPanel.scrollable() {
		t.Fatal("expected 200 lines to overflow the window and be scrollable")
	}
	if m.transientPanel.offset != 0 {
		t.Fatalf("expected to open scrolled to the top, got offset %d", m.transientPanel.offset)
	}

	// Scrolling up at the top is a clamped no-op.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'k'})
	if m.transientPanel.offset != 0 {
		t.Errorf("scroll-up at the top should clamp to 0, got %d", m.transientPanel.offset)
	}

	// Down advances the window.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'j'})
	if m.transientPanel.offset != 1 {
		t.Errorf("one line down should move to offset 1, got %d", m.transientPanel.offset)
	}

	// End jumps to (and clamps at) the bottom.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnd})
	max := m.transientPanel.maxOffset()
	if m.transientPanel.offset != max {
		t.Errorf("end should jump to maxOffset %d, got %d", max, m.transientPanel.offset)
	}
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'j'})
	if m.transientPanel.offset != max {
		t.Errorf("scroll-down at the bottom should clamp to %d, got %d", max, m.transientPanel.offset)
	}
}

// TestDialogBlockLetsStreamEventsThrough is the P33.20 regression: while an
// overlay dialog is open, stream-lifecycle events must still reach the main
// update path rather than being swallowed. Before the allowlist, a
// batchEventMsg landing with a dialog open fell into the dialog's default case
// and was dropped — so a run streaming behind an open overlay would never end.
func TestDialogBlockLetsStreamEventsThrough(t *testing.T) {
	m := idleModel(t)
	d := newListDialog(dialogPalette, 40, 10, "Commands", false, nil)
	m.dialog = &d
	m.streaming = true

	// A stream close arriving while the dialog is open must run the real
	// teardown, not be eaten by the dialog block.
	m = driveUpdate(t, m, batchEventMsg{closed: true})
	if m.streaming {
		t.Error("streamClosedMsg was swallowed by the open-dialog block; stream never ended (P33.20)")
	}

	if !isStreamLifecycleMsg(batchEventMsg{}) {
		t.Error("batchEventMsg must be on the stream-lifecycle allowlist")
	}
	if isStreamLifecycleMsg(tea.KeyPressMsg{}) {
		t.Error("a key press is not a stream-lifecycle message")
	}
}
