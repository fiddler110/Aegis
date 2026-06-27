package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// shimmerWindow is the half-width (in cells) of the bright band that sweeps
// across shimmer text. A wider window makes a softer, longer pulse.
const shimmerWindow = 5

// shimmerText renders s as a "working" pulse: a bright band travels left to
// right across the glyphs, fading back to a dim base on either side. step
// advances the band one cell per call (drive it from a spinner tick). This is
// Aegis's lightweight take on Crush's animated gradient working indicator —
// same moving-gradient feel, built on lipgloss.Blend1D with no extra deps.
func shimmerText(s string, step int, base, hi color.Color) string {
	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return ""
	}
	// ramp[0]=base … ramp[shimmerWindow]=hi; index by distance from the band.
	ramp := lipgloss.Blend1D(shimmerWindow+1, base, hi)

	// The band sweeps the full width plus a lead-in/out so it enters and exits
	// cleanly rather than popping at the edges.
	period := n + shimmerWindow*2
	head := step%period - shimmerWindow

	var b strings.Builder
	for i, r := range runes {
		d := i - head
		if d < 0 {
			d = -d
		}
		idx := shimmerWindow - d
		if idx < 0 {
			idx = 0
		}
		b.WriteString(lipgloss.NewStyle().Foreground(ramp[idx]).Bold(true).Render(string(r)))
	}
	return b.String()
}
