package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/scottymacleod/aegis/internal/api"
)

// sessionItem is a single row in the session picker list.
type sessionItem struct {
	id      string
	title   string
	mode    string
	updated time.Time
}

func (s sessionItem) FilterValue() string { return s.title + " " + s.id }
func (s sessionItem) Title() string {
	title := s.title
	if title == "" {
		title = "(untitled)"
	}
	return title
}
func (s sessionItem) Description() string {
	return fmt.Sprintf("%s · %s · %s", short(s.id), s.mode, s.updated.Format("2006-01-02 15:04"))
}

type sessionPickerSelectedMsg struct{ id string }
type sessionPickerCancelMsg struct{}

// sessionPickerModel is the interactive session resume/switch overlay.
type sessionPickerModel struct {
	list list.Model
}

func newSessionPicker(termW, termH int, sessions []api.SessionMeta, currentID string) sessionPickerModel {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{id: s.ID, title: s.Title, mode: s.Mode, updated: s.UpdatedAt}
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

	palW := min(termW-6, 70)
	palH := min(termH-8, max(len(sessions)*2+6, 10))

	l := list.New(items, delegate, palW, palH)
	l.Title = "Switch Session"
	l.Styles.Title = lipgloss.NewStyle().
		Background(colBrandBg).
		Foreground(colBrandFg).
		Bold(true).
		Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 0)
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.SetShowPagination(true)
	l.SetShowHelp(false)

	return sessionPickerModel{list: l}
}

func (p sessionPickerModel) Update(msg tea.Msg) (sessionPickerModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return p, func() tea.Msg { return sessionPickerCancelMsg{} }
		case "enter":
			if item, ok := p.list.SelectedItem().(sessionItem); ok {
				id := item.id
				return p, func() tea.Msg { return sessionPickerSelectedMsg{id: id} }
			}
			return p, func() tea.Msg { return sessionPickerCancelMsg{} }
		}
	}
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p sessionPickerModel) View() string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Background(colSurface).
		Padding(0, 1).
		Render(p.list.View())
}
