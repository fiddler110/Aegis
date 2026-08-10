package drive

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/skills"
)

// The file tools refuse a write into the materialized built-in skill tree
// (see TestSkillAssetsAreReadOnlyToTools), but the shell tool can still reach
// it — a redirect or a `python -c` has no such gate. The drive repairs that at
// every phase boundary, because a phase starting against corrupted tooling is
// precisely the case that cannot recover on its own: observed live in the
// P38.1 re-test (2026-08-09), where a clobbered recon.py left every subsequent
// `python recon.py` raising SyntaxError with nothing pointing at the cause.
func TestRepairSkillAssetsRestoresClobberedScript(t *testing.T) {
	workDir := t.TempDir()
	if err := skills.MaterializeBuiltinsToProject(workDir, []string{"threat-modeling"}); err != nil {
		t.Fatal(err)
	}
	recon := filepath.Join(workDir, ".aegis", "builtin-skills", "threat-modeling", "recon.py")
	original, err := os.ReadFile(recon)
	if err != nil {
		t.Fatalf("expected threat-modeling to materialize recon.py: %v", err)
	}

	// Exactly what the model did: write the command line it meant to run.
	if err := os.WriteFile(recon, []byte("python recon.py <workdir>"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	st := &State{Cwd: workDir, ErrOut: &out, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	st.repairSkillAssets()

	got, err := os.ReadFile(recon)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("recon.py was not restored:\n got: %.80q\nwant: %.80q", got, original)
	}
	if !strings.Contains(out.String(), "recon.py") {
		t.Errorf("expected the notice to name the restored file, got %q", out.String())
	}

	// A second pass has nothing to do and must stay silent, so a clean drive
	// does not emit a notice at every phase boundary.
	out.Reset()
	st.repairSkillAssets()
	if out.Len() != 0 {
		t.Errorf("expected no notice when nothing is stale, got %q", out.String())
	}
}

// Each phase must narrow the tool surface to its own list and hand back the
// previous exposure, so one phase's narrowing never leaks into the next.
func TestScopeToolsPerPhase(t *testing.T) {
	var calls [][]string
	restored := 0
	st := &State{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ScopeTools: func(allow []string) func() {
			calls = append(calls, allow)
			return func() { restored++ }
		},
	}

	restore := st.scopeTools(Phase{name: "architecture", tools: setupPhaseTools})
	if len(calls) != 1 {
		t.Fatalf("expected one scope call, got %d", len(calls))
	}
	// P39.18: the setup phase runs recon and scaffold through typed tools, not
	// a composed shell command line, so shell is gone from this surface.
	if slices.Contains(calls[0], "shell") {
		t.Error("the setup phase no longer needs shell — recon and scaffold are typed tools")
	}
	for _, want := range []string{"threat_model_recon", "threat_model_scaffold"} {
		if !slices.Contains(calls[0], want) {
			t.Errorf("the setup phase needs %s", want)
		}
	}
	restore()
	if restored != 1 {
		t.Errorf("restore count = %d, want 1", restored)
	}

	// Fill phases drop shell and write_file: the suite already exists, so a
	// shell call is a detour and a whole-file write can only clobber it.
	for _, banned := range []string{"shell", "write_file"} {
		if slices.Contains(fillPhaseTools, banned) {
			t.Errorf("fill phases should not offer %s", banned)
		}
	}
	// The assessment phase is the exception: its completion condition includes
	// inventory.yaml, whose marker is cleared by running the bundled
	// inventory.py. Narrowing that capability away there makes the phase
	// impossible to finish, however well the model fills 0-assessment.md. Since
	// P39.18 the capability is the typed threat_model_inventory tool rather than
	// shell — the sidecar still gets generated, without a composed command line.
	if !slices.Contains(assessmentPhaseTools, "threat_model_inventory") {
		t.Error("the assessment phase needs threat_model_inventory, or it can never complete")
	}
	if slices.Contains(assessmentPhaseTools, "shell") {
		t.Error("the assessment phase no longer needs shell — inventory.py is a typed tool")
	}
	// The tool that makes filling tractable for a weak model must be offered
	// wherever filling happens.
	for _, set := range [][]string{setupPhaseTools, fillPhaseTools, dfdPhaseTools, assessmentPhaseTools} {
		if !slices.Contains(set, "fill_marker") {
			t.Errorf("phase tool set %v is missing fill_marker", set)
		}
	}
	// No phase offers web_search — the detour that opened a real run.
	for _, set := range [][]string{setupPhaseTools, fillPhaseTools, dfdPhaseTools, assessmentPhaseTools} {
		if slices.Contains(set, "web_search") {
			t.Errorf("phase tool set %v offers web_search", set)
		}
	}
}

// A phase with no declared tools, or a host that wired no hook, must behave
// exactly as it did before per-phase narrowing existed.
func TestScopeToolsNoopWhenUnset(t *testing.T) {
	called := false
	st := &State{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ScopeTools: func([]string) func() { called = true; return func() {} },
	}
	st.scopeTools(Phase{name: "x"})() // no tools declared
	if called {
		t.Error("a phase with no tool list still narrowed the surface")
	}

	noHook := &State{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	noHook.scopeTools(Phase{name: "x", tools: fillPhaseTools})() // must not panic
}

// A blank Cwd (a host that never set one) must not panic or touch the disk.
func TestRepairSkillAssetsNoopWithoutCwd(t *testing.T) {
	var out bytes.Buffer
	st := &State{ErrOut: &out, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	st.repairSkillAssets()
	if out.Len() != 0 {
		t.Errorf("expected no output, got %q", out.String())
	}
}

// Both editing guardrails must name the handle-based editors before edit_file.
// They are separate constants used by different prompt paths, and updating only
// one is a live-observed mistake: the phase-6 rule was corrected while the
// hand-tuned phase prompts kept telling the model to use edit_file, so the
// re-opened assessment phase went on failing exactly as before.
func TestEditGuardrailsPreferHandleBasedTools(t *testing.T) {
	for name, rule := range map[string]string{
		"monolithicWriteGuardrail":  monolithicWriteGuardrail,
		"phase6IncrementalEditRule": phase6IncrementalEditRule,
	} {
		for _, want := range []string{"fill_marker", "edit_section"} {
			if !strings.Contains(rule, want) {
				t.Errorf("%s does not mention %s", name, want)
			}
		}
		if i, j := strings.Index(rule, "edit_section"), strings.Index(rule, "edit_file"); i > j {
			t.Errorf("%s names edit_file before edit_section; the handle-based tools should lead", name)
		}
	}

	// The re-entry guardrail is the exception: a hollow resume has no markers,
	// so it must lead with edit_section and never mention fill_marker or
	// PENDING — naming a tool with nothing to target sends the model hunting
	// for markers that do not exist.
	if !strings.Contains(hollowReentryEditGuardrail, "edit_section") {
		t.Error("hollowReentryEditGuardrail does not mention edit_section")
	}
	for _, banned := range []string{"fill_marker", "PENDING"} {
		if strings.Contains(hollowReentryEditGuardrail, banned) {
			t.Errorf("hollowReentryEditGuardrail must not mention %s", banned)
		}
	}
	if i, j := strings.Index(hollowReentryEditGuardrail, "edit_section"), strings.Index(hollowReentryEditGuardrail, "edit_file"); i > j {
		t.Error("hollowReentryEditGuardrail names edit_file before edit_section")
	}
}

// component-name-consistency must route back to the phase that authors the
// analysis file. Closing it means writing a full STRIDE section per missing
// component, which the bounded three-round verify-fix loop cannot do: on a live
// run eleven components stayed missing through every round and the drive
// stopped with an unverified suite.
func TestComponentConsistencyRoutesToAnalysisPhase(t *testing.T) {
	const report = "FAIL component-name-consistency\n" +
		"       - component 'AppState' present in {architecture, model} but missing from {analysis}\n" +
		"       - component 'GatewayDB' present in {architecture, model} but missing from {analysis}\n"

	ph, ok := ownerPhaseForContentFailure(ThreatModelPhases, report)
	if !ok {
		t.Fatal("component-name-consistency did not route to any phase; it falls through to the bounded fix loop")
	}
	if ph.name != "framework-analysis" {
		t.Errorf("routed to %q, want framework-analysis", ph.name)
	}
	// And the re-opened phase must consider itself unfinished while it fails.
	if !phaseHasContentFailure(ph, report) {
		t.Error("the analysis phase should treat an outstanding component gap as incomplete")
	}
}

// promptToolRe finds a backticked tool name in a phase prompt.
var promptToolRe = regexp.MustCompile("`([a-z_]+)`")

// A phase's prompt must not name a tool the phase does not offer. Narrowing is
// sharp: removing a tool a prompt requires produces a phase that loops without
// progress, which reads as model incompetence rather than as configuration.
// That is exactly what happened when shell was narrowed away from the
// assessment phase, whose prompt tells the model to run inventory.py.
func TestPhasePromptsOnlyNameAvailableTools(t *testing.T) {
	// Names that appear in prompts as prose or as script/file references
	// rather than as tool calls.
	notATool := map[string]bool{
		"path": true, "index": true, "key": true, "heading": true, "content": true,
		"mode": true, "run_dir": true, "framework": true, "stride": true,
		"section": true, "python": true, "true": true, "false": true,
	}
	registered := map[string]bool{}
	for _, n := range []string{
		"read_file", "write_file", "edit_file", "edit_section", "fill_marker",
		"multi_edit", "ls", "glob", "grep", "shell", "render_diagram",
		"yaml_validate", "skill", "repomap", "web_search", "web_fetch",
	} {
		registered[n] = true
	}

	for _, ph := range ThreatModelPhases {
		if len(ph.tools) == 0 {
			continue // no narrowing declared: every tool is available
		}
		offered := map[string]bool{}
		for _, n := range ph.tools {
			offered[n] = true
		}
		prompt := ph.promptFn(PhaseParams{
			task: "t", skillDir: "/ws/.aegis/builtin-skills/threat-modeling",
			cwd: "/ws", runDir: "/ws/.aegis/security/threat-model/run-1",
		})
		for _, loc := range promptToolRe.FindAllStringSubmatchIndex(prompt, -1) {
			name := prompt[loc[2]:loc[3]]
			if notATool[name] || !registered[name] {
				continue
			}
			// A prohibition is not a requirement: every fill prompt says "never
			// `write_file` a suite file", which is the narrowing agreeing with
			// itself, not a tool the phase needs.
			lead := prompt[max(0, loc[0]-40):loc[0]]
			if strings.Contains(lead, "never") || strings.Contains(lead, "not ") || strings.Contains(lead, "instead of") {
				continue
			}
			if !offered[name] {
				t.Errorf("phase %q names tool %q in its prompt but does not offer it (tools: %v)", ph.name, name, ph.tools)
			}
		}

		// A prompt that tells the model to run a script needs the shell tool,
		// and says so with "run `python …`" rather than by naming shell — which
		// is why the backticked-name scan above does not catch it. This is the
		// exact shape of the assessment-phase regression: its prompt instructs
		// `python inventory.py <run-dir>`, shell was narrowed away, and the
		// phase became impossible to complete.
		if strings.Contains(prompt, "python ") && !offered["shell"] {
			t.Errorf("phase %q tells the model to run a script but does not offer shell (tools: %v)", ph.name, ph.tools)
		}
	}
}
