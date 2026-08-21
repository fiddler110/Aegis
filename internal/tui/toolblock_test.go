package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
)

// manyLines returns a result body with n newline-separated lines — enough to
// exceed toolMaxLinesCompact and collapse to a summary line under the
// session-wide compact default.
func manyLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line"
	}
	return strings.Join(lines, "\n")
}

// TestToolBlockToggle_ExpandsOneCardIndependentOfSessionDefault is the core
// P75.1 regression guard: toggling the last tool block expands only that
// card in place, while the session-wide toolCompact default — and therefore
// every other (in this case, later) card — is untouched.
func TestToolBlockToggle_ExpandsOneCardIndependentOfSessionDefault(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("run a command", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "shell", ToolID: "tu_1", ToolInput: json.RawMessage(`{"command":"ls"}`)})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "shell", ToolID: "tu_1", ToolResult: manyLines(20)})
	m.refresh()
	collapsed := plainView(m)
	if !strings.Contains(collapsed, "/tools full to expand") {
		t.Fatalf("expected the over-cap result to collapse to a summary line, got:\n%s", collapsed)
	}

	m.toggleLastToolBlock()
	m.refresh()
	expanded := plainView(m)
	if strings.Contains(expanded, "/tools full to expand") {
		t.Fatalf("expected the toggle to expand this card in place, got:\n%s", expanded)
	}
	if !m.toolCompact {
		t.Fatalf("expected the session-wide toolCompact default to stay untouched by a per-block toggle")
	}

	// A second card resolving afterward must still start collapsed — the
	// toggle is per-block, not a session-wide flip.
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "shell", ToolID: "tu_2", ToolInput: json.RawMessage(`{"command":"ls -la"}`)})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "shell", ToolID: "tu_2", ToolResult: manyLines(20)})
	m.refresh()
	got := plainView(m)
	if strings.Count(got, "/tools full to expand") != 1 {
		t.Fatalf("expected only the second (untoggled) card to still be collapsed, got:\n%s", got)
	}

	// Toggling again collapses the first card back.
	m.toolBlocks[0].toggleFull(&m)
	m.refresh()
	got = plainView(m)
	if strings.Count(got, "/tools full to expand") != 2 {
		t.Fatalf("expected toggling back to re-collapse the first card, got:\n%s", got)
	}
}

// TestToolBlockToggle_UpgradedGroupStaysAddressable covers the P74.4/P75.1
// interaction: a solo card that later upgrades into a two-member read/search
// group (foldIntoReadGroup) must hand off its transcript item's addressable
// entry to the group, not leave a stale card entry the toggle can no longer
// reach.
func TestToolBlockToggle_UpgradedGroupStaysAddressable(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("look around", nil)
	m.streaming = true
	m.followBottom = true
	m.toolCompact = true

	in1, _ := json.Marshal(map[string]string{"path": "a.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: in1})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_1", ToolResult: "package a\n"})

	in2, _ := json.Marshal(map[string]string{"path": "b.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_2", ToolInput: in2})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_2", ToolResult: "package b\n"})
	m.refresh()

	if len(m.toolBlocks) != 1 {
		t.Fatalf("expected the pair to collapse into exactly one addressable block, got %d", len(m.toolBlocks))
	}
	if _, isGroup := m.toolBlocks[0].(*toolGroup); !isGroup {
		t.Fatalf("expected the addressable block to be the upgraded group, got %T", m.toolBlocks[0])
	}

	collapsed := plainView(m)
	if !strings.Contains(collapsed, "/tools full to expand") {
		t.Fatalf("expected the group to render collapsed by default, got:\n%s", collapsed)
	}

	m.toggleLastToolBlock()
	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "b.go") {
		t.Fatalf("expected the toggle to expand the group to its per-call listing, got:\n%s", got)
	}
}

// visibleRowContaining returns the index of the first visible pane row whose
// plain text contains needle, or -1.
func visibleRowContaining(m model, needle string) int {
	for i, l := range m.transcript.VisibleLines() {
		if strings.Contains(ansi.Strip(l), needle) {
			return i
		}
	}
	return -1
}

