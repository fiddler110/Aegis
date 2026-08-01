package drive

import (
	"strings"
	"testing"
)

// multiFileVerifyReport is a verify.py report carrying the suite-wide checks
// P52.7 (check 15) and P52.8 (checks 16-19) failing across three different
// suite files at once — the shape that used to fall through to the generic
// verify-fix turn because contentSubstanceChecks could only map a check *name*
// to a phase, and one name covers all seven files.
const multiFileVerifyReport = `$ verify.py <run-dir>
PASS no-skeleton-syntax
FAIL section-bodies-nonempty
       - 0.1-architecture.md:14  ## "Trust Boundaries" section is empty (marker ` + "`ARCH-TB`" + ` removed, nothing written in its place)
       - 2-stride-analysis.md:31  ## "Summary" section is empty (marker ` + "`AN-SUM`" + ` removed, nothing written in its place)
FAIL evidence-cells-cited
       - 2-stride-analysis.md:44  Evidence cell 'internal/server/auth.go' is a bare filename — cite a line number, symbol, or config key (SKILL.md §3)
FAIL no-placeholder-cells
       - 3-findings.md:20  Impact cell 'TBD' is a placeholder, not content
FAIL no-duplicate-header-rows
       - 3-findings.md:12  data row duplicates the table header

1 passed, 4 failed`

// TestFileOwnerPhaseUsesPhaseGlobs pins the mapping the routing rests on:
// Phase.globs is already a file→phase table, so a suite filename resolves
// to its owning phase with no second table to keep in sync — including the
// glob-matched analysis file, whose framework short-name is the model's choice
// at run time.
func TestFileOwnerPhaseUsesPhaseGlobs(t *testing.T) {
	cases := map[string]string{
		"0.1-architecture.md":   "architecture",
		"1.1-model.mmd":         "data-flow-diagram",
		"1-model.md":            "data-flow-diagram",
		"2-stride-analysis.md":  "framework-analysis",
		"2-linddun-analysis.md": "framework-analysis",
		"3-findings.md":         "findings",
		"0-assessment.md":       "assessment",
		"inventory.yaml":        "assessment",
	}
	for file, want := range cases {
		ph, ok := fileOwnerPhase(ThreatModelPhases, file)
		if !ok {
			t.Errorf("%s: no owning phase found", file)
			continue
		}
		if ph.name != want {
			t.Errorf("%s: owner = %q, want %q", file, ph.name, want)
		}
	}
	if _, ok := fileOwnerPhase(ThreatModelPhases, "notes.txt"); ok {
		t.Error("a file no phase's globs claim must not resolve to an owner")
	}
	// A path, not just a basename — evidence lines carry basenames today, but
	// nothing should break if that ever changes.
	if ph, ok := fileOwnerPhase(ThreatModelPhases, "run-2026/3-findings.md"); !ok || ph.name != "findings" {
		t.Errorf("a path must resolve by basename; got %q ok=%v", ph.name, ok)
	}
}

// TestContentFailureFilesParsesEvidence checks the `file:line` extraction that
// makes per-file routing possible, including that it reports each file once, in
// report order, and confines itself to the named check's own block.
func TestContentFailureFilesParsesEvidence(t *testing.T) {
	got := contentFailureFiles(multiFileVerifyReport, "section-bodies-nonempty")
	want := []string{"0.1-architecture.md", "2-stride-analysis.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("files = %v, want %v", got, want)
	}
	if got := contentFailureFiles(multiFileVerifyReport, "no-placeholder-cells"); len(got) != 1 || got[0] != "3-findings.md" {
		t.Errorf("no-placeholder-cells files = %v, want [3-findings.md]", got)
	}
	if got := contentFailureFiles(multiFileVerifyReport, "no-skeleton-syntax"); len(got) != 0 {
		t.Errorf("a PASSing check has no failure evidence, got %v", got)
	}
}

// TestOwnerPhaseRoutesPerFileCheck is the follow-up's whole point: a suite-wide
// check failing on the architecture file must re-open the architecture phase,
// not fall through to the generic fix turn (and not route to findings just
// because findings is the fixed-route phase for the older checks).
func TestOwnerPhaseRoutesPerFileCheck(t *testing.T) {
	report := "FAIL section-bodies-nonempty\n       - 0.1-architecture.md:14  ## \"Trust Boundaries\" section is empty\n\n0 passed, 1 failed"
	ph, ok := ownerPhaseForContentFailure(ThreatModelPhases, report)
	if !ok {
		t.Fatal("a per-file content-substance failure must route to a content phase")
	}
	if ph.name != "architecture" {
		t.Errorf("routed to %q, want architecture (the phase whose globs own the failing file)", ph.name)
	}

	// Same check, different file, different owner.
	report = "FAIL prose-sections-substantive\n       - 0-assessment.md:8  ## \"Executive Summary\" section holds 40 characters\n\n0 passed, 1 failed"
	if ph, ok := ownerPhaseForContentFailure(ThreatModelPhases, report); !ok || ph.name != "assessment" {
		t.Errorf("routed to %q ok=%v, want assessment", ph.name, ok)
	}

	// A per-file failure on a file no phase owns must not route — better the
	// generic fix turn than re-opening an arbitrary phase told to edit only its
	// own files.
	report = "FAIL section-bodies-nonempty\n       - scratch-notes.md:3  ## \"Notes\" section is empty\n\n0 passed, 1 failed"
	if ph, ok := ownerPhaseForContentFailure(ThreatModelPhases, report); ok {
		t.Errorf("an unowned file must not route; got %q", ph.name)
	}
}

