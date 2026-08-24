package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

// contextWindowFor returns an approximate context-window size (in tokens) for a
// model, used to render the usage indicator. Values are conservative defaults
// matched on common model-name fragments; unknown models fall back to 128k.
func contextWindowFor(model string) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gemini"):
		return 1_000_000
	case strings.Contains(m, "claude"), strings.Contains(m, "o1"), strings.Contains(m, "o3"):
		return 200_000
	case strings.Contains(m, "gpt-4.1"):
		return 1_000_000
	case strings.Contains(m, "gpt-4o"), strings.Contains(m, "gpt-4"), strings.Contains(m, "llama"), strings.Contains(m, "qwen"):
		return 128_000
	default:
		return 128_000
	}
}

// renderContextBar renders a compact usage meter for the context window:
// a filled/empty bar plus a percentage, coloured green→amber→red as it fills.
func renderContextBar(used, total, width int) string {
	if total <= 0 {
		total = 128_000
	}
	frac := float64(used) / float64(total)
	if frac > 1 {
		frac = 1
	}
	barW := max(width-5, 4) // leave room for " 99%"
	filled := int(frac*float64(barW) + 0.5)

	col := colSuccess
	switch {
	case frac >= 0.9:
		col = colDanger
	case frac >= 0.7:
		col = colWarning
	}
	bar := lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("▰", filled)) +
		lipgloss.NewStyle().Foreground(colBorder).Render(strings.Repeat("▱", barW-filled))
	pct := lipgloss.NewStyle().Foreground(colTextMuted).Render(fmt.Sprintf(" %d%%", int(frac*100+0.5)))
	return bar + pct
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// truncate shortens s to a display width of n cells, appending an ellipsis when
// it overflows. It is width- and rune-aware (and ANSI-aware), so it never slices
// a multi-byte rune in half or miscounts wide glyphs — important because these
// strings feed straight into lipgloss layout.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
