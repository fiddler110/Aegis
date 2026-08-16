package tui

import "charm.land/bubbles/v2/list"

// paletteItem is a single entry in the command palette list.
type paletteItem struct {
	name string
	desc string
}

func (p paletteItem) FilterValue() string { return "/" + p.name + " " + p.desc }
func (p paletteItem) Title() string       { return "/" + p.name }
func (p paletteItem) Description() string { return p.desc }

// paletteItemsFrom converts command entries into list items for the palette.
func paletteItemsFrom(entries []cmdEntry) []list.Item {
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = paletteItem(e)
	}
	return items
}

func newPalette(termW, termH int, entries []cmdEntry) listDialog {
	palW := min(termW-6, 62)
	palH := min(termH-8, 22)
	// Browse mode by default; typing any character activates filtering naturally.
	return newListDialog(dialogPalette, palW, palH, "Command Palette", false, paletteItemsFrom(entries))
}
