package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"

	"github.com/fiddler110/aegis/internal/hwinfo"
	"github.com/fiddler110/aegis/internal/modelcatalog"
)

// modelItem is a single row in the model picker list.
type modelItem struct {
	provider string
	id       string
	tier     modelcatalog.Tier
	context  string
	notes    string
	current  bool
	hwFit    string // P20.3 hardware-fit badge for TierLocal entries; "" when not applicable
}

func (m modelItem) FilterValue() string { return m.provider + " " + m.id + " " + m.notes }
func (m modelItem) Title() string {
	title := m.provider + " · " + m.id
	if m.current {
		title = "● " + title
	}
	return title
}
func (m modelItem) Description() string {
	desc := fmt.Sprintf("%s · ctx %s · %s", m.tier, m.context, m.notes)
	if m.hwFit != "" {
		desc += " · " + m.hwFit
	}
	return desc
}

// hwFitBadge renders a short hardware-fit note for a TierLocal model given
// detected hardware (P20.3), or "" when there's nothing useful to say —
// hardware RAM couldn't be detected, or this model asserts no MinRAMGB
// floor. Qualitative guidance only, mirrors modelcatalog.RecommendLocal.
func hwFitBadge(m modelcatalog.Model, hw hwinfo.Info) string {
	if m.Tier != modelcatalog.TierLocal || m.MinRAMGB == 0 || !hw.RAMKnown() {
		return ""
	}
	if hw.TotalRAMGB() >= float64(m.MinRAMGB) {
		return "fits your ~" + fmt.Sprintf("%.0f", hw.TotalRAMGB()) + "GB RAM"
	}
	return fmt.Sprintf("wants ~%dGB RAM (you have ~%.0fGB)", m.MinRAMGB, hw.TotalRAMGB())
}

// newModelPicker builds the P16.6 model picker from the curated catalog,
// grouped by provider (sorted by provider name, catalog order within each
// provider) with the session's current model marked and pinned first in its
// group. If the current model isn't in the catalog (a custom override), it's
// prepended as its own entry so the picker always reflects what's active.
//
// P20.3: TierLocal entries get a hardware-fit badge in their description
// when internal/hwinfo can detect the machine's RAM. Detection is a cheap,
// local, no-network call (see internal/hwinfo package doc), so it's fine to
// run it fresh every time the picker opens rather than threading it through
// as a parameter.
func newModelPicker(termW, termH int, catalog []modelcatalog.Model, current string) listDialog {
	hw := hwinfo.Detect()
	entries := make([]modelItem, len(catalog))
	for i, m := range catalog {
		entries[i] = modelItem{
			provider: m.Provider,
			id:       m.ID,
			tier:     m.Tier,
			context:  m.Context,
			notes:    m.Notes,
			current:  current != "" && strings.EqualFold(m.ID, current),
			hwFit:    hwFitBadge(m, hw),
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].provider != entries[j].provider {
			return entries[i].provider < entries[j].provider
		}
		// Current model sorts first within its provider group.
		return entries[i].current && !entries[j].current
	})

	foundCurrent := false
	for _, e := range entries {
		if e.current {
			foundCurrent = true
			break
		}
	}
	if current != "" && !foundCurrent {
		entries = append([]modelItem{{provider: "current (custom)", id: current, current: true}}, entries...)
	}

	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = e
	}

	palW := min(termW-6, 74)
	palH := dialogListH(termH, len(items), 12)

	return newListDialog(dialogModelPicker, palW, palH, "Switch Model", true, items)
}
