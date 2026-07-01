package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// timelineEntry records one user turn for the conversation timeline picker.
type timelineEntry struct {
	text       string    // first line of the user's message
	ts         time.Time // when the turn was sent
	byteOffset int       // byte offset in transcript.buf at the time of writing
}

type timelineItem struct{ e timelineEntry }

func (t timelineItem) FilterValue() string { return t.e.text }
func (t timelineItem) Title() string       { return t.e.text }
func (t timelineItem) Description() string { return t.e.ts.Format("15:04:05") }

type timelinePickerSelectedMsg struct{ entry timelineEntry }
type timelinePickerCancelMsg struct{}

// timelinePickerModel is an overlay for jumping to a past conversation turn.
type timelinePickerModel struct {
	list list.Model
}

func newTimelinePicker(termW, termH int, entries []timelineEntry) timelinePickerModel {
	items := make([]list.Item, len(entries))
	// Show newest first.
	for i, e := range entries {
		items[len(entries)-1-i] = timelineItem{e}
	}

	palW := min(termW-6, 72)
	palH := min(termH-8, max(len(entries)*2+6, 10))

	l := list.New(items, aegisListDelegate(), palW, palH)
	configureDialogList(&l, fmt.Sprintf("Timeline (%d turns)", len(entries)), true)

	return timelinePickerModel{list: l}
}

func (p timelinePickerModel) Update(msg tea.Msg) (timelinePickerModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return p, func() tea.Msg { return timelinePickerCancelMsg{} }
		case "enter":
			if item, ok := p.list.SelectedItem().(timelineItem); ok {
				e := item.e
				return p, func() tea.Msg { return timelinePickerSelectedMsg{entry: e} }
			}
			return p, func() tea.Msg { return timelinePickerCancelMsg{} }
		}
	}
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p timelinePickerModel) View() string {
	return dialogFrame(p.list.View())
}
