package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestWordBounds(t *testing.T) {
	tests := []struct {
		name       string
		plain      string
		col        int
		start, end int
	}{
		{"middle of word", "hello world", 2, 0, 5},
		{"start of word", "hello world", 0, 0, 5},
		{"last char of word", "hello world", 4, 0, 5},
		{"on space", "hello world", 5, 5, 6},
		{"second word", "hello world", 7, 6, 11},
		{"punctuation is its own word", "a->b", 1, 1, 2},
		{"underscore counts as a word char", "foo_bar baz", 3, 0, 7},
		{"col past end clamps to end", "hi", 10, 2, 2},
		{"negative col clamps to 0", "hi", -3, 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := wordBounds(tt.plain, tt.col)
			if start != tt.start || end != tt.end {
				t.Fatalf("wordBounds(%q, %d) = (%d,%d), want (%d,%d)", tt.plain, tt.col, start, end, tt.start, tt.end)
			}
		})
	}
}

func TestSelectedTextSingleRow(t *testing.T) {
	lines := []string{"hello world"}
	if got := selectedText(lines, 0, 0, 0, 5); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestSelectedTextMultiRow(t *testing.T) {
	lines := []string{"first line", "second line", "third line"}
	got := selectedText(lines, 0, 6, 2, 5)
	want := "line\nsecond line\nthird"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSelectedTextClampsOutOfRange(t *testing.T) {
	lines := []string{"short"}
	if got := selectedText(lines, 0, 0, 0, 100); got != "short" {
		t.Fatalf("expected an out-of-range end column to clamp to the line end, got %q", got)
	}
	if got := selectedText(lines, -5, -5, 0, 3); got != "sho" {
		t.Fatalf("expected an out-of-range start to clamp to the line start, got %q", got)
	}
}

func TestRegisterClickCounting(t *testing.T) {
	m := &model{}
	if n := m.registerClick(5, 3); n != 1 {
		t.Fatalf("first click: got %d, want 1", n)
	}
	if n := m.registerClick(5, 3); n != 2 {
		t.Fatalf("second click at same cell: got %d, want 2", n)
	}
	if n := m.registerClick(5, 3); n != 3 {
		t.Fatalf("third click at same cell: got %d, want 3", n)
	}
	if n := m.registerClick(5, 3); n != 1 {
		t.Fatalf("fourth click at same cell: got %d, want wraparound to 1", n)
	}

	m.registerClick(5, 3)
	if n := m.registerClick(6, 3); n != 1 {
		t.Fatalf("click at a different cell: got %d, want 1", n)
	}

	m.registerClick(6, 3)
	m.sel.lastClickAt = time.Now().Add(-time.Second)
	if n := m.registerClick(6, 3); n != 1 {
		t.Fatalf("click after the double-click window elapsed: got %d, want 1", n)
	}
}

func TestPaneOriginAndToPaneCoord(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	if col, row := m.paneOrigin(); col != 1 || row != 1 {
		t.Fatalf("expected origin (1,1) without a sidebar, got (%d,%d)", col, row)
	}

	if _, _, ok := m.toPaneCoord(1, 1); !ok {
		t.Fatalf("expected (1,1) to fall inside the pane")
	}
	if _, _, ok := m.toPaneCoord(0, 1); ok {
		t.Fatalf("expected (0,1), over the left padding column, to fall outside the pane")
	}
	if _, _, ok := m.toPaneCoord(1, 0); ok {
		t.Fatalf("expected (1,0), over the title bar, to fall outside the pane")
	}

	m.sidebarOpen = true
	if col, _ := m.paneOrigin(); col != 1+sidebarTotalW {
		t.Fatalf("expected the sidebar width folded into the origin, got col %d", col)
	}
}

func TestHandleMouseClickFocusesAndArmsSelection(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	if m.focusedIdx != -1 {
		t.Fatalf("expected no item focused initially, got %d", m.focusedIdx)
	}
	cmd := m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if cmd != nil {
		t.Fatalf("expected a plain single click to return no command")
	}
	if m.focusedIdx < 0 {
		t.Fatalf("expected the item under the click to become focused")
	}
	if !m.sel.active || m.sel.have {
		t.Fatalf("expected a single click to arm a drag without an existing selection, got active=%v have=%v", m.sel.active, m.sel.have)
	}
}

func TestHandleMouseDragThenReleaseCopies(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	m.handleMouseMotion(tea.MouseMotionMsg{X: 10, Y: 2})
	if m.sel.curCol == m.sel.anchorCol {
		t.Fatalf("expected motion to move the selection cursor")
	}
	cmd := m.handleMouseRelease(tea.MouseReleaseMsg{X: 10, Y: 2})
	if cmd == nil {
		t.Fatalf("expected releasing after a drag to return a copy command")
	}
	if !m.sel.have || m.sel.active {
		t.Fatalf("expected a completed (non-active) selection after release, got active=%v have=%v", m.sel.active, m.sel.have)
	}
}

func TestHandleMouseClickNoDragReleaseCopiesNothing(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	cmd := m.handleMouseRelease(tea.MouseReleaseMsg{X: 2, Y: 2})
	if cmd != nil {
		t.Fatalf("expected a release with no movement to copy nothing")
	}
	if m.sel.have {
		t.Fatalf("expected no lingering selection after a plain click")
	}
}

func TestHandleMouseDoubleClickSelectsWord(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	cmd := m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if !m.sel.have || m.sel.active {
		t.Fatalf("expected a double-click to leave a completed selection, got active=%v have=%v", m.sel.active, m.sel.have)
	}
	if m.sel.anchorCol == m.sel.curCol {
		t.Fatalf("expected the double-click word selection to span more than zero columns")
	}
	if cmd == nil {
		t.Fatalf("expected a double-click on a non-blank cell to copy the word")
	}
}

func TestHandleMouseTripleClickSelectsLine(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	cmd := m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if m.sel.anchorCol != 0 {
		t.Fatalf("expected a triple-click to select from column 0, got %d", m.sel.anchorCol)
	}
	if cmd == nil {
		t.Fatalf("expected a triple-click on a non-blank line to copy it")
	}
}

func TestHandleMouseClickIgnoresNonLeftButton(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	cmd := m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseRight})
	if cmd != nil || m.focusedIdx != -1 {
		t.Fatalf("expected a right-click to be ignored entirely")
	}
}

