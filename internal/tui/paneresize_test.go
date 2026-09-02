package tui

import "testing"

// P40.1: resizePane grows/shrinks the focused pane within bounds.
func TestResizePaneSidebar(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.chrome.width = 120 // wide enough that the sidebar shows (>= sidebarMinTermW)
	m.chrome.height = 40
	m.chrome.sidebarOpen = true
	m.layout()

	start := m.chrome.sidebarW
	if start != sidebarInnerW {
		t.Fatalf("initial sidebarW = %d, want default %d", start, sidebarInnerW)
	}

	if !m.resizePane(paneResizeStep) {
		t.Fatal("grow: resizePane returned false, want a change")
	}
	if m.chrome.sidebarW != start+paneResizeStep {
		t.Fatalf("grow: sidebarW = %d, want %d", m.chrome.sidebarW, start+paneResizeStep)
	}

	if !m.resizePane(-paneResizeStep) {
		t.Fatal("shrink: resizePane returned false, want a change")
	}
	if m.chrome.sidebarW != start {
		t.Fatalf("shrink: sidebarW = %d, want %d", m.chrome.sidebarW, start)
	}
}

func TestResizePaneSidebarClampsToBounds(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.chrome.width = 120
	m.chrome.height = 40
	m.chrome.sidebarOpen = true
	m.layout()

	// Grow past the max — width clamps and eventually reports no change.
	for i := 0; i < 100; i++ {
		m.resizePane(paneResizeStep)
	}
	if m.chrome.sidebarW > sidebarMaxW {
		t.Fatalf("sidebarW = %d exceeded max %d", m.chrome.sidebarW, sidebarMaxW)
	}
	if m.resizePane(paneResizeStep) {
		t.Fatal("at max, resizePane should report no change")
	}

	// Shrink past the min likewise clamps.
	for i := 0; i < 100; i++ {
		m.resizePane(-paneResizeStep)
	}
	if m.chrome.sidebarW < sidebarMinW {
		t.Fatalf("sidebarW = %d below min %d", m.chrome.sidebarW, sidebarMinW)
	}
	if m.resizePane(-paneResizeStep) {
		t.Fatal("at min, resizePane should report no change")
	}
}

func TestResizePaneTerminalFocused(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.chrome.width = 120
	m.chrome.height = 40
	m.splitTerm.term = newTermPane(t.TempDir(), 30)
	m.splitTerm.termOpen = true
	m.splitTerm.termFocused = true
	m.layout()

	start := m.splitTerm.term.width
	if !m.resizePane(paneResizeStep) {
		t.Fatal("grow: resizePane returned false, want a change")
	}
	if m.splitTerm.term.width != start+paneResizeStep {
		t.Fatalf("grow: term.width = %d, want %d", m.splitTerm.term.width, start+paneResizeStep)
	}
}

func TestResizePaneNoFocusIsNoop(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.chrome.width = 120
	m.chrome.height = 40
	// Neither the sidebar nor a focused terminal is open.
	if m.resizePane(paneResizeStep) {
		t.Fatal("with no resizable pane focused, resizePane should be a no-op")
	}
}
