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

// sessionPickerH sizes the picker to n rows. Shared by the loading frame and
// the populated one, so opening ahead of the data can't change the shape the
// data settles into.
func sessionPickerH(termH, n int) int { return dialogListH(termH, n, 10) }

// newSessionPicker opens the session switcher on the keypress, before
// ListSessions has answered: it carries one spinner row until
// sessionPickerItems' rows land via setItems (P33.7).
func newSessionPicker(termW, termH int, frame string) listDialog {
	return newLoadingDialog(dialogSessionPicker, min(termW-6, 70), sessionPickerH(termH, 0), "Switch Session", frame)
}

func sessionPickerItems(sessions []api.SessionMeta) []list.Item {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{id: s.ID, title: s.Title, mode: s.Mode, updated: s.UpdatedAt}
	}
	return items
}
