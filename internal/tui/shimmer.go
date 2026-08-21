package tui

import (
	"image/color"
	"math"
	"strings"
	"time"

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

// stallRampColor (P74.11) is the shimmer's highlight color for a wait of the
// given length against bound (Config.MaxTurnStall, the same value the engine
// aborts a stalled turn against): colAccent at zero elapsed, easing toward
// colWarning as elapsed grows. Between "working" and a hard 900s abort the
// old shimmer looked identical at second 2 and second 400; this makes the
// wait itself legible instead of adding a second, separate indicator.
//
// The ramp is deliberately front-loaded — sqrt(t) reaches half-warning at a
// quarter of the way in — and saturates at 70% of bound rather than 100%, so
// a run reads as visibly "getting stuck" well before it actually aborts,
// mirroring the comparison client's continuous red-creep. bound <= 0 (the
// stall bound disabled) or elapsed <= 0 leaves the color at colAccent.
func stallRampColor(elapsed, bound time.Duration) color.Color {
	if bound <= 0 || elapsed <= 0 {
		return colAccent
	}
	const saturateAt = 0.7 // fraction of bound where the ramp hits full colWarning
	t := float64(elapsed) / (float64(bound) * saturateAt)
	if t > 1 {
		t = 1
	}
	t = math.Sqrt(t)
	ramp := lipgloss.Blend1D(101, colAccent, colWarning)
	return ramp[int(t*100)]
}
