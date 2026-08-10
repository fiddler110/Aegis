package drive

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// scaffoldRunDir writes a fake threat-model run directory whose files mirror what
// scaffold.py produces: each named file carries a `<!-- PENDING` marker unless
// listed in cleared. Returns the run-dir path.
func scaffoldRunDir(t *testing.T, cleared map[string]bool) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"0-assessment.md", "0.1-architecture.md", "1.1-model.mmd", "1-model.md",
		"2-stride-analysis.md", "3-findings.md", "inventory.yaml",
	}
	for _, f := range files {
		body := "# " + f + "\n"
		if !cleared[f] {
			body += "<!-- PENDING: some-section -->\n"
		} else {
			body += "real content, no marker\n"
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFileHasPendingMarker(t *testing.T) {
	dir := t.TempDir()
	withMarker := filepath.Join(dir, "a.md")
	without := filepath.Join(dir, "b.md")
	if err := os.WriteFile(withMarker, []byte("x\n<!-- PENDING: k -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(without, []byte("x\ndone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileHasPendingMarker(withMarker) {
		t.Error("expected marker file to report pending")
	}
	if fileHasPendingMarker(without) {
		t.Error("expected clean file to report not pending")
	}
	if fileHasPendingMarker(filepath.Join(dir, "missing.md")) {
		t.Error("missing file must not report pending")
	}
}

// TestPhaseCompletionAndPending exercises the per-phase oracle that advances the
// phased drive: a phase is complete only when every file it owns exists and is
// free of PENDING markers, and pending() reports exactly the still-unfilled owned
// files. Covers the glob-matched analysis phase and the setup phase's empty-runDir
// case (never complete, so it always runs).
func TestPhaseCompletionAndPending(t *testing.T) {
	arch := ThreatModelPhases[0]     // setup, owns 0.1-architecture.md
	dfd := ThreatModelPhases[1]      // 1.1-model.mmd, 1-model.md
	analysis := ThreatModelPhases[2] // 2-*-analysis.md (glob)

	// An empty run dir (nothing scaffolded) is never complete, so setup runs.
	if arch.complete("") {
		t.Error("setup phase must not be complete with an empty run dir")
	}
	if got := arch.pending(""); !reflect.DeepEqual(got, []string{"0.1-architecture.md"}) {
		t.Errorf("empty-runDir pending = %v, want the phase globs", got)
	}

	// Fully scaffolded, nothing filled: every content phase is incomplete.
	all := scaffoldRunDir(t, nil)
	for _, ph := range ThreatModelPhases {
		if ph.complete(all) {
			t.Errorf("phase %q must be incomplete when its files still have markers", ph.name)
		}
	}
	if got := dfd.pending(all); !reflect.DeepEqual(got, []string{"1-model.md", "1.1-model.mmd"}) {
		t.Errorf("dfd pending = %v", got)
	}
	// The analysis phase resolves its file via glob (2-*-analysis.md).
	if got := analysis.pending(all); !reflect.DeepEqual(got, []string{"2-stride-analysis.md"}) {
		t.Errorf("analysis pending = %v, want the glob-matched analysis file", got)
	}

	// Clear the architecture file only: that phase is now complete, others not.
	partial := scaffoldRunDir(t, map[string]bool{"0.1-architecture.md": true})
	if !arch.complete(partial) {
		t.Error("architecture phase must be complete once its file has no marker")
	}
	if analysis.complete(partial) {
		t.Error("analysis phase must still be incomplete")
	}
	if got := arch.pending(partial); len(got) != 0 {
		t.Errorf("completed phase pending = %v, want empty", got)
	}

	// Clear the analysis file: the glob phase is complete.
	analysisDone := scaffoldRunDir(t, map[string]bool{"2-stride-analysis.md": true})
	if !analysis.complete(analysisDone) {
		t.Error("analysis phase must be complete once the glob-matched file has no marker")
	}
}

func TestPhasePlanFor(t *testing.T) {
	if p := PlanFor("threat-modeling", nil); p == nil {
		t.Fatal("threat-modeling must have a phase plan")
	}
	if p := PlanFor("deep-research", nil); p != nil {
		t.Error("a skill declaring no phases must fall back to the generic drive")
	}
	if p := PlanFor("", nil); p != nil {
		t.Error("empty skill must have no phase plan")
	}
	// The plan must lead with the setup phase and cover the five content files
	// in dependency order (architecture → DFD → analysis → findings → assessment).
	plan := PlanFor("threat-modeling", nil)
	if !plan[0].setup {
		t.Error("first phase must be the setup phase")
	}
	for _, ph := range plan[1:] {
		if ph.setup {
			t.Errorf("only the first phase may be a setup phase; %q is not first but is marked setup", ph.name)
		}
	}
	wantOrder := []string{"architecture", "data-flow-diagram", "framework-analysis", "findings", "assessment"}
	var gotOrder []string
	for _, ph := range plan {
		gotOrder = append(gotOrder, ph.name)
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("phase order = %v, want %v", gotOrder, wantOrder)
	}
}

func TestLinearDriveForced(t *testing.T) {
	t.Setenv("AEGIS_SKILL_DRIVE", "")
	if LinearForced() {
		t.Error("unset env must not force linear drive")
	}
	t.Setenv("AEGIS_SKILL_DRIVE", "linear")
	if !LinearForced() {
		t.Error("AEGIS_SKILL_DRIVE=linear must force the generic drive")
	}
	t.Setenv("AEGIS_SKILL_DRIVE", "LINEAR")
	if !LinearForced() {
		t.Error("the flag must be case-insensitive")
	}
	t.Setenv("AEGIS_SKILL_DRIVE", "phased")
	if LinearForced() {
		t.Error("only 'linear' forces the generic drive")
	}
}

// TestPhasePromptsAreWired guards the per-phase prompts against silent drift: each
// must orient a fresh context by naming the run directory (for content phases) and
// pointing at the skill's on-disk assets by their real relative paths, since the
// fresh context has no memory of prior phases.
func TestPhasePromptsAreWired(t *testing.T) {
	skillDir := "/ws/.aegis/builtin-skills/threat-modeling"
	runDir := "/ws/.aegis/security/threat-model/stride-app-2026-07-24-1200"
	p := PhaseParams{task: "threat model this repo", skillDir: skillDir, cwd: "/ws", runDir: runDir}

	arch := phasePromptArchitecture(p)
	for _, want := range []string{"threat_model_recon", "threat_model_scaffold", "0.1-architecture.md", "output-formats.md", "threat model this repo", "stride"} {
		if !strings.Contains(arch, want) {
			t.Errorf("architecture prompt missing %q", want)
		}
	}

	for name, prompt := range map[string]string{
		"dfd":        phasePromptDFD(p),
		"analysis":   phasePromptAnalysis(p),
		"findings":   phasePromptFindings(p),
		"assessment": phasePromptAssessment(p),
	} {
		if !strings.Contains(prompt, runDir) {
			t.Errorf("%s prompt must name the run directory so a fresh context can find the files", name)
		}
	}

	if !strings.Contains(phasePromptAssessment(p), "threat_model_inventory") {
		t.Error("assessment prompt must tell the model to call threat_model_inventory to clear inventory.yaml's marker")
	}
	if !strings.Contains(phase6Preamble(runDir, skillDir), runDir) {
		t.Error("phase-6 preamble must name the run directory")
	}
}

// TestContentPromptsSuppressSelfVerification (P47.3) guards the anti-self-verify
// instruction against silent drift. On the 2026-07-24 run both context overflows
// were driven by the model re-auditing already-filled files and recomputing
// coverage arithmetic across dozens of in-phase turns — work the deterministic
// phase-6 verifier owns. The two large content-phase seeds (analysis, findings)
// and the shared in-phase continuation prompt must all carry the instruction;
// the short DFD/assessment seeds deliberately do not.
func TestContentPromptsSuppressSelfVerification(t *testing.T) {
	p := PhaseParams{
		task:     "threat model this repo",
		skillDir: "/ws/.aegis/builtin-skills/threat-modeling",
		cwd:      "/ws",
		runDir:   "/ws/.aegis/security/threat-model/stride-app-2026-07-24-1200",
	}
	carriers := map[string]string{
		"analysis": phasePromptAnalysis(p),
		"findings": phasePromptFindings(p),
		"continue": phaseContinuePrompt(ThreatModelPhases[2], []string{"2-stride-analysis.md"}),
	}
	for name, prompt := range carriers {
		if !strings.Contains(prompt, noSelfVerifyInstruction) {
			t.Errorf("%s prompt must carry the no-self-verify instruction (P47.3)", name)
		}
	}
	// The instruction must point at the mechanical verifier as the authority so
	// the model knows what it is deferring to, not just that it should stop.
	for _, want := range []string{"verify.py", "phase-6"} {
		if !strings.Contains(noSelfVerifyInstruction, want) {
			t.Errorf("no-self-verify instruction should reference %q", want)
		}
	}
}

// TestContentPromptsForbidMonolithicWrites guards the anti-monolithic-write
// instruction against silent drift. On the 2026-07-30 FirewallRiskRater run the
// findings phase died because the model wrote the entire 3-findings.md in one
// `write_file` call, whose arguments truncated at the context ceiling into a
// malformed tool call. Every content-phase seed that authors a large file
// (findings, assessment) and the shared in-phase continuation prompt must tell
// the model to edit incrementally and never `write_file` a whole suite file; the
// analysis seed carries its own inline copy of the same rule.
func TestContentPromptsForbidMonolithicWrites(t *testing.T) {
	p := PhaseParams{
		task:     "threat model this repo",
		skillDir: "/ws/.aegis/builtin-skills/threat-modeling",
		cwd:      "/ws",
		runDir:   "/ws/.aegis/security/threat-model/stride-app-2026-07-24-1200",
	}
	carriers := map[string]string{
		"findings":   phasePromptFindings(p),
		"assessment": phasePromptAssessment(p),
		"continue":   phaseContinuePrompt(ThreatModelPhases[3], []string{"3-findings.md"}),
		"analysis":   phasePromptAnalysis(p),
	}
	for name, prompt := range carriers {
		low := strings.ToLower(prompt)
		if !strings.Contains(low, "write_file") {
			t.Errorf("%s prompt must forbid a monolithic `write_file` of a suite file", name)
		}
		if !strings.Contains(low, "truncat") {
			t.Errorf("%s prompt must explain that a monolithic write truncates on a small context window", name)
		}
	}
	// The shared constant is what findings/assessment/continue carry; keep it
	// pointed at the concrete failure so its intent survives edits.
	for _, want := range []string{"write_file", "edit_file", "truncat"} {
		if !strings.Contains(strings.ToLower(monolithicWriteGuardrail), want) {
			t.Errorf("monolithicWriteGuardrail should reference %q", want)
		}
	}
}
