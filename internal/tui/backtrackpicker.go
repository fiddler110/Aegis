package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
)

// backtrackItem is one prior user turn, shown in the P22.3 Esc-Esc backtrack
// picker. text is the verbatim original user message where available (looked
// up from the session's own message list at the checkpoint's Seq — see
// fetchBacktrackTargets), falling back to the checkpoint's own (120-rune
// truncated) label when that lookup fails for any reason.
type backtrackItem struct {
	cpID      string
	text      string
	createdAt time.Time
	fileCount int
}

func (b backtrackItem) FilterValue() string { return b.text }
func (b backtrackItem) Title() string       { return firstLine(b.text) }
func (b backtrackItem) Description() string {
	desc := b.createdAt.Format("2006-01-02 15:04:05")
	if b.fileCount > 0 {
		desc += fmt.Sprintf(" · %d file(s)", b.fileCount)
	}
	return desc
}

// newBacktrackPicker builds the Esc-Esc backtrack dialog: previous user turns
// newest-first (matching ListCheckpoints' own ordering and /rewind's no-arg
// listing convention). Picking an entry forks the conversation up to just
// before that turn and pre-fills the new session's input with its original
// text so the user can edit it before resending (see forkAndSwitchCmd).
func newBacktrackPicker(termW, termH int, items []backtrackItem) listDialog {
	litems := make([]list.Item, len(items))
	for i, it := range items {
		litems[i] = it
	}

	palW := min(termW-6, 72)
	palH := min(termH-8, max(len(items)*2+6, 10))

	return newListDialog(dialogBacktrackPicker, palW, palH, "Backtrack: edit a previous message", true, litems)
}