// TestPhaseHasContentFailureIsFileScoped guards the re-entry completion oracle
// against the trap suite-wide checks introduce: "section-bodies-nonempty fails"
// is not the same as "it fails on a file this phase owns". Treating them as the
// same would keep a phase re-opening over another phase's file — which its own
// prompt forbids it from editing, so it could never clear.
func TestPhaseHasContentFailureIsFileScoped(t *testing.T) {
	arch, _ := phaseByName(ThreatModelPhases, "architecture")
	dfd, _ := phaseByName(ThreatModelPhases, "data-flow-diagram")
	findings, _ := phaseByName(ThreatModelPhases, "findings")

	if !phaseHasContentFailure(arch, multiFileVerifyReport) {
		t.Error("architecture owns 0.1-architecture.md's section-bodies-nonempty failure")
	}
	if !phaseHasContentFailure(findings, multiFileVerifyReport) {
		t.Error("findings owns 3-findings.md's no-placeholder-cells failure")
	}
	if phaseHasContentFailure(dfd, multiFileVerifyReport) {
		t.Error("the DFD phase owns none of the failing files and must report no content failure")
	}

	// Once the architecture file clears, the architecture phase is done even
	// though the same check still fails elsewhere.
	partial := "FAIL section-bodies-nonempty\n       - 2-stride-analysis.md:31  ## \"Summary\" section is empty\n\n0 passed, 1 failed"
	if phaseHasContentFailure(arch, partial) {
		t.Error("architecture must complete once its own file clears, regardless of other files")
	}
}

// TestReentryEvidenceIsScopedToThePhasesFiles closes the loop at the prompt: the
// re-entry tells the model to "edit only the file(s) this phase owns", so
// handing it another phase's failures in the same message would contradict that
// instruction and invite exactly the cross-phase edits the phased drive exists
// to prevent.
func TestReentryEvidenceIsScopedToThePhasesFiles(t *testing.T) {
	arch, _ := phaseByName(ThreatModelPhases, "architecture")
	prompt := hollowBodyReentryPrompt(arch, "run-2026", ".aegis/skills/threat-modeling", multiFileVerifyReport)

	if !strings.Contains(prompt, "0.1-architecture.md:14") {
		t.Errorf("the phase's own evidence must be named; got:\n%s", prompt)
	}
	for _, foreign := range []string{"2-stride-analysis.md", "3-findings.md"} {
		if strings.Contains(prompt, foreign) {
			t.Errorf("another phase's file %q leaked into the re-entry prompt; got:\n%s", foreign, prompt)
		}
	}
	// The mechanical check is still not the re-entry's business.
	if strings.Contains(prompt, "no-duplicate-header-rows") {
		t.Errorf("a mechanical failure must stay with the generic fix turn; got:\n%s", prompt)
	}
	// A check whose every evidence line was filtered out must not appear as a
	// bare FAIL header with nothing under it.
	if strings.Contains(prompt, "FAIL evidence-cells-cited") {
		t.Errorf("a fully-filtered check must be dropped with its evidence; got:\n%s", prompt)
	}
}

// TestChecksForPhaseIncludesOnlyOwnedPerFileChecks pins the input to the
// evidence extraction: a per-file check counts as the phase's only when it
// actually fails on one of that phase's files.
func TestChecksForPhaseIncludesOnlyOwnedPerFileChecks(t *testing.T) {
	analysis, _ := phaseByName(ThreatModelPhases, "framework-analysis")
	got := strings.Join(checksForPhase(analysis, multiFileVerifyReport), ",")
	for _, want := range []string{"section-bodies-nonempty", "evidence-cells-cited"} {
		if !strings.Contains(got, want) {
			t.Errorf("framework-analysis must own %q (it fails on 2-stride-analysis.md); got %q", want, got)
		}
	}
	if strings.Contains(got, "no-placeholder-cells") {
		t.Errorf("no-placeholder-cells fails only on 3-findings.md and must not be claimed; got %q", got)
	}

	// The fixed-route checks stay unconditional for their own phase — they read
	// one file by construction, so there is no evidence to consult.
	findings, _ := phaseByName(ThreatModelPhases, "findings")
	got = strings.Join(checksForPhase(findings, sampleVerifyReport), ",")
	if !strings.Contains(got, "finding-bodies-nonempty") || !strings.Contains(got, "coverage-matches-related-threats") {
		t.Errorf("findings must always own its two fixed-route checks; got %q", got)
	}
}
