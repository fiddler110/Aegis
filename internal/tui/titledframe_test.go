package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTitledFrameRowsShareOneWidth(t *testing.T) {
	body := "short\na much longer line that runs past the frame's inner width for sure\n\nwide ✓ glyph"
	out := titledFrame("Aegis configuration", "↑↓ choose · enter open · esc discard", body, 40)
	lines := strings.Split(out, "\n")
	want := lipgloss.Width(lines[0])
	for i, l := range lines {
		if w := lipgloss.Width(l); w != want {
			t.Errorf("row %d is %d wide, want %d: %q", i, w, want, ansi.Strip(l))
		}
	}
	plain := ansi.Strip(out)
	if !strings.HasPrefix(plain, "╭─ Aegis configuration ") || !strings.Contains(lines[len(lines)-1], "esc discard") {
		t.Errorf("title or keys not in the edges:\n%s", plain)
	}
}

func TestTitledFrameCutsAnOverlongTitle(t *testing.T) {
	out := titledFrame(strings.Repeat("t", 80), "", "x", 20)
	if w := lipgloss.Width(strings.Split(out, "\n")[0]); w != 22 {
		t.Errorf("top edge width = %d, want 22", w)
	}
}

func TestWizardMenuRendersInTheTitledFrame(t *testing.T) {
	w := newWizard(100, 40, newTheme())
	w.presetLabel, w.modelName = "Anthropic (Claude)", "claude-opus-5-5"
	w.conn = connResult{tested: true, ok: true, status: "✓ 4 models · ANTHROPIC_API_KEY"}
	w.enterMenu()
	view := ansi.Strip(w.view())
	t.Log("\n" + view)
	if !strings.Contains(view, "╭─ Aegis configuration") || !strings.Contains(view, "esc discard") {
		t.Errorf("menu not framed with title and keys:\n%s", view)
	}
}
