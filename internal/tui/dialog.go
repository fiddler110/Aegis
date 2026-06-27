package tui

import (
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// Shared chrome for the overlay dialogs (command palette, persona/session
// pickers) so they read as one cohesive component family — a rounded primary
// frame, a brand title chip, and a left accent bar marking the selection. This
// mirrors Crush's dialog styling (primary frame + accent selection) while
// staying idiomatic to the bubbles list delegate.

// aegisListDelegate returns the shared list delegate styling for overlay
// dialogs: the selected row is marked with a left primary accent bar, normal
// rows use the base / muted foreground tiers.
func aegisListDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colPrimary).
		Foreground(colPrimary).
		Bold(true).
		Padding(0, 0, 0, 1)
	d.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(colSecondary).
		Padding(0, 0, 0, 2)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(colFgBase).
		Padding(0, 0, 0, 2)
	d.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(colFgMost).
		Padding(0, 0, 0, 2)
	return d
}

// configureDialogList applies the chrome common to overlay pickers: a brand
// title chip, hidden status/help bars, and filtering. Pass pagination=true for
// lists long enough to page.
func configureDialogList(l *list.Model, title string, pagination bool) {
	l.Title = title
	l.Styles.Title = lipgloss.NewStyle().
		Background(colBrandBg).
		Foreground(colBrandFg).
		Bold(true).
		Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 0)
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.SetShowPagination(pagination)
	l.SetShowHelp(false)
}

// dialogFrame wraps overlay content in the shared rounded primary border.
func dialogFrame(content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colPrimary).
		Background(colSurface).
		Padding(0, 1).
		Render(content)
}
