package drive

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/skills"
)

// TestDeepResearchDeclaresAPlan is the P71.8 regression: deep-research's own
// SKILL.md now declares `phases:` (research → synthesize) and a `run_dir:`
// glob, loaded through the real frontmatter parser rather than a hand-built
// PhaseSpec slice, so a future edit to the YAML that breaks parsing or drops
// a required field (a phase with no files, which validPhases silently drops)
// fails here instead of only showing up as "the generic drive ran instead".
func TestDeepResearchDeclaresAPlan(t *testing.T) {
	workDir := t.TempDir()
	if err := skills.MaterializeBuiltinsToProject(workDir, []string{"deep-research"}); err != nil {
		t.Fatal(err)
	}
	sk, ok := skills.Load(workDir, "", []string{"deep-research"}, "deep-research")
	if !ok {
		t.Fatal("deep-research skill not found")
	}
	if sk.RunDir != ".aegis/research/*" {
		t.Errorf("run_dir = %q, want .aegis/research/*", sk.RunDir)
	}
	plan := PlanFor(sk.Name, sk.Phases)
	if plan == nil {
		t.Fatal("deep-research must have a phase plan now that its SKILL.md declares one")
	}
	if len(plan) != 2 {
		t.Fatalf("got %d phases, want 2 (research, synthesize)", len(plan))
	}
	if plan[0].Name() != "research" || !plan[0].setup {
		t.Errorf("phase 0 = %+v, want the research setup phase", plan[0])
	}
	if strings.Join(plan[0].globs, ",") != "findings.md" {
		t.Errorf("research phase globs = %v, want [findings.md]", plan[0].globs)
	}
	if plan[1].Name() != "synthesize" || plan[1].setup {
		t.Errorf("phase 1 = %+v, want the non-setup synthesize phase", plan[1])
	}
	if strings.Join(plan[1].globs, ",") != "report.md" {
		t.Errorf("synthesize phase globs = %v, want [report.md]", plan[1].globs)
	}
	// P73.1: both phases declare a mechanical content gate now, not just the
	// research file's own require_pattern — a report with no [n] markers
	// must not look complete either.
	if plan[0].requirePattern == nil || plan[0].requirePattern.String() != `url:\s*https?://` {
		t.Errorf("research phase requirePattern = %v, want the url: pattern", plan[0].requirePattern)
	}
	if plan[1].requirePattern == nil || plan[1].requirePattern.String() != `(?m)^\d+\.\s.*https?://` {
		t.Errorf("synthesize phase requirePattern = %v, want the numbered-source-with-URL pattern", plan[1].requirePattern)
	}
}

// TestPlanForBuildsFromFrontmatterSpecs is the P52.12 generalization: any skill
// declaring `phases:` gets the phased drive, with no code change and no name
// hard-coded anywhere.
func TestPlanForBuildsFromFrontmatterSpecs(t *testing.T) {
	specs := []skills.PhaseSpec{
		{Name: "outline", Setup: true, Files: []string{"outline.md"}, Prompt: "Draft the outline for {task} in {run_dir}."},
		{Name: "chapters", Files: []string{"ch-*.md"}, Prompt: "Write the chapters."},
	}
	plan := PlanFor("documentation-as-code", specs)
	if len(plan) != 2 {
		t.Fatalf("got %d phases, want 2", len(plan))
	}
	if plan[0].Name() != "outline" || !plan[0].setup {
		t.Errorf("phase 0 = %+v, want the outline setup phase", plan[0])
	}
	if plan[1].setup {
		t.Error("only the first phase may be the setup phase")
	}
	if strings.Join(plan[1].globs, ",") != "ch-*.md" {
		t.Errorf("globs = %v, want [ch-*.md]", plan[1].globs)
	}
}

