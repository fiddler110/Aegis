package tui

import "charm.land/lipgloss/v2"

// renderQuitConfirmBox is the P16.6 quit-confirmation dialog shown when the
// user asks to quit (ctrl+c, /quit, /exit) while a turn is streaming — until
// now that path cancelled the run and quit silently with no chance to back
// out.
func renderQuitConfirmBox() string {
	heading := lipgloss.NewStyle().Foreground(colBrandFg).Bold(true).Render("Quit Aegis?")
	body := lipgloss.NewStyle().Foreground(colTextDim).Render("A response is still streaming. It will be interrupted.")
	footer := lipgloss.NewStyle().Foreground(colTextMuted).Render("y/enter: quit anyway    n/esc: keep working")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colDestructive).
		Background(colSurface).
		Padding(1, 3).
		Render(heading + "\n\n" + body + "\n\n" + footer)
}
