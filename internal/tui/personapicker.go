package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	palH := min(termH-8, max(len(personas)*2+6, 10))

	l := list.New(items, delegate, palW, palH)
	l.Title = "Select Persona"
	l.Styles.Title = lipgloss.NewStyle().
		Background(colBrandBg).
		Foreground(colBrandFg).
		Bold(true).
		Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 0)
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)

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
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Background(colSurface).
		Padding(0, 1).
		Render(p.list.View())
}
