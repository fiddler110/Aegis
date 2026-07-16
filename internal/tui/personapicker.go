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

// personaPickerH sizes the picker to n rows. Shared by the loading frame and
// the populated one, so opening ahead of the data can't change the shape the
// data settles into (mirrors sessionPickerH/backtrackPickerH, P33.13).
func personaPickerH(termH, n int) int { return dialogListH(termH, n, 10) }

// newPersonaPicker opens the persona switcher on the "/persona" dispatch,
// before ListPersonas has answered: it carries one spinner row until
// personaPickerItems' rows land via setItems. Finishes P33.7 for the one
// other genuinely remote-backed picker — deferred there because opening
// early requires a pre-dispatch hook the generic slash-command path didn't
// have (see dispatchSlash in tui.go).
func newPersonaPicker(termW, termH int, frame string) listDialog {
	return newLoadingDialog(dialogPersonaPicker, min(termW-6, 62), personaPickerH(termH, 0), "Select Persona", frame)
}

func personaPickerItems(personas []api.PersonaInfo) []list.Item {
	items := make([]list.Item, len(personas))
	for i, p := range personas {
		items[i] = personaItem{name: p.Name, desc: p.Description}
	}
	return items
}
