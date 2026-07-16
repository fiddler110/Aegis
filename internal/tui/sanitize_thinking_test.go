package tui

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

// Model-generated reasoning reaches the terminal through lipgloss styling
// rather than glamour, so neither mdRender nor remapANSI16 ever sees it
// (P24.20, FIND-17). These cover both display paths for thinking text: the
// streaming dim tail, and the settled collapsible block.

// TestStreamingThinkingIsSanitized drives a thinking event carrying an OSC 52
// clipboard-write and a cursor-move CSI through the real applyEvent/View path.
func TestStreamingThinkingIsSanitized(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true
	m.applyEvent(api.Event{
		Kind: api.KindThinking,
		Text: "I should check\x1b]52;c;ZXZpbA==\x07 the file\x1b[10;5H first",
	})
	m.refresh() // builds the ephemeral tail segment and hands it to SetTail

	view := m.transcript.View()
	if strings.Contains(view, "\x1b]52") {
		t.Error("OSC 52 clipboard sequence survived into the streaming thinking view")
	}
	if strings.Contains(view, "\x1b[10;5H") {
		t.Error("cursor-reposition CSI survived into the streaming thinking view")
	}
	// The prose itself must still be there — sanitizing must not eat the text.
	if !strings.Contains(view, "I should check") {
		t.Error("thinking prose was lost from the streaming view")
	}
}

// TestSettledThinkingBlockIsSanitized covers appendThinkingBlock, the single
// choke point for settled blocks — reached from flushThinking for a live turn
// and from loadHistory for replayed history.
func TestSettledThinkingBlockIsSanitized(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.thinkExpanded = true

	m.appendThinkingBlock("reasoning\x1b]0;pwned\x07 continues\x1b[2J", 3)

	if len(m.thinkEntries) != 1 {
		t.Fatalf("thinkEntries = %d, want 1", len(m.thinkEntries))
	}
	e := m.thinkEntries[0]
	for _, s := range []string{e.expanded, e.collapsed} {
		if strings.Contains(s, "\x1b]0;") {
			t.Error("OSC title-bar sequence survived into a settled thinking block")
		}
		if strings.Contains(s, "\x1b[2J") {
			t.Error("clear-screen CSI survived into a settled thinking block")
		}
	}
	if !strings.Contains(e.expanded, "reasoning") {
		t.Error("thinking prose was lost from the settled block")
	}
}
