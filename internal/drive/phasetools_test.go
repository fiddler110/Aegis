package drive

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/skills"
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

// P62.10: phase 6 — the verify/fix rounds plus the quality pass, typically the
// phase a build spends the most turns in — narrowed nothing at all, so it ran on
// the session's whole surface while every content phase ran on a declared one.
// The list is read off its own prompts: read the suite, fix in place with the
// handle-based editors, never re-scaffold and never write_file a suite file.
func TestPhase6NarrowsToItsOwnSurface(t *testing.T) {
	ph := phase6Phase(ThreatModelPhases)
	if len(ph.tools) == 0 {
		t.Fatal("phase 6 declares no tool surface for the built-in threat-model plan")
	}
	for _, want := range []string{"read_file", "fill_marker", "edit_section", "edit_file", "threat_model_inventory"} {
		if !has(ph.tools, want) {
			t.Errorf("phase 6 is missing %q: %v", want, ph.tools)
		}
	}
	// The two the content phases dropped, for reasons phase 6 shares: a shell
	// call is a detour, a whole-file write clobbers a finished suite, and
	// web_search is the detour that opened a real run (P39.14).
	for _, banned := range []string{"shell", "write_file", "web_search"} {
		if has(ph.tools, banned) {
			t.Errorf("phase 6 offers %q: %v", banned, ph.tools)
		}
	}
	// A re-opened content phase runs inside phase 6's scope, so anything it
	// needs must survive being nested in this list.
	for _, want := range fillPhaseTools {
		if !has(ph.tools, want) {
			t.Errorf("phase 6 does not offer %q, which a re-opened fill phase needs", want)
		}
	}
}

// A plan that never declared per-phase tools must not acquire a threat-model
// surface at its verify round: narrowing is opt-in, and a frontmatter-declared
// skill (deep-research and friends) has opted out of it everywhere else.
func TestPhase6DoesNotNarrowAnUndeclaredPlan(t *testing.T) {
	plan := planFromSpecs([]skills.PhaseSpec{{Name: "gather", Files: []string{"a.md"}}})
	if ph := phase6Phase(plan); len(ph.tools) != 0 {
		t.Errorf("phase 6 narrowed a plan that declares no tools: %v", ph.tools)
	}
	if ph := phase6Phase(nil); len(ph.tools) != 0 {
		t.Errorf("phase 6 narrowed an empty plan: %v", ph.tools)
	}
}

// The narrowing has to actually be applied, not merely declared — the P62.9
// finding was a declared surface that nothing called. runPhasedVerifyAndQuality
// returns immediately when there is no verifier to run, which is enough to
// observe the scope it takes on the way in, and the restore on the way out.
func TestPhase6ScopesToolsOnEntryAndRestores(t *testing.T) {
	var calls [][]string
	restored := 0
	st := &State{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ScopeTools: func(allow []string) func() {
			calls = append(calls, allow)
			return func() { restored++ }
		},
		plan: ThreatModelPhases,
	}
	if err := runPhasedVerifyAndQuality(context.Background(), st); err != nil {
		t.Fatalf("phase 6 with nothing to verify: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected exactly one scope call, got %d: %v", len(calls), calls)
	}
	if !has(calls[0], "threat_model_inventory") || has(calls[0], "web_search") {
		t.Errorf("phase 6 scoped the wrong surface: %v", calls[0])
	}
	if restored != 1 {
		t.Errorf("phase 6 restore count = %d, want 1", restored)
	}
}

// Phase 6's own prompts must not instruct a tool phase 6 does not offer — the
// same contract TestPhasePromptsOnlyNameAvailableTools holds the content phases
// to, applied to the phase that was previously exempt because it narrowed
// nothing.
func TestPhase6PromptsOnlyNameAvailableTools(t *testing.T) {
	offered := map[string]bool{}
	for _, n := range phase6Tools {
		offered[n] = true
	}
	prompts := map[string]string{
		"quality":    QualityReviewPrompt(),
		"verify-fix": VerifyFixPrompt("FAIL count-consistency\n  - 3-findings.md:12  x"),
		"preamble":   phase6Preamble("/w/run", "/w/.aegis/builtin-skills/threat-modeling"),
	}
	for name, prompt := range prompts {
		for _, loc := range promptToolRe.FindAllStringSubmatchIndex(prompt, -1) {
			tn := prompt[loc[2]:loc[3]]
			if !phase6PromptToolNames[tn] {
				continue
			}
			lead := prompt[max(0, loc[0]-40):loc[0]]
			if strings.Contains(lead, "never") || strings.Contains(lead, "not ") || strings.Contains(lead, "instead of") {
				continue // a prohibition is the narrowing agreeing with itself
			}
			if !offered[tn] {
				t.Errorf("the %s prompt names tool %q, which phase 6 does not offer (tools: %v)", name, tn, phase6Tools)
			}
		}
	}
}

// The tool names phase-6 prompts can plausibly contain, so a backticked
// argument or filename is not mistaken for a tool call.
var phase6PromptToolNames = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true, "edit_section": true,
	"fill_marker": true, "multi_edit": true, "ls": true, "glob": true, "grep": true,
	"shell": true, "render_diagram": true, "yaml_validate": true, "web_search": true,
	"web_fetch": true, "threat_model_inventory": true, "threat_model_recon": true,
	"threat_model_scaffold": true,
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
