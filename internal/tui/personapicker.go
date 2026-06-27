package tui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/scottymacleod/aegis/internal/api"
)

// personaItem is a single row in the persona picker list.
type personaItem struct {
	name string
	desc string
}

func (p personaItem) FilterValue() string { return p.name + " " + p.desc }
func (p personaItem) Title() string       { return p.name }
func (p personaItem) Description() string { return p.desc }

type personaPickerSelectedMsg struct{ name string }
type personaPickerCancelMsg struct{}

// personaPickerModel is the interactive persona selection overlay.
type personaPickerModel struct {
	list list.Model
}

func newPersonaPicker(termW, termH int, personas []api.PersonaInfo) personaPickerModel {
	items := make([]list.Item, len(personas))
	for i, p := range personas {
		items[i] = personaItem{name: p.Name, desc: p.Description}
	}

	palW := min(termW-6, 62)
	palH := min(termH-8, max(len(personas)*2+6, 10))

	l := list.New(items, aegisListDelegate(), palW, palH)
	configureDialogList(&l, "Select Persona", false)

	return personaPickerModel{list: l}
}

func (p personaPickerModel) Update(msg tea.Msg) (personaPickerModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return p, func() tea.Msg { return personaPickerCancelMsg{} }
		case "enter":
			if item, ok := p.list.SelectedItem().(personaItem); ok {
				name := item.name
				return p, func() tea.Msg { return personaPickerSelectedMsg{name: name} }
			}
			return p, func() tea.Msg { return personaPickerCancelMsg{} }
		}
	}
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p personaPickerModel) View() string {
	return dialogFrame(p.list.View())
}
