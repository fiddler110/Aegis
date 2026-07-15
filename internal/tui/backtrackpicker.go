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

// backtrackPickerH sizes the picker to n rows, the same way sessionPickerH
// does: the loading frame and the populated one must agree.
func backtrackPickerH(termH, n int) int { return dialogListH(termH, n, 10) }

// newBacktrackPicker opens the Esc-Esc backtrack dialog on the second press,
// before its two checkpoint/session round-trips have answered: it carries one
// spinner row until backtrackPickerItems' rows land via setItems (P33.7).
func newBacktrackPicker(termW, termH int, frame string) listDialog {
	return newLoadingDialog(dialogBacktrackPicker, min(termW-6, 72), backtrackPickerH(termH, 0), "Backtrack: edit a previous message", frame)
}

// backtrackPickerItems renders previous user turns newest-first (matching
// ListCheckpoints' own ordering and /rewind's no-arg listing convention).
// Picking an entry forks the conversation up to just before that turn and
// pre-fills the new session's input with its original text so the user can
// edit it before resending (see forkAndSwitchCmd).
func backtrackPickerItems(items []backtrackItem) []list.Item {
	litems := make([]list.Item, len(items))
	for i, it := range items {
		litems[i] = it
	}
	return litems
}
