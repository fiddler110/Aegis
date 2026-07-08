package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCtrlR_OpensHistorySearch checks the P22.4 rebind: Ctrl+R opens the
// input-history picker (newest first) rather than the session switcher,
// which moved to Ctrl+Y to make room.
func TestCtrlR_OpensHistorySearch(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.history = []string{"first message", "second message", "third message"}

	m = driveUpdate(t, m, ctrlKeyMsg('r'))
	if m.dialog == nil {
		t.Fatal("expected ctrl+r to open a dialog")
	}
	if m.dialog.kind != dialogHistoryPicker {
		t.Fatalf("expected dialogHistoryPicker, got %v", m.dialog.kind)
	}
	items := m.dialog.list.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 history items, got %d", len(items))
	}
	if items[0].(historyItem).text != "third message" {
		t.Errorf("expected newest entry first, got %#v", items[0])
	}
}

// TestCtrlR_NoHistoryShowsToast checks Ctrl+R with no sent messages yet
// doesn't open an empty dialog.
func TestCtrlR_NoHistoryShowsToast(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m = driveUpdate(t, m, ctrlKeyMsg('r'))
	if m.dialog != nil {
		t.Fatal("expected no dialog to open with empty history")
	}
}

// TestHistoryPickerSelection_RecallsOntoInput checks selecting an entry sets
// the input line to the recalled text without sending it, mirroring a
// shell's reverse-search accept behavior.
func TestHistoryPickerSelection_RecallsOntoInput(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.history = []string{"old draft", "recall me"}

	m = driveUpdate(t, m, ctrlKeyMsg('r'))
	if m.dialog == nil {
		t.Fatal("expected dialog to open")
	}

	m = driveUpdate(t, m, dialogSelectedMsg{kind: dialogHistoryPicker, item: historyItem{text: "recall me"}})
	if m.dialog != nil {
		t.Error("expected dialog to close after selection")
	}
	if m.ta.Value() != "recall me" {
		t.Errorf("input value = %q, want %q", m.ta.Value(), "recall me")
	}
}

// TestCtrlY_OpensSessionFetch checks the session switcher moved to Ctrl+Y
// (freeing Ctrl+R for history search) — driving it returns a non-nil cmd
// (fetchSessions) rather than opening the history dialog.
func TestCtrlY_OpensSessionFetch(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	_, cmd := m.Update(ctrlKeyMsg('y'))
	if cmd == nil {
		t.Fatal("expected ctrl+y to produce a fetchSessions cmd")
	}
}
