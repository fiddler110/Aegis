package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// gradientText renders s with a smooth horizontal foreground gradient that ramps
// from `from` to `to` across the string's runes, so the colour walks left→right
// over the text. Adapted from Crush's styles.ForegroundGrad. Intended for short
// display strings (the wordmark, logo art) — not bulk transcript text.
func gradientText(s string, bold bool, from, to color.Color) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) == 1 {
		return lipgloss.NewStyle().Foreground(from).Bold(bold).Render(s)
	}

	ramp := lipgloss.Blend1D(len(runes), from, to)
	var b strings.Builder
	for i, r := range runes {
		b.WriteString(lipgloss.NewStyle().Foreground(ramp[i]).Bold(bold).Render(string(r)))
	}
	return b.String()
}
