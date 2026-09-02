package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

	// P74.2: the title bar row is gone, so the origin is (1,0) — the origin
	// itself no longer moves when the sidebar opens, since it composites as
	// an overlay instead of being joined into the layout.
	if col, row := m.paneOrigin(); col != 1 || row != 0 {
		t.Fatalf("expected origin (1,0) without a title bar, got (%d,%d)", col, row)
	}

	if _, _, ok := m.toPaneCoord(1, 0); !ok {
		t.Fatalf("expected (1,0) to fall inside the pane")
	}
	if _, _, ok := m.toPaneCoord(0, 0); ok {
		t.Fatalf("expected (0,0), over the left padding column, to fall outside the pane")
	}

	m.chrome.sidebarOpen = true
	if col, row := m.paneOrigin(); col != 1 || row != 0 {
		t.Fatalf("expected the sidebar overlay to leave the pane origin unmoved, got (%d,%d)", col, row)
	}
	if _, _, ok := m.toPaneCoord(1, 0); ok {
		t.Fatalf("expected (1,0), under the sidebar overlay, to fall outside the pane while it's open")
	}
	if _, _, ok := m.toPaneCoord(sidebarTotalW, 0); !ok {
		t.Fatalf("expected (%d,0), just past the sidebar overlay, to fall inside the pane", sidebarTotalW)
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

// TestSelectionOverlayUsesBackgroundNotReverse pins P74.18: the drag-selection
// overlay must set a background fill and leave foreground untouched, not
// SGR-7 Reverse — which fragments over chroma-highlighted content because it
// swaps whichever fg/bg happen to be active per cell, so every differently
// colored token inverts to a different background.
func TestSelectionOverlayUsesBackgroundNotReverse(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	// Two differently-colored tokens on one line with no reset in between,
	// the shape chroma-highlighted diff/read_file output actually takes.
	red := "\x1b[38;2;255;0;0mHello"
	green := "\x1b[38;2;0;255;0mWorld\x1b[0m"
	m.transcript.items = nil // drop the welcome banner so our line is row 0
	m.transcript.invalidateItemsHeight()
	m.transcript.AppendRaw(red + " " + green + "\n")
	m.transcript.GotoTop()

	m.sel.have = true
	m.sel.anchorRow, m.sel.anchorCol = 0, 0
	m.sel.curRow, m.sel.curCol = 0, len("Hello World")

	out := m.renderTranscriptContent()

	if strings.Contains(out, "\x1b[7m") || strings.Contains(out, ";7m") {
		t.Fatalf("selection overlay still emits SGR-7 Reverse: %q", out)
	}

	bgProbe := lipgloss.NewStyle().Background(colSelectionBg).Render("X")
	bgCode, _, ok := strings.Cut(bgProbe, "X")
	if !ok || bgCode == "" {
		t.Fatalf("could not derive the selection background escape from probe %q", bgProbe)
	}
	if !strings.Contains(out, bgCode) {
		t.Fatalf("expected the selection background code %q in rendered output %q", bgCode, out)
	}

	// Both original foreground colors must survive under the selection — a
	// background fill replaces the cell's background only.
	if !strings.Contains(out, "255;0;0") || !strings.Contains(out, "0;255;0") {
		t.Fatalf("expected both chroma foreground colors preserved under the selection, got %q", out)
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
