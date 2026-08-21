package tui

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// P74.2: the sidebar composites over the finished frame instead of being
// joined into the layout, so opening/closing it must never change the
// transcript pane's own geometry (width/height) — only what's painted on
// top of it.
func TestSidebarOverlayDoesNotChangeTranscriptGeometry(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	closedW, closedH := m.transcript.Width(), m.transcript.Height()

	m.sidebarOpen = true
	m.layout()
	if w, h := m.transcript.Width(), m.transcript.Height(); w != closedW || h != closedH {
		t.Fatalf("sidebar open: transcript geometry changed to (%d,%d), want unchanged (%d,%d)", w, h, closedW, closedH)
	}

	// The overlay itself must still be visible in the rendered frame.
	if got := plainView(m); !strings.Contains(got, "◇ SESSION") {
		t.Errorf("expected the sidebar overlay to render with sidebarOpen=true, got:\n%s", got)
	}

	m.sidebarOpen = false
	m.layout()
	if w, h := m.transcript.Width(), m.transcript.Height(); w != closedW || h != closedH {
		t.Fatalf("sidebar closed again: transcript geometry = (%d,%d), want unchanged (%d,%d)", w, h, closedW, closedH)
	}
}

// P74.2: the scrollbar column carries no information while pinned to the
// bottom — the normal state — so it must render blank there, and only draw
// a track/thumb once the user has scrolled away.
func TestScrollbarAutoHidesWhilePinnedToBottom(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})

	// Push in enough content that the pane is actually scrollable.
	for i := 0; i < 200; i++ {
		m.transcript.Append("scrollbar-line\n")
	}
	m.applyViewportHeight()

	if !m.followBottom {
		t.Fatal("expected followBottom true immediately after appending content")
	}
	if bar := ansi.Strip(m.renderScrollbar()); strings.ContainsAny(bar, "┃│") {
		t.Errorf("expected a blank scrollbar column while pinned to the bottom, got %q", bar)
	}

	m.followBottom = false
	if bar := ansi.Strip(m.renderScrollbar()); !strings.ContainsAny(bar, "┃│") {
		t.Errorf("expected a visible scrollbar track/thumb once scrolled away from the bottom, got %q", bar)
	}
}

// P74.2: resize re-wrap is the property the whole alt-screen decision exists
// to keep — the transcript's rendered width must still track the terminal
// width after the chrome removal (title bar gone, sidebar off the layout).
func TestResizeStillRewrapsAfterChromeRemoval(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	wide := m.transcript.Width()

	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 40})
	narrow := m.transcript.Width()

	if narrow >= wide {
		t.Fatalf("expected the transcript to narrow on resize: wide=%d narrow=%d", wide, narrow)
	}

	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	if got := m.transcript.Width(); got != wide {
		t.Fatalf("expected the transcript to widen back to %d, got %d", wide, got)
	}
}

// P74.2: the brand mark and connection/model badge, which used to be their
// own always-visible title-bar row, are now folded into the status line —
// there is no separate row for them, and they still appear somewhere in the
// frame.
func TestTitleBarFoldedIntoStatusLine(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "test-model-xyz", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	seg := ansi.Strip(m.renderBrandSegment())
	if !strings.Contains(seg, "AEGIS") {
		t.Errorf("expected the folded brand segment to contain the wordmark, got %q", seg)
	}
	if !strings.Contains(seg, "test-model-xyz") {
		t.Errorf("expected the folded brand segment to contain the model name, got %q", seg)
	}

	statusRow := ansi.Strip(m.renderInputArea())
	if !strings.Contains(statusRow, "AEGIS") {
		t.Errorf("expected the status line to carry the brand mark, got %q", statusRow)
	}

	// The frame's very first row is transcript content now, not a title bar:
	// renderChat no longer prepends a dedicated brand/connection row above it.
	full := ansi.Strip(m.renderChat())
	firstLine := strings.SplitN(full, "\n", 2)[0]
	if strings.Contains(firstLine, "AEGIS") {
		t.Errorf("expected no standalone title-bar row at the top of the frame, got %q", firstLine)
	}
}

// P74.2: a click over the sidebar overlay's screen columns must not resolve
// to transcript content underneath it — the pane no longer reserves layout
// width for the sidebar, so without this check a click there would silently
// select/focus content the user cannot see.
func TestSidebarOverlayOccludesPaneMouseCoords(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.sidebarOpen = true

	if _, _, ok := m.toPaneCoord(1, 0); ok {
		t.Fatal("expected a click under the sidebar overlay to fall outside the pane")
	}
	if _, _, ok := m.toPaneCoord(sidebarTotalW, 0); !ok {
		t.Fatalf("expected a click just past the sidebar overlay (col %d) to fall inside the pane", sidebarTotalW)
	}
}
