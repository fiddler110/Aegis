package tui

import (
	"charm.land/bubbles/v2/list"

	"github.com/fiddler110/aegis/internal/api"
)

// personaItem is a single row in the persona picker list.
type personaItem struct {
	name string
	desc string
}

func (p personaItem) FilterValue() string { return p.name + " " + p.desc }
func (p personaItem) Title() string       { return p.name }
func (p personaItem) Description() string { return p.desc }

func newPersonaPicker(termW, termH int, personas []api.PersonaInfo) listDialog {
	items := make([]list.Item, len(personas))
	for i, p := range personas {
		items[i] = personaItem{name: p.Name, desc: p.Description}
	}

	palW := min(termW-6, 62)
	palH := min(termH-8, max(len(personas)*2+6, 10))

	return newListDialog(dialogPersonaPicker, palW, palH, "Select Persona", false, items)
}
