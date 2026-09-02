package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/provider"
)

// TestLoadHistoryUpgradesWriteFileToAccurateDiff is P64.4's replay-side
// check: a stored write_file call/result pair must render the same accurate
// diff a live session ends up with (TestToolCard_WriteFileResultUpgradesToAccurateDiff),
// reading the Presentation payload back from persisted history exactly as
// P64.4's own requirement — "computed once, read back unchanged on replay,
// no I/O at render time" — describes, rather than re-deriving anything from
// disk.
func TestLoadHistoryUpgradesWriteFileToAccurateDiff(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	toolInput, _ := json.Marshal(map[string]string{"path": "a.txt", "content": "line1\nline2\n"})
	presentation, _ := json.Marshal(map[string]string{"old": "line1\nold-line2\n"})

	msgs := []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_1", Name: "write_file", Input: toolInput},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "tu_1", Content: "wrote 12 bytes", Presentation: presentation},
		}},
	}

	m.loadHistory(msgs)

	got := plainView(m)
	if !strings.Contains(got, "old-line2") || !strings.Contains(got, "- ") {
		t.Fatalf("expected replay to render an accurate diff against the persisted prior content, got:\n%s", got)
	}
}

// TestLoadHistoryOrphanedWriteFileStillRenders is the safety net for the
// mechanism above: a write_file tool_use with no matching tool_result in
// stored history (shouldn't happen — repairOrphanedToolUses is supposed to
// prevent it — but P64.4's replay path defers this call's render, so it must
// not silently vanish if that invariant is ever violated) still shows up as
// the ordinary call-time preview.
func TestLoadHistoryOrphanedWriteFileStillRenders(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	toolInput, _ := json.Marshal(map[string]string{"path": "a.txt", "content": "line1\nline2\n"})
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_1", Name: "write_file", Input: toolInput},
		}},
	}

	m.loadHistory(msgs)

	got := plainView(m)
	if !strings.Contains(got, "write_file") {
		t.Fatalf("expected the orphaned call to still render, got:\n%s", got)
	}
}
