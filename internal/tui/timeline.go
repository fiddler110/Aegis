package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
)

// timelineEntry records one user turn for the conversation timeline picker.
type timelineEntry struct {
	text       string    // first line of the user's message
	ts         time.Time // when the turn was sent
	blockIndex int       // transcript block count at the time of writing
}

type timelineItem struct{ e timelineEntry }

func (t timelineItem) FilterValue() string { return t.e.text }
func (t timelineItem) Title() string       { return t.e.text }
func (t timelineItem) Description() string { return t.e.ts.Format("15:04:05") }

func newTimelinePicker(termW, termH int, entries []timelineEntry) listDialog {
	items := make([]list.Item, len(entries))
	// Show newest first.
	for i, e := range entries {
		items[len(entries)-1-i] = timelineItem{e}
	}

	palW := min(termW-6, 72)
	palH := dialogListH(termH, len(entries), 10)

	return newListDialog(dialogTimelinePicker, palW, palH, fmt.Sprintf("Timeline (%d turns)", len(entries)), true, items)
}
