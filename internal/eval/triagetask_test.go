package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The correct fixes for the two FIXME issues, applied by the tests below to
// stand in for a competent agent. They are written as exact replacements so a
// change to the fixture that silently invalidates them fails loudly here rather
// than quietly turning the "perfect run" test into a weaker assertion.
const (
	sqlVulnerable = `    query = "SELECT id, owner, body FROM records WHERE owner = '%s'" % owner
    rows = [dict(r) for r in conn.execute(query).fetchall()]`
	sqlFixed = `    query = "SELECT id, owner, body FROM records WHERE owner = ?"
    rows = [dict(r) for r in conn.execute(query, (owner,)).fetchall()]`

	traversalVulnerable = `    path = os.path.join(UPLOAD_ROOT, name)`
	traversalFixed      = `    root = os.path.abspath(UPLOAD_ROOT)
    path = os.path.abspath(os.path.join(root, name))
    if os.path.commonpath([root, path]) != root:
        raise ValueError("path escapes the upload root")`
)

// materializeTriage writes the fixture into a fresh temp dir.
func materializeTriage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := TriageTask().Materialize(dir); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	return dir
}

func patch(t *testing.T, dir, name, old, new string) {
	t.Helper()
	path := filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain the expected anchor:\n%s", name, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// applyCorrectFixes plays the part of an agent that got both remediations right.
func applyCorrectFixes(t *testing.T, dir string) {
	t.Helper()
	patch(t, dir, "store.py", sqlVulnerable, sqlFixed)
	patch(t, dir, "files.py", traversalVulnerable, traversalFixed)
}

// writeFindings emits a findings.json from a list of "file:category" pairs.
func writeFindings(t *testing.T, dir string, pairs ...string) {
	t.Helper()
	type entry struct {
		File     string `json:"file"`
		Category string `json:"category"`
		Severity string `json:"severity"`
	}
	list := make([]entry, 0, len(pairs))
	for _, p := range pairs {
		file, cat, ok := strings.Cut(p, ":")
		if !ok {
			t.Fatalf("bad pair %q", p)
		}
		list = append(list, entry{File: file, Category: cat, Severity: "high"})
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "findings.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func allPlanted() []string {
	out := make([]string, 0, len(triageIssues))
	for _, i := range triageIssues {
		out = append(out, i.File+":"+i.Category)
	}
	return out
}

func scoreOf(t *testing.T, score TaskScore, id string) Criterion {
	t.Helper()
	for _, c := range score.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no criterion %q in:\n%s", id, score.Table())
	return Criterion{}
}

// TestTriageFixtureStartsVulnerable is the instrument check, and it is the most
// important test in this file: the P62.2 fixture shipped twice in a state where
// it measured nothing while looking green, and both times the thing that caught
// it was an assertion about *mechanism* rather than about the reported number.
// The two security tests must fail before an agent touches anything — otherwise
// every model "fixes" the code by doing nothing — and the two functional tests
// must pass, or the regression criterion can never be lost and is not a
// criterion at all.
func TestTriageFixtureStartsVulnerable(t *testing.T) {
	py := findInterpreter(t)
	dir := materializeTriage(t)

	results, detail := runTriageTests(dir, py)
	if detail != "" {
		t.Fatalf("the fixture's own test suite does not run: %s", detail)
	}
	for name, want := range map[string]bool{
		"test_find_by_owner_resists_injection":        false,
		"test_read_upload_refuses_traversal":          false,
		"test_find_by_owner_returns_that_owners_rows": true,
		"test_read_upload_reads_a_real_file":          true,
	} {
		got, ok := results[name]
		if !ok {
			t.Errorf("%s did not run at all", name)
			continue
		}
		if got != want {
			t.Errorf("%s = pass:%v, want pass:%v — the fixture is not in its designed starting state", name, got, want)
		}
	}
}

// TestTriagePerfectRunScoresFull pins the ceiling. A rubric nobody can max out
// is a rubric with a broken criterion in it.
func TestTriagePerfectRunScoresFull(t *testing.T) {
	py := findInterpreter(t)
	dir := materializeTriage(t)
	applyCorrectFixes(t, dir)
	writeFindings(t, dir, allPlanted()...)

	score := TriageTask().Grade(dir, py)
	if score.Earned != score.Possible {
		t.Errorf("a perfect run scored %d/%d, want full marks:\n%s", score.Earned, score.Possible, score.Table())
	}
	if score.Possible != 12 {
		t.Errorf("rubric is worth %d points, want 12", score.Possible)
	}
	if outcome := TriageTask().Outcome(dir, py); !outcome.Passed {
		t.Errorf("a perfect run did not pass: %v", outcome.Failures)
	}
}

// TestTriageFloorIsAboveZero is the reason this task exists. Measured
// 2026-08-17, five of six runs on the seeded-bug task scored zero by giving up
// after one tool call, which makes the arms indistinguishable no matter how
// many runs are taken. A model that does the least useful possible thing here —
// greps out the committed credential and stops — must still land on the scale.
func TestTriageFloorIsAboveZero(t *testing.T) {
	py := findInterpreter(t)
	dir := materializeTriage(t)
	writeFindings(t, dir, "config.py:hardcoded_credential")

	score := TriageTask().Grade(dir, py)
	if score.Earned == 0 {
		t.Fatalf("the laziest useful run scored zero — the task still bottoms out:\n%s", score.Table())
	}
	if got := scoreOf(t, score, "found_hardcoded_credential"); got.Earned != 1 {
		t.Errorf("found_hardcoded_credential = %d, want 1", got.Earned)
	}
	// It must not accidentally earn what it did not do.
	if got := scoreOf(t, score, "fixed_sql_injection"); got.Earned != 0 {
		t.Errorf("fixed_sql_injection = %d, want 0 — nothing was fixed", got.Earned)
	}
}

// TestTriageDiscoveryAndRemediationAreIndependent is the property that makes
// the rubric more than a boolean: two runs can score the same total for
// completely different reasons, and the criteria must separate them.
func TestTriageDiscoveryAndRemediationAreIndependent(t *testing.T) {
	py := findInterpreter(t)

	auditor := materializeTriage(t) // finds everything, fixes nothing
	writeFindings(t, auditor, allPlanted()...)
	auditorScore := TriageTask().Grade(auditor, py)

	fixer := materializeTriage(t) // fixes everything, reports nothing
	applyCorrectFixes(t, fixer)
	fixerScore := TriageTask().Grade(fixer, py)

	if got := scoreOf(t, auditorScore, "fixed_sql_injection"); got.Earned != 0 {
		t.Errorf("audit-only run earned a remediation point: %+v", got)
	}
	if got := scoreOf(t, fixerScore, "found_sql_injection"); got.Earned != 0 {
		t.Errorf("fix-only run earned a discovery point without reporting: %+v", got)
	}
	if got := scoreOf(t, fixerScore, "fixed_sql_injection"); got.Earned != 1 {
		t.Errorf("fix-only run did not earn its remediation point: %+v", got)
	}
	if auditorScore.Earned == 0 || fixerScore.Earned == 0 {
		t.Errorf("both partial strategies must score: auditor=%d fixer=%d", auditorScore.Earned, fixerScore.Earned)
	}
}

// TestTriagePrecisionPunishesOverReporting closes the obvious exploit: if only
// discovery were scored, the winning strategy would be to name every file in
// every category without reading anything.
func TestTriagePrecisionPunishesOverReporting(t *testing.T) {
	py := findInterpreter(t)
	dir := materializeTriage(t)
	// Everything true, plus two inventions — including one against wire.py,
	// which is clean on purpose.
	writeFindings(t, dir, append(allPlanted(),
		"wire.py:sql_injection",
		"config.py:path_traversal",
	)...)

	score := TriageTask().Grade(dir, py)
	precision := scoreOf(t, score, "precision")
	if precision.Earned != 0 {
		t.Errorf("precision = %d/2 with two false positives, want 0:\n%s", precision.Earned, score.Table())
	}
	if !strings.Contains(precision.Detail, "wire.py") {
		t.Errorf("precision detail should name the spurious finding, got %q", precision.Detail)
	}

	// One false positive costs one point, not both — the penalty has to be
	// graduated or it is just another boolean.
	dir2 := materializeTriage(t)
	writeFindings(t, dir2, append(allPlanted(), "wire.py:sql_injection")...)
	if got := scoreOf(t, TriageTask().Grade(dir2, py), "precision"); got.Earned != 1 {
		t.Errorf("precision with one false positive = %d, want 1", got.Earned)
	}
}

// TestTriageDetectsTestTampering covers the cheat that would otherwise be
// cheapest: the remediation points are read out of a test suite, so editing the
// suite is the direct route to them.
func TestTriageDetectsTestTampering(t *testing.T) {
	py := findInterpreter(t)
	dir := materializeTriage(t)
	// Neuter the security tests without fixing anything.
	if err := os.WriteFile(filepath.Join(dir, "test_security.py"),
		[]byte("def test_find_by_owner_resists_injection():\n    pass\n\n\ndef test_read_upload_refuses_traversal():\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	score := TriageTask().Grade(dir, py)
	if got := scoreOf(t, score, "no_cheat"); got.Earned != 0 {
		t.Errorf("no_cheat = %d/2 after rewriting the test suite, want 0", got.Earned)
	}
	// And the restored suite must be what remediation was graded against.
	if got := scoreOf(t, score, "fixed_sql_injection"); got.Earned != 0 {
		t.Errorf("fixed_sql_injection = %d after a tampered suite, want 0 — grading used the agent's own tests", got.Earned)
	}
	if _, err := os.Stat(filepath.Join(dir, "test_security.py")); err != nil {
		t.Errorf("grading should have restored the protected file: %v", err)
	}
}

// TestTriagePunishesFixingByDeletion is the other half of the cheat surface: a
// path-traversal check that refuses everything, or a query that returns nothing,
// passes the security test and breaks the product.
func TestTriagePunishesFixingByDeletion(t *testing.T) {
	py := findInterpreter(t)
	dir := materializeTriage(t)
	patch(t, dir, "files.py", traversalVulnerable,
		"    raise ValueError(\"uploads are disabled\")\n    path = os.path.join(UPLOAD_ROOT, name)")

	score := TriageTask().Grade(dir, py)
	if got := scoreOf(t, score, "fixed_path_traversal"); got.Earned != 1 {
		t.Errorf("refusing everything should still pass the security test, got %+v", got)
	}
	if got := scoreOf(t, score, "no_regression"); got.Earned != 0 {
		t.Errorf("no_regression = %d, want 0 — the feature was deleted, not secured", got.Earned)
	}
}

// TestTriageBrokenProjectIsChargedOnceAndDiagnosed is a regression from the
// first live run of this task: `aegis-qwen35-9b:32k` scored a clean 7/7 on
// discovery and precision, then left the project unable to import, and all
// three run-dependent criteria printed one truncated "Traceback (most recent
// call last):" between them — a rubric reporting the same non-diagnosis three
// times. Breaking the code must still cost the regression point, but it is
// charged once and must name what broke.
func TestTriageBrokenProjectIsChargedOnceAndDiagnosed(t *testing.T) {
	py := findInterpreter(t)
	dir := materializeTriage(t)
	// A syntax error in a module both suites import.
	if err := os.WriteFile(filepath.Join(dir, "store.py"), []byte("def find_by_owner(\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFindings(t, dir, allPlanted()...)

	score := TriageTask().Grade(dir, py)

	regression := scoreOf(t, score, "no_regression")
	if regression.Earned != 0 {
		t.Errorf("no_regression = %d, want 0 — the project does not import", regression.Earned)
	}
	if strings.HasPrefix(regression.Detail, "Traceback") || !strings.Contains(regression.Detail, "Error") {
		t.Errorf("no_regression detail must name the actual error, got %q", regression.Detail)
	}
	// The remediation criteria say they were not evaluated rather than
	// repeating the traceback, so the table reads as one cause not three.
	for _, id := range []string{"fixed_sql_injection", "fixed_path_traversal"} {
		c := scoreOf(t, score, id)
		if c.Earned != 0 {
			t.Errorf("%s = %d, want 0", id, c.Earned)
		}
		if !strings.Contains(c.Detail, "not evaluated") {
			t.Errorf("%s detail = %q, want a 'not evaluated' note rather than a repeated traceback", id, c.Detail)
		}
	}
	// Discovery is unaffected: the audit half of the task was done correctly,
	// and a rubric that let a broken build erase it would be back to measuring
	// one thing.
	if got := scoreOf(t, score, "found_sql_injection"); got.Earned != 1 {
		t.Errorf("found_sql_injection = %d, want 1 — discovery must survive a broken build", got.Earned)
	}
}

// TestTriageFindingsParsingIsForgivingAboutShapeNotSubstance: a model that
// writes ./store.py, or "SQL Injection", has identified the right thing and
// must not be marked down for formatting. A model that names the wrong file
// must be.
func TestTriageFindingsParsing(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) map[finding]bool {
		if err := os.WriteFile(filepath.Join(dir, "findings.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, _ := readFindings(dir)
		return got
	}

	got := write(`[{"file":"./store.py","category":"SQL Injection"},{"file":"src\\files.py","category":"path-traversal"}]`)
	if !got[finding{"store.py", "sql_injection"}] {
		t.Errorf("path and category normalization failed for store.py: %v", got)
	}
	if !got[finding{"files.py", "path_traversal"}] {
		t.Errorf("path and category normalization failed for files.py: %v", got)
	}

	got = write(`{"findings":[{"file":"config.py","category":"hardcoded_credential"}]}`)
	if !got[finding{"config.py", "hardcoded_credential"}] {
		t.Errorf("wrapped-object findings.json was not read: %v", got)
	}

	if _, detail := func() (map[finding]bool, string) {
		if err := os.WriteFile(filepath.Join(dir, "findings.json"), []byte("not json at all"), 0o644); err != nil {
			t.Fatal(err)
		}
		return readFindings(dir)
	}(); !strings.Contains(detail, "not valid JSON") {
		t.Errorf("unparseable findings.json should say so, got %q", detail)
	}
}

// TestTriageAnswerKeyMatchesTheFixture guards the failure mode where the
// fixture is edited and the key is not: every planted issue must actually be
// present in the file it is attributed to, and wire.py must stay clean.
func TestTriageAnswerKeyMatchesTheFixture(t *testing.T) {
	files := TriageTask().Files
	markers := map[string]string{
		"hardcoded_credential":      "sk-live-",
		"sql_injection":             "% owner",
		"path_traversal":            "os.path.join(UPLOAD_ROOT, name)",
		"tls_verification_disabled": "CERT_NONE",
		"unsafe_deserialization":    "pickle.loads",
	}
	for _, issue := range triageIssues {
		src, ok := files[issue.File]
		if !ok {
			t.Errorf("answer key names %s, which the fixture does not contain", issue.File)
			continue
		}
		if marker := markers[issue.Category]; !strings.Contains(src, marker) {
			t.Errorf("%s is keyed as %s but does not contain %q", issue.File, issue.Category, marker)
		}
	}
	for _, marker := range markers {
		if strings.Contains(files["wire.py"], marker) {
			t.Errorf("wire.py is supposed to be clean but contains %q", marker)
		}
	}
	// Exactly two FIXMEs, and they must be the two the remediation criteria score.
	var marked []string
	for name, src := range files {
		if strings.Contains(src, "FIXME(aegis-eval)") {
			marked = append(marked, name)
		}
	}
	if len(marked) != 2 {
		t.Errorf("fixture has %d FIXME(aegis-eval) markers, want exactly 2: %v", len(marked), marked)
	}
	for _, want := range []string{"store.py", "files.py"} {
		if !strings.Contains(files[want], "FIXME(aegis-eval)") {
			t.Errorf("%s must carry a FIXME(aegis-eval) marker — it is scored as a remediation", want)
		}
	}
}

// TestTriagePromptNamesEveryCategory: grading uses a closed vocabulary, so a
// category the prompt never mentions is one no model can earn.
func TestTriagePromptNamesEveryCategory(t *testing.T) {
	prompt := TriageTask().Prompt("python")
	for _, cat := range triageCategories {
		if !strings.Contains(prompt, cat) {
			t.Errorf("prompt never mentions category %q, so it is unearnable", cat)
		}
	}
	for _, issue := range triageIssues {
		var known bool
		for _, cat := range triageCategories {
			if cat == issue.Category {
				known = true
			}
		}
		if !known {
			t.Errorf("answer key uses category %q which is not in the prompt vocabulary", issue.Category)
		}
	}
	for _, name := range triageProtected {
		if !strings.Contains(prompt, name) {
			t.Errorf("prompt does not tell the agent to leave %s alone, but grading penalizes touching it", name)
		}
	}
}
