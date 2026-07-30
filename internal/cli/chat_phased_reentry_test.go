package cli

import (
	"strings"
	"testing"
)

// sampleVerifyReport is a verify.py report shaped exactly as verifySkillOutputs
// passes it back: the `$ …` command header, PASS/FAIL lines at column 0, indented
// `- …` evidence under each FAIL, and the trailing summary line. It carries one
// content-substance failure (finding-bodies-nonempty) the P47.9 router owns and
// one purely mechanical failure (no-duplicate-header-rows) it does not.
const sampleVerifyReport = `$ verify.py <run-dir>
PASS no-skeleton-syntax
FAIL finding-bodies-nonempty
       - 3-findings.md:42 FIND-01 "Evidence" section is empty
       - 3-findings.md:58 FIND-02 "Remediation" section is empty
PASS threat-coverage-bijection
FAIL no-duplicate-header-rows
       - 3-findings.md:12 data row duplicates the table header 'Threat ID | Finding ID'

3 passed, 2 failed`

// TestGrowingPhaseConvForced is the P47.4 escape-hatch guard: the stateless
// continuation is the default, and only AEGIS_PHASE_CONV=growing restores the
// old accumulate-then-reset behaviour, case-insensitively.
func TestGrowingPhaseConvForced(t *testing.T) {
	t.Setenv("AEGIS_PHASE_CONV", "")
	if growingPhaseConvForced() {
		t.Error("unset env must leave the P47.4 stateless continuation on (growing off)")
	}
	t.Setenv("AEGIS_PHASE_CONV", "growing")
	if !growingPhaseConvForced() {
		t.Error("AEGIS_PHASE_CONV=growing must force the growing conversation")
	}
	t.Setenv("AEGIS_PHASE_CONV", "GROWING")
	if !growingPhaseConvForced() {
		t.Error("the flag must be case-insensitive")
	}
	t.Setenv("AEGIS_PHASE_CONV", "stateless")
	if growingPhaseConvForced() {
		t.Error("only 'growing' forces the pre-P47.4 behaviour")
	}
}

// TestFreshPhaseConv_NudgePrefix is the P47.4 guard that the near-stateless
// continuation carries the P39.7 "act now" nudge onto the fresh context — the
// stall-breaker must survive the per-turn reset, not just the growing-conv path.
func TestFreshPhaseConv_NudgePrefix(t *testing.T) {
	st := &phasedDriveState{system: "SYSTEM PROMPT", taskPrompt: "tm this", skillDir: "/ws/.aegis/builtin-skills/threat-modeling", cwd: "/ws"}
	analysis := threatModelPhases[2]
	pending := []string{"2-stride-analysis.md"}

	withNudge := convSeedText(t, st.freshPhaseConv(analysis, "/ws/run", pending, actNowNudge()))
	if !strings.Contains(withNudge, actNowNudge()) {
		t.Error("a nudged continuation must carry the act-now nudge onto the reset context")
	}
	if !strings.Contains(withNudge, "still contain") {
		t.Error("a content-phase continuation must still carry the continuation prompt after the nudge")
	}
	noNudge := convSeedText(t, st.freshPhaseConv(analysis, "/ws/run", pending, ""))
	if strings.Contains(noNudge, actNowNudge()) {
		t.Error("an empty nudge must not inject the act-now text")
	}
}

// TestFailuresContainCheck matches on the exact `FAIL <check>` line and does not
// false-match a check that only appears in evidence prose or as a PASS.
func TestFailuresContainCheck(t *testing.T) {
	if !failuresContainCheck(sampleVerifyReport, "finding-bodies-nonempty") {
		t.Error("a failing check must be detected")
	}
	if !failuresContainCheck(sampleVerifyReport, "no-duplicate-header-rows") {
		t.Error("the mechanical failure must be detected too")
	}
	if failuresContainCheck(sampleVerifyReport, "no-skeleton-syntax") {
		t.Error("a PASS check must not be reported as a failure")
	}
	if failuresContainCheck(sampleVerifyReport, "coverage-matches-related-threats") {
		t.Error("a check absent from the report must not match")
	}
	if failuresContainCheck("", "finding-bodies-nonempty") {
		t.Error("an empty report contains no failures")
	}
}

// TestOwnerPhaseForContentFailure is the core P47.9 routing decision: a
// content-substance failure resolves to the content phase that owns the file,
// a purely mechanical failure does not route, and the check names are gated to a
// plan that actually has the owning phase.
func TestOwnerPhaseForContentFailure(t *testing.T) {
	ph, ok := ownerPhaseForContentFailure(threatModelPhases, sampleVerifyReport)
	if !ok {
		t.Fatal("finding-bodies-nonempty must route to a content phase")
	}
	if ph.name != "findings" {
		t.Errorf("finding-bodies-nonempty must route to the findings phase, got %q", ph.name)
	}

	// Coverage-consistency also routes to findings (the "by extension" check).
	covReport := "FAIL coverage-matches-related-threats\n       - 3-findings.md:9 T8 mapped to FIND-01 but not in FIND-01 Related Threats\n\n0 passed, 1 failed"
	if ph, ok := ownerPhaseForContentFailure(threatModelPhases, covReport); !ok || ph.name != "findings" {
		t.Errorf("coverage-matches-related-threats must route to findings; got %q ok=%v", ph.name, ok)
	}

	// A purely mechanical failure must NOT route — the generic verify-fix turn
	// handles it in place.
	mechReport := "FAIL no-duplicate-header-rows\n       - 3-findings.md:12 dup\n\n0 passed, 1 failed"
	if _, ok := ownerPhaseForContentFailure(threatModelPhases, mechReport); ok {
		t.Error("a purely mechanical failure must not route to a content phase")
	}

	// A plan without the owning phase never routes, even with the check present.
	stub := []skillPhase{{name: "architecture"}}
	if _, ok := ownerPhaseForContentFailure(stub, sampleVerifyReport); ok {
		t.Error("a plan lacking the owning phase must not route")
	}
}

