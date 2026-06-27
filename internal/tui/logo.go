package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// aegisShieldLines is the Aegis emblem: the wordmark crowning a shield that
// tapers to a protective point. Each line is 14 display cells wide so callers
// can concatenate aligned content to the right of it. Diagonals use box-drawing
// glyphs (╱╲) rather than ASCII slashes for crisper edges.
var aegisShieldLines = []string{
	`  ╔══════════╗`,
	`  ║  AEGIS   ║`,
	`  ╠══════════╣`,
	`  ║    ╱╲    ║`,
	`  ║   ╱  ╲   ║`,
	`  ║  ╱ ◆  ╲  ║`,
	`  ║ ╱      ╲ ║`,
	`  ╚══╗    ╔══╝`,
	`     ╚════╝   `,
}

// aegisNameLine is the index of the line carrying the AEGIS wordmark.
const aegisNameLine = 1

// renderAegisLogo returns the shield art coloured with a vertical gradient from
// a cool lavender crest down to the brand purple point, with the AEGIS wordmark
// re-rendered in a brighter horizontal gradient so the name pops. Returned as
// one coloured string per source line; visual widths are unchanged (ANSI escape
// codes add no cells), so right-aligned content still lines up.
func renderAegisLogo() []string {
	ramp := lipgloss.Blend1D(len(aegisShieldLines), colGradFrom, colGradTo)
	out := make([]string, len(aegisShieldLines))
	for i, line := range aegisShieldLines {
		frame := lipgloss.NewStyle().Foreground(ramp[i]).Bold(true)
		if i == aegisNameLine {
			before, after, found := strings.Cut(line, "AEGIS")
			if found {
				out[i] = frame.Render(before) +
					gradientText("AEGIS", true, colWordFrom, colWordTo) +
					frame.Render(after)
				continue
			}
		}
		out[i] = frame.Render(line)
	}
	return out
}

// barLabel renders a message role label (e.g. "You", "Assistant") preceded by a
// coloured left bar — Aegis's adaptation of Crush's ▌ message accent. The bar
// sits on the short, non-wrapping label line so it survives transcript re-wrap.
func barLabel(label string, c color.Color) string {
	st := lipgloss.NewStyle().Foreground(c).Bold(true)
	return st.Render("▌ ") + st.Render(label)
}

// renderBrandMark returns the compact title-bar wordmark: an accent bar followed
// by the gradient-blended AEGIS name. It reads as a single high-quality mark
// rather than a flat coloured chip.
func renderBrandMark() string {
	bar := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render("▌ ")
	return bar + gradientText("AEGIS", true, colWordFrom, colWordTo) + " "
}