// TestMouseClickTogglesToolBlock is the click-to-expand half of P75.1: a
// left click landing on a resolved tool card's own disclosure icon (the
// narrow "▸ "/"▾ " glyph toggleIcon renders, not anywhere else on the row)
// toggles it in place, reusing the same toolBlocks registry and toggleFull
// the keyboard path (toggleLastToolBlock) already exercises — mirroring
// TestToolBlockToggle_ExpandsOneCardIndependentOfSessionDefault but driven
// through handleMouseClick's hit-testing instead of "last block" addressing.
func TestMouseClickTogglesToolBlock(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("run a command", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "shell", ToolID: "tu_1", ToolInput: json.RawMessage(`{"command":"ls"}`)})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "shell", ToolID: "tu_1", ToolResult: manyLines(20)})
	m.refresh()

	row := visibleRowContaining(m, "/tools full to expand")
	if row < 0 {
		t.Fatalf("expected to find the collapsed summary line, got:\n%s", plainView(m))
	}
	if !strings.HasPrefix(ansi.Strip(m.transcript.VisibleLines()[row]), "▸ ") {
		t.Fatalf("expected the collapsed summary line to start with the ▸ disclosure icon, got:\n%s", m.transcript.VisibleLines()[row])
	}

	// X:1 -> pane-relative col 0 (paneOrigin is (1,0)), landing on the icon
	// glyph itself; Y is pane-relative directly since the origin's row is 0.
	if cmd := m.handleMouseClick(tea.MouseClickMsg{X: 1, Y: row, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("expected a toggle click to return no clipboard command")
	}
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "/tools full to expand") {
		t.Fatalf("expected the click to expand the card in place, got:\n%s", got)
	}
	if len(m.toolBlocks) != 1 || !m.toolBlocks[0].(*toolCard).full {
		t.Fatalf("expected the clicked card's own full state to flip")
	}

	// The expanded card is taller than the pane, so its (now ▾) icon has
	// scrolled above the follow-bottom window — scroll to the top to bring
	// it back on screen before the second click, exactly as a real user
	// would need to (refresh() re-pins to the bottom on every call while
	// followBottom is true, so that has to come off first, same as a real
	// scroll-up would clear it).
	m.followBottom = false
	m.transcript.GotoTop()
	m.refresh()
	row = visibleRowContaining(m, "▾")
	if row < 0 {
		t.Fatalf("expected to find the expanded card's ▾ icon, got:\n%s", plainView(m))
	}

	// A second click on the icon, backdated past the double-click window so
	// it registers as a fresh click count of 1 rather than a double-click,
	// collapses it back.
	m.sel.lastClickAt = m.sel.lastClickAt.Add(-time.Second)
	if cmd := m.handleMouseClick(tea.MouseClickMsg{X: 1, Y: row, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("expected a toggle click to return no clipboard command")
	}
	m.refresh()
	got = plainView(m)
	if !strings.Contains(got, "/tools full to expand") {
		t.Fatalf("expected the second click to collapse the card back, got:\n%s", got)
	}
}

// TestMouseClickOffIconDoesNotToggle guards the actual point of the
// disclosure icon: a click elsewhere on a toggleable card's own row — same
// row, same item, just past the icon's two columns — must not toggle it,
// only a click on the icon itself does.
func TestMouseClickOffIconDoesNotToggle(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("run a command", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "shell", ToolID: "tu_1", ToolInput: json.RawMessage(`{"command":"ls"}`)})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "shell", ToolID: "tu_1", ToolResult: manyLines(20)})
	m.refresh()

	row := visibleRowContaining(m, "/tools full to expand")
	if row < 0 {
		t.Fatalf("expected to find the collapsed summary line, got:\n%s", plainView(m))
	}

	// X:4 -> pane-relative col 3, past the icon's two clickable columns
	// (0 and 1) but still on the same card row.
	m.handleMouseClick(tea.MouseClickMsg{X: 4, Y: row, Button: tea.MouseLeft})
	m.refresh()
	if strings.Count(plainView(m), "/tools full to expand") != 1 {
		t.Fatalf("expected a click off the disclosure icon to leave the card collapsed, got:\n%s", plainView(m))
	}
	if m.toolBlocks[0].(*toolCard).full {
		t.Fatalf("expected a click off the icon to not toggle the card's full state")
	}
}

// TestMouseClickElsewhereStillArmsSelection guards against the toggle branch
// swallowing ordinary clicks: a click on a plain (non-tool-block) transcript
// row must still focus it and arm a drag-select, unchanged from before
// P75.1's mouse slice.
func TestMouseClickElsewhereStillArmsSelection(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("hello there", nil)
	m.refresh()

	row := visibleRowContaining(m, "hello there")
	if row < 0 {
		t.Fatalf("expected to find the user message, got:\n%s", plainView(m))
	}

	m.handleMouseClick(tea.MouseClickMsg{X: 2, Y: row, Button: tea.MouseLeft})
	if !m.sel.active {
		t.Fatalf("expected a plain click to arm a drag-select")
	}
}
