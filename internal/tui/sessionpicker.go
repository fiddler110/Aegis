package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"

	"github.com/fiddler110/aegis/internal/api"
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

func newSessionPicker(termW, termH int, sessions []api.SessionMeta, currentID string) listDialog {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{id: s.ID, title: s.Title, mode: s.Mode, updated: s.UpdatedAt}
	}

	palW := min(termW-6, 70)
	palH := min(termH-8, max(len(sessions)*2+6, 10))

	return newListDialog(dialogSessionPicker, palW, palH, "Switch Session", true, items)
}
