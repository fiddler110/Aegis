package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paletteItem is a single entry in the command palette list.
type paletteItem struct {
	name string
	desc string
}

func (p paletteItem) FilterValue() string { return "/" + p.name + " " + p.desc }
func (p paletteItem) Title() string       { return "/" + p.name }
func (p paletteItem) Description() string { return p.desc }

// paletteSelectedMsg is emitted when the user picks a command.
type paletteSelectedMsg struct{ name string }

// paletteCancelMsg is emitted when the user closes without picking.
type paletteCancelMsg struct{}

// paletteItemsFrom converts command entries into list items for the palette.
func paletteItemsFrom(entries []cmdEntry) []list.Item {
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = paletteItem{name: e.name, desc: e.desc}
	}
	return items
}

// paletteModel is the command palette overlay.
type paletteModel struct {
	list list.Model
}

func newPalette(termW, termH int, entries []cmdEntry) paletteModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colAccent).
		Foreground(colAccent).
		Bold(true).
		Padding(0, 0, 0, 1)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(colTextDim).
		Padding(0, 0, 0, 2)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(colTextDim).
		Padding(0, 0, 0, 2)
	delegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(colTextMuted).
		Padding(0, 0, 0, 2)

	palW := min(termW-6, 62)
	palH := min(termH-8, 22)

	l := list.New(paletteItemsFrom(entries), delegate, palW, palH)
	l.Title = "Command Palette"
	l.Styles.Title = lipgloss.NewStyle().
		Background(colBrandBg).
		Foreground(colBrandFg).
		Bold(true).
		Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 0)
	l.SetFilteringEnabled(true)
	// Start in browse mode; typing any character activates filtering naturally.
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)

	return paletteModel{list: l}
}

func (p paletteModel) Update(msg tea.Msg) (paletteModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return p, func() tea.Msg { return paletteCancelMsg{} }
		case "enter":
			if item, ok := p.list.SelectedItem().(paletteItem); ok {
				return p, func() tea.Msg { return paletteSelectedMsg{name: item.name} }
			}
			return p, func() tea.Msg { return paletteCancelMsg{} }
		}
	}
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p paletteModel) View() string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Background(colSurface).
		Padding(0, 1).
		Render(p.list.View())
}
