package tui

import (
	"charm.land/bubbles/v2/list"
)

// frameworkItem is a single row in the /threat-model framework picker list.
type frameworkItem struct {
	name string
	desc string
}

func (f frameworkItem) FilterValue() string { return f.name + " " + f.desc }
func (f frameworkItem) Title() string       { return f.name }
func (f frameworkItem) Description() string { return f.desc }

// threatModelFrameworks mirrors the framework table in the threat-modeling
// skill (internal/skills/builtin/threat-modeling/SKILL.md §1) so the picker's
// descriptions stay in sync with the guidance the model itself follows once a
// framework is chosen.
var threatModelFrameworks = []frameworkItem{
	{"STRIDE", "6-category threat taxonomy per DFD element — general-purpose default"},
	{"LINDDUN", "7-category privacy threats — PII/regulated privacy contexts"},
	{"PASTA", "Risk-centric, 7-stage, includes attack simulation — enterprise, business-impact traceability"},
	{"Trike", "Requirements/access-control model, explicit risk acceptance — governance-heavy, auditable risk decisions"},
	{"VAST", "Scales across many teams, Agile/DevSecOps cadence — large orgs, many services"},
	{"NIST 800-154", "Data-centric: flow/storage/exposure of sensitive data — compliance, data-protection-anchored assessments"},
}

// newThreatModelFrameworkPicker builds the framework-selection picker shown
// when /threat-model is invoked without a recognized leading framework name —
// forcing the choice up front via a dialog instead of spending a model turn
// asking the same question in chat.
func newThreatModelFrameworkPicker(termW, termH int) listDialog {
	items := make([]list.Item, len(threatModelFrameworks))
	for i, f := range threatModelFrameworks {
		items[i] = f
	}

	palW := min(termW-6, 74)
	palH := dialogListH(termH, len(items), 12)

	return newListDialog(dialogThreatModelPicker, palW, palH, "Select Threat-Modeling Framework", false, items)
}
