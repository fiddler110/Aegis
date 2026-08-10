package drive

import (
	"strings"
	"testing"
)

func has(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// P39.18: shell was exposed to the setup phase for exactly three command lines
// — the recon digest, `date`, and the scaffolder — each of which the model had
// to compose as a string. With those as typed tools, shell has no remaining use
// in the setup phase, and leaving it exposed would leave the failure mode
// reachable: an argument error for a bundled script must be structurally
// impossible, not merely corrected.
func TestSetupPhaseUsesTypedSkillToolsInsteadOfShell(t *testing.T) {
	if has(setupPhaseTools, "shell") {
		t.Errorf("setup phase still exposes shell: %v", setupPhaseTools)
	}
	for _, want := range []string{"threat_model_recon", "threat_model_scaffold"} {
		if !has(setupPhaseTools, want) {
			t.Errorf("setup phase is missing %q: %v", want, setupPhaseTools)
		}
	}
}

// The assessment phase kept shell solely to run inventory.py, whose sidecar is
// part of the phase's completion condition. The typed tool replaces that one
// use, so shell goes here too.
func TestAssessmentPhaseUsesTypedInventoryToolInsteadOfShell(t *testing.T) {
	if has(assessmentPhaseTools, "shell") {
		t.Errorf("assessment phase still exposes shell: %v", assessmentPhaseTools)
	}
	if !has(assessmentPhaseTools, "threat_model_inventory") {
		t.Errorf("assessment phase is missing threat_model_inventory: %v", assessmentPhaseTools)
	}
}

// No phase may expose shell any more, and the fill phases must not have gained
// the script tools either — narrowing is the point.
func TestNoThreatModelPhaseExposesShell(t *testing.T) {
	for _, ph := range ThreatModelPhases {
		if has(ph.tools, "shell") {
			t.Errorf("phase %q exposes shell: %v", ph.name, ph.tools)
		}
	}
	for _, ph := range ThreatModelPhases {
		if ph.setup {
			continue
		}
		if has(ph.tools, "threat_model_scaffold") || has(ph.tools, "threat_model_recon") {
			t.Errorf("phase %q exposes a setup-only script tool: %v", ph.name, ph.tools)
		}
	}
}

// The phase prompts must instruct the model in terms of the typed tools; a
// leftover `python …/scaffold.py --framework <name>` line is the exact string
// the model was mis-composing.
func TestPhasePromptsNameTypedToolsNotCommandLines(t *testing.T) {
	p := PhaseParams{task: "model this repo", skillDir: ".aegis/builtin-skills/threat-modeling", cwd: "/w", runDir: "/w/run"}
	arch := phasePromptArchitecture(p)
	for _, banned := range []string{"recon.py", "scaffold.py", "--framework", "`date`"} {
		if strings.Contains(arch, banned) {
			t.Errorf("architecture prompt still mentions %q", banned)
		}
	}
	for _, want := range []string{"threat_model_recon", "threat_model_scaffold"} {
		if !strings.Contains(arch, want) {
			t.Errorf("architecture prompt does not name %q", want)
		}
	}
	assess := phasePromptAssessment(p)
	if strings.Contains(assess, "inventory.py") {
		t.Error("assessment prompt still names a python command line for inventory.py")
	}
	if !strings.Contains(assess, "threat_model_inventory") {
		t.Error("assessment prompt does not name threat_model_inventory")
	}
}
