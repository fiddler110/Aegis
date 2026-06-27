package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

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

	palW := min(termW-6, 70)
	palH := min(termH-8, max(len(sessions)*2+6, 10))

	l := list.New(items, aegisListDelegate(), palW, palH)
	configureDialogList(&l, "Switch Session", true)

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
	return dialogFrame(p.list.View())
}
