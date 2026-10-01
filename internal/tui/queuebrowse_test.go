package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// altUpKeyMsg/altDownKeyMsg/ctrlAltUpKeyMsg/ctrlAltDownKeyMsg are the P85.3
// browse/reorder key presses.
func altUpKeyMsg() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt} }
func altDownKeyMsg() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt} }
func ctrlAltUpKeyMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt | tea.ModCtrl}
}
func ctrlAltDownKeyMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt | tea.ModCtrl}
}

// queuedModel returns a model mid-run with three messages already queued via
// TQ8 Enter-while-streaming, ready for the P85.3 browse/reorder keys.
func queuedModel(t *testing.T, drafts ...string) model {
	t.Helper()
	m := steerModel(t, "")
	m.composer.queued = append(m.composer.queued, drafts...)
	m.refresh()
	return m
}

// TestQueueBrowseEntersFromPressedDirection mirrors HistUp/HistDown's own
// first-press behavior: alt+up enters the queue from its last (bottom-most)
// entry, alt+down from its first.
func TestQueueBrowseEntersFromPressedDirection(t *testing.T) {
	m := queuedModel(t, "first", "second", "third")

	up := driveUpdate(t, m, altUpKeyMsg())
	if up.composer.queuedSel != 2 {
		t.Fatalf("queuedSel = %d, want 2 (alt+up enters at the last entry)", up.composer.queuedSel)
	}
	if got := plainView(up); !strings.Contains(got, "[3/3] ▸▸ third") {
		t.Fatalf("expected the selected entry expanded, got:\n%s", got)
	}

	down := driveUpdate(t, m, altDownKeyMsg())
	if down.composer.queuedSel != 0 {
		t.Fatalf("queuedSel = %d, want 0 (alt+down enters at the first entry)", down.composer.queuedSel)
	}
	if got := plainView(down); !strings.Contains(got, "[1/3] ▸▸ first") {
		t.Fatalf("expected the selected entry expanded, got:\n%s", got)
	}
}

// TestQueueBrowseClampsAtEnds checks alt+up/alt+down stop at the queue's
// boundaries rather than wrapping or going out of range.
func TestQueueBrowseClampsAtEnds(t *testing.T) {
	m := queuedModel(t, "first", "second")
	m.composer.queuedSel = 0

	m = driveUpdate(t, m, altUpKeyMsg())
	if m.composer.queuedSel != 0 {
		t.Fatalf("queuedSel = %d, want clamped at 0", m.composer.queuedSel)
	}

	m.composer.queuedSel = 1
	m = driveUpdate(t, m, altDownKeyMsg())
	if m.composer.queuedSel != 1 {
		t.Fatalf("queuedSel = %d, want clamped at len-1", m.composer.queuedSel)
	}
}

// TestQueueMoveReordersSelection is the reorder half: ctrl+alt+up/down swap
// the selected entry with its neighbor and the selection follows it, so
// repeated presses walk an entry to the front of the queue.
func TestQueueMoveReordersSelection(t *testing.T) {
	m := queuedModel(t, "first", "second", "third")
	m.composer.queuedSel = 2 // "third"

	m = driveUpdate(t, m, ctrlAltUpKeyMsg())
	want := []string{"first", "third", "second"}
	if !equalStrings(m.composer.queued, want) {
		t.Fatalf("queued = %v, want %v", m.composer.queued, want)
	}
	if m.composer.queuedSel != 1 {
		t.Fatalf("queuedSel = %d, want 1 (selection follows the moved entry)", m.composer.queuedSel)
	}

	m = driveUpdate(t, m, ctrlAltUpKeyMsg())
	want = []string{"third", "first", "second"}
	if !equalStrings(m.composer.queued, want) {
		t.Fatalf("queued = %v, want %v", m.composer.queued, want)
	}
	if m.composer.queuedSel != 0 {
		t.Fatalf("queuedSel = %d, want 0", m.composer.queuedSel)
	}

	// Already at the front: ctrl+alt+up is a no-op.
	m = driveUpdate(t, m, ctrlAltUpKeyMsg())
	if !equalStrings(m.composer.queued, want) {
		t.Fatalf("queued = %v, want unchanged %v", m.composer.queued, want)
	}

	m = driveUpdate(t, m, ctrlAltDownKeyMsg())
	want = []string{"first", "third", "second"}
	if !equalStrings(m.composer.queued, want) {
		t.Fatalf("queued = %v, want %v", m.composer.queued, want)
	}
	if m.composer.queuedSel != 1 {
		t.Fatalf("queuedSel = %d, want 1", m.composer.queuedSel)
	}
}

// TestQueueMoveRequiresSelection checks ctrl+alt+up/down are no-ops when
// nothing is selected yet (queuedSel == -1) — browse first, then reorder.
func TestQueueMoveRequiresSelection(t *testing.T) {
	m := queuedModel(t, "first", "second")
	want := append([]string(nil), m.composer.queued...)

	m = driveUpdate(t, m, ctrlAltUpKeyMsg())
	if !equalStrings(m.composer.queued, want) {
		t.Fatalf("queued = %v, want unchanged %v", m.composer.queued, want)
	}
	m = driveUpdate(t, m, ctrlAltDownKeyMsg())
	if !equalStrings(m.composer.queued, want) {
		t.Fatalf("queued = %v, want unchanged %v", m.composer.queued, want)
	}
}

// TestQueueSelectionResetsOnDrainAndDiscard checks the selection index never
// survives a queue mutation it wasn't computed against: draining the front
// entry into the next turn, or an interrupt/error discarding the queue.
func TestQueueSelectionResetsOnDrainAndDiscard(t *testing.T) {
	m := queuedModel(t, "first", "second")
	m.composer.queuedSel = 1
	m.refresh()

	m = driveUpdate(t, m, streamClosedMsg{})
	if m.composer.queuedSel != -1 {
		t.Fatalf("queuedSel = %d, want reset to -1 after the drain", m.composer.queuedSel)
	}

	m2 := queuedModel(t, "first", "second")
	m2.composer.queuedSel = 1
	m2.streamState.cancel = func() {}
	m2 = driveUpdate(t, m2, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m2.composer.queuedSel != -1 {
		t.Fatalf("queuedSel = %d, want reset to -1 after interrupt", m2.composer.queuedSel)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