func TestHandleMouseClickOutsidePaneIgnored(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	cmd := m.handleMouseClick(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if cmd != nil || m.focusedIdx != -1 || m.sel.active {
		t.Fatalf("expected a click over the title bar to be ignored")
	}
}

// TestMouseDragThroughUpdate exercises the same click/drag/release sequence
// as TestHandleMouseDragThenReleaseCopies, but through the real Update
// dispatch (the path bubbletea actually drives at runtime) rather than
// calling the handler methods directly, to verify the tea.MouseClickMsg /
// tea.MouseMotionMsg / tea.MouseReleaseMsg cases are wired up correctly.
func TestMouseDragThroughUpdate(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m = driveUpdate(t, m, tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if !m.sel.active {
		t.Fatalf("expected Update to arm a drag selection on mouse click")
	}

	m = driveUpdate(t, m, tea.MouseMotionMsg{X: 10, Y: 2})
	if m.sel.curCol == m.sel.anchorCol {
		t.Fatalf("expected Update to move the selection cursor on mouse motion")
	}

	next, cmd := m.Update(tea.MouseReleaseMsg{X: 10, Y: 2})
	m, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned unexpected type %T", next)
	}
	if cmd == nil {
		t.Fatalf("expected Update to return a copy command on releasing after a drag")
	}
	if !m.sel.have || m.sel.active {
		t.Fatalf("expected a completed selection after release via Update, got active=%v have=%v", m.sel.active, m.sel.have)
	}
}

// TestMouseWheelThroughUpdate is a minimal smoke test that tea.MouseWheelMsg
// still reaches the transcript pane's scroll handling through Update.
func TestMouseWheelThroughUpdate(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	for i := 0; i < 200; i++ {
		m.transcript.Append("line\n")
	}
	m.transcript.GotoTop()
	before := m.transcript.AtTop()

	m = driveUpdate(t, m, tea.MouseWheelMsg{X: 2, Y: 2, Button: tea.MouseWheelDown})
	if !before {
		t.Fatalf("test setup: expected to start scrolled to top")
	}
	if m.transcript.AtTop() {
		t.Fatalf("expected a wheel-down event dispatched through Update to scroll the transcript")
	}
}
