package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
)

// historyItem is one previously sent message, shown in the P22.4 Ctrl+R
// history-search picker. FilterValue backs the list's built-in fuzzy filter
// (configureDialogList enables filtering), which is the actual "search" in
// "prompt-history search" — typing narrows the list incrementally, the same
// interaction as a shell's reverse-i-search.
type historyItem struct{ text string }

func (h historyItem) FilterValue() string { return h.text }
func (h historyItem) Title() string       { return firstLine(h.text) }
func (h historyItem) Description() string {
	if strings.Contains(h.text, "\n") {
		return "(multi-line)"
	}
	return ""
}

// firstLine returns s up to its first newline, so a multi-line history entry
// still renders as one list row.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// newHistoryPicker builds the Ctrl+R input-history search dialog: entries
// newest-first (most recently sent message on top), same convention as
// newTimelinePicker.
func newHistoryPicker(termW, termH int, history []string) listDialog {
	items := make([]list.Item, len(history))
	for i, h := range history {
		items[len(history)-1-i] = historyItem{h}
	}

	palW := min(termW-6, 72)
	palH := min(termH-8, max(len(history)*2+6, 10))

	return newListDialog(dialogHistoryPicker, palW, palH, fmt.Sprintf("Input history (%d)", len(history)), true, items)
}
