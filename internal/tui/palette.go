package tui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
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
	palW := min(termW-6, 62)
	palH := min(termH-8, 22)

	l := list.New(paletteItemsFrom(entries), aegisListDelegate(), palW, palH)
	// Browse mode by default; typing any character activates filtering naturally.
	configureDialogList(&l, "Command Palette", false)

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
	return dialogFrame(p.list.View())
}