// TestDeclaredPhasePromptSubstitutesAndGuards checks the two things a declared
// prompt must do beyond echoing its author's text: fill in the run-time
// placeholders, and carry the guardrails (P39.14 incremental edits,
// non-interactive) that a skill author should not have to remember.
func TestDeclaredPhasePromptSubstitutesAndGuards(t *testing.T) {
	spec := skills.PhaseSpec{
		Name:   "chapters",
		Files:  []string{"ch-1.md", "ch-2.md"},
		Prompt: "Task: {task}. Write into {run_dir}; rules in {skill_dir}. Phase {phase} owns {files}.",
	}
	got := declaredPhasePrompt(spec, PhaseParams{
		task: "document the API", skillDir: ".aegis/skills/dac", cwd: "/ws", runDir: "/ws/out",
	})

	for _, want := range []string{
		"Task: document the API.",
		"/ws/out",
		".aegis/skills/dac",
		"Phase chapters owns ch-1.md, ch-2.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q; got:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "never `write_file` a suite file") {
		t.Errorf("declared prompt must carry the incremental-edit guardrail; got:\n%s", got)
	}
	if !strings.Contains(got, "non-interactive run") {
		t.Errorf("declared prompt must carry the non-interactive instruction; got:\n%s", got)
	}
	if strings.Contains(got, "{") {
		t.Errorf("an unsubstituted placeholder survived; got:\n%s", got)
	}
}

// TestDeclaredPhasePromptDefaultsWhenAbsent lets a `phases:` entry be as terse
// as a name plus globs — the drive still has something to seed the context with.
func TestDeclaredPhasePromptDefaultsWhenAbsent(t *testing.T) {
	spec := skills.PhaseSpec{Name: "data-flow", Files: []string{"1-model.md"}}
	got := declaredPhasePrompt(spec, PhaseParams{task: "t", cwd: "/ws", runDir: "/ws/out"})
	if !strings.Contains(got, "data flow") {
		t.Errorf("default prompt must name the phase; got:\n%s", got)
	}
	if !strings.Contains(got, "1-model.md") {
		t.Errorf("default prompt must name the phase's files; got:\n%s", got)
	}
}

// TestBuiltinThreatModelPlanWinsOverFrontmatter pins the one deliberate
// exception. The built-in plan's per-phase prompts are hand-tuned Go functions
// carrying guardrails a frontmatter string cannot express, and every P38.1/P47.x
// live run was tuned against them — an edited SKILL.md silently replacing them
// would be a regression dressed up as a generalization.
func TestBuiltinThreatModelPlanWinsOverFrontmatter(t *testing.T) {
	specs := []skills.PhaseSpec{{Name: "everything", Files: []string{"*.md"}, Prompt: "do it all"}}
	plan := PlanFor("threat-modeling", specs)
	if len(plan) != len(ThreatModelPhases) {
		t.Fatalf("got %d phases, want the built-in plan's %d", len(plan), len(ThreatModelPhases))
	}
	if plan[0].Name() != ThreatModelPhases[0].Name() {
		t.Errorf("first phase = %q, want the built-in %q", plan[0].Name(), ThreatModelPhases[0].Name())
	}
}

// TestDeclaredPlanRoutesContentFailuresByFile proves the file→phase routing
// (the P52.7/P52.8 follow-up) works for a declared plan too — the routing reads
// Phase.globs, which a declared plan populates from `files:`, so it needs no
// per-skill knowledge.
func TestDeclaredPlanRoutesContentFailuresByFile(t *testing.T) {
	plan := PlanFor("documentation-as-code", []skills.PhaseSpec{
		{Name: "outline", Setup: true, Files: []string{"outline.md"}},
		{Name: "chapters", Files: []string{"ch-*.md"}},
	})
	report := "FAIL section-bodies-nonempty\n       - ch-2.md:14  ## \"Design\" section is empty\n\n0 passed, 1 failed"
	ph, ok := ownerPhaseForContentFailure(plan, report)
	if !ok || ph.Name() != "chapters" {
		t.Errorf("routed to %q ok=%v, want chapters", ph.Name(), ok)
	}
}