// TestPhaseHasContentFailure is the re-entry completion oracle: it is true while
// a check the phase owns still fails and false once every one clears (the
// re-entry cannot use the PENDING-marker oracle — a hollow resume has none).
func TestPhaseHasContentFailure(t *testing.T) {
	findings, _ := phaseByName(threatModelPhases, "findings")
	arch, _ := phaseByName(threatModelPhases, "architecture")

	if !phaseHasContentFailure(findings, sampleVerifyReport) {
		t.Error("findings phase must report an outstanding content failure while finding-bodies-nonempty fails")
	}
	// A different phase owns none of these checks.
	if phaseHasContentFailure(arch, sampleVerifyReport) {
		t.Error("a phase that owns none of the failing content checks must report no content failure")
	}
	// Once the content checks pass (only a mechanical one remains), the oracle is
	// satisfied so the re-entry hands back to the phase-6 loop.
	clean := "PASS finding-bodies-nonempty\nPASS coverage-matches-related-threats\nFAIL no-duplicate-header-rows\n       - x\n\n2 passed, 1 failed"
	if phaseHasContentFailure(findings, clean) {
		t.Error("re-entry must complete once every owned content check passes")
	}
}

// TestExtractCheckFailures pulls only the owned check's FAIL block (its evidence)
// out of a mixed report, so the re-entry prompt names the empty sections without
// dumping unrelated mechanical failures at the model.
func TestExtractCheckFailures(t *testing.T) {
	got := extractCheckFailures(sampleVerifyReport, []string{"finding-bodies-nonempty"})
	if !strings.Contains(got, "FAIL finding-bodies-nonempty") {
		t.Errorf("extract must keep the owned FAIL header; got:\n%s", got)
	}
	for _, want := range []string{`FIND-01 "Evidence" section is empty`, `FIND-02 "Remediation" section is empty`} {
		if !strings.Contains(got, want) {
			t.Errorf("extract must keep the owned check's evidence %q; got:\n%s", want, got)
		}
	}
	// The unrelated mechanical block and PASS lines must be excluded.
	for _, unwanted := range []string{"no-duplicate-header-rows", "duplicates the table header", "no-skeleton-syntax", "3 passed"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("extract must drop unrelated line %q; got:\n%s", unwanted, got)
		}
	}
	// Nothing matched → empty, so the caller falls back to the full report.
	if got := extractCheckFailures(sampleVerifyReport, []string{"nonexistent-check"}); got != "" {
		t.Errorf("no matching check must yield empty, got:\n%s", got)
	}
}

// TestHollowBodyReentryPrompt guards the P47.9 re-entry prompt against drift: it
// must orient the fresh context (run dir + SKILL.md), name the specific empty
// sections from the verifier evidence, carry the incremental-edit guardrail, and
// crucially NOT reference `<!-- PENDING -->` markers (a hollow resume has none).
func TestHollowBodyReentryPrompt(t *testing.T) {
	findings, _ := phaseByName(threatModelPhases, "findings")
	runDir := "/ws/.aegis/security/threat-model/stride-app-2026-07-27-1200"
	prompt := hollowBodyReentryPrompt(findings, runDir, "/ws/.aegis/builtin-skills/threat-modeling", sampleVerifyReport)

	for _, want := range []string{runDir, "SKILL.md", "findings phase", `FIND-01 "Evidence" section is empty`, "one section or one row per edit", "do not recompute"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("re-entry prompt missing %q; got:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "<!-- PENDING") {
		t.Error("re-entry prompt must NOT mention PENDING markers — a hollow resume has none")
	}
	// The unrelated mechanical failure must not leak into the focused prompt.
	if strings.Contains(prompt, "no-duplicate-header-rows") {
		t.Error("re-entry prompt must carry only the owned check's evidence, not unrelated failures")
	}
}

// TestPhaseByName covers the small lookup both routers depend on.
func TestPhaseByName(t *testing.T) {
	if ph, ok := phaseByName(threatModelPhases, "findings"); !ok || ph.name != "findings" {
		t.Errorf("phaseByName(findings) = %q ok=%v", ph.name, ok)
	}
	if _, ok := phaseByName(threatModelPhases, "nope"); ok {
		t.Error("phaseByName must report ok=false for an unknown phase")
	}
}
