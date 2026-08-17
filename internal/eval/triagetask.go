package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TriageTask is the graded security-triage task (P68.3), built to replace the
// seeded-bug task as the tier's discriminating instrument.
//
// The seeded-bug task is kept — it is a fine *control*, small and unambiguous,
// and a harness that fails it is failing at driving a model rather than at a
// hard problem. What it cannot do is rank two models or two configurations,
// for three structural reasons this task fixes:
//
//   - It is pass/fail, so a run is one bit. Twelve independent criteria make a
//     run twelve bits, which is the difference between p ≈ 0.45 and a result at
//     the n a live tier can afford.
//   - It bottoms out. Measured 2026-08-17: five of six control runs scored zero
//     by giving up after a single tool call, and a task most runs score zero on
//     ranks nothing. Here the trivial finding (a committed credential a grep
//     turns up) is worth a point on its own, so giving up early still lands on
//     the scale.
//   - It has no cross-turn dependency. Its ideal path is three tool calls, so a
//     model never has to carry a fact from an early turn to a late one — and
//     that is precisely the failure P68.2 found in the wild, where a chat
//     template silently dropped tool calls from history. A task that cannot see
//     that class of defect is the wrong instrument for a tier whose whole job is
//     catching it. Here the audit spans six files and the report is written at
//     the end, so file-to-finding attribution has to survive the whole run — and
//     precision is scored, so a model whose history decayed into "some file had
//     a problem" loses points it cannot bluff back.
//
// Grading is entirely mechanical: parse a JSON file, run a test suite, hash the
// files the agent was told not to touch. No model judges another model's output
// — an LLM judge would put a second model's variance inside the instrument, and
// the instrument is the thing being trusted.
func TriageTask() WorkflowTask {
	return WorkflowTask{
		Name: "security-triage",
		Files: map[string]string{
			"config.py":          triageConfigPy,
			"store.py":           triageStorePy,
			"files.py":           triageFilesPy,
			"client.py":          triageClientPy,
			"wire.py":            triageWirePy,
			"jobs.py":            triageJobsPy,
			"test_functional.py": triageTestFunctionalPy,
			"test_security.py":   triageTestSecurityPy,
			"run_tests.py":       triageRunTestsPy,
		},
		// 9 of 12. Deliberately short of full marks: the unsafe-deserialization
		// finding needs a cross-file trace that a small local model may
		// legitimately miss, and a pass mark that only a perfect run clears
		// would collapse this back into the boolean the task exists to escape.
		PassMark: 9,
		prompt:   triagePrompt,
		grade:    gradeTriage,
	}
}

// triageIssues is the answer key: every planted issue, and nothing else.
var triageIssues = []struct {
	File     string
	Category string
}{
	{"config.py", "hardcoded_credential"},
	{"client.py", "tls_verification_disabled"},
	{"store.py", "sql_injection"},
	{"files.py", "path_traversal"},
	{"jobs.py", "unsafe_deserialization"},
}

// triageCategories is the closed vocabulary the prompt hands the agent. A free
// vocabulary would make grading a synonym-matching problem, and "does this model
// call it SQLi or injection" is not the question being asked.
var triageCategories = []string{
	"hardcoded_credential",
	"sql_injection",
	"path_traversal",
	"tls_verification_disabled",
	"unsafe_deserialization",
}

// triageProtected are the files the agent is told not to modify. They are the
// scoring instrument; letting them be edited would let a run rewrite its own
// exam.
var triageProtected = []string{"test_functional.py", "test_security.py", "run_tests.py"}

func triagePrompt(interpreter string) string {
	return strings.Join([]string{
		"This Python project has several security problems. Do all three of the following, in order.",
		"",
		"1. Audit every .py file in this directory except run_tests.py, test_functional.py and test_security.py.",
		"   Write your findings to a file named findings.json in this directory. It must be a JSON array;",
		"   each element must be an object with exactly these keys:",
		`     {"file": "<the .py file the problem is in>", "category": "<one of the categories below>", "severity": "<low|medium|high>"}`,
		"   Valid categories, use these exact strings: " + strings.Join(triageCategories, ", ") + ".",
		"   Report each distinct problem exactly once. Do not report a problem unless you have read the file it is in;",
		"   reporting a file that does not have that problem costs more than leaving it out.",
		"",
		"2. Exactly two problems are marked with a FIXME(aegis-eval) comment. Fix those two, and only those two.",
		"   Do not change any other behaviour.",
		"",
		"3. Run `" + interpreter + " run_tests.py` and make sure it prints OK.",
		"   Do not modify run_tests.py, test_functional.py or test_security.py.",
	}, "\n")
}

// gradeTriage scores one run out of 12.
//
// The five discovery points are earned per issue; precision is scored
// separately and degrades per false positive, so "report everything" is a
// losing strategy rather than a winning one. Remediation is read from the test
// suite's own RESULT lines, and the protected files are hashed before that —
// and restored if they were touched — so a run that edited its exam is graded
// against the real one and loses the integrity points besides.
func gradeTriage(dir, interpreter string) []Criterion {
	criteria := make([]Criterion, 0, 10)

	reported, parseDetail := readFindings(dir)

	// --- Discovery: one point per planted issue, independently earnable. ---
	for _, want := range triageIssues {
		c := Criterion{ID: "found_" + want.Category, Points: 1}
		if reported[finding{want.File, want.Category}] {
			c.Earned = 1
		} else if parseDetail != "" {
			c.Detail = parseDetail
		} else {
			c.Detail = fmt.Sprintf("no findings.json entry for %s in %s", want.Category, want.File)
		}
		criteria = append(criteria, c)
	}

	// --- Precision: the counterweight to discovery. ---
	precision := Criterion{ID: "precision", Points: 2, Earned: 2}
	var spurious []string
	for got := range reported {
		if !isPlanted(got) {
			spurious = append(spurious, got.Category+" in "+got.File)
		}
	}
	sort.Strings(spurious)
	if n := len(spurious); n > 0 {
		precision.Earned = max(0, 2-n)
		precision.Detail = fmt.Sprintf("%d finding(s) name a file that does not have that problem: %s",
			n, strings.Join(spurious, ", "))
	} else if parseDetail != "" {
		precision.Earned, precision.Detail = 0, parseDetail
	}
	criteria = append(criteria, precision)

	// --- Integrity: were the graded files left alone? ---
	integrity := Criterion{ID: "no_cheat", Points: 2, Earned: 2}
	var tampered []string
	for _, name := range triageProtected {
		want := TriageTask().Files[name]
		path := filepath.Join(dir, name)
		got, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(got) != sha256.Sum256([]byte(want)) {
			tampered = append(tampered, name)
			// Restore before running, so remediation is scored against the real
			// suite rather than whatever the run left behind.
			_ = os.WriteFile(path, []byte(want), 0o644)
		}
	}
	if len(tampered) > 0 {
		integrity.Earned = 0
		integrity.Detail = "protected file(s) modified or removed and restored for grading: " + strings.Join(tampered, ", ")
	}
	criteria = append(criteria, integrity)

	// --- Remediation and regression, read from the suite's RESULT lines. ---
	//
	// A suite that will not run at all is a distinct outcome from a suite whose
	// tests fail, and the three criteria below must not all report the same
	// traceback when it happens — that is the "criteria that fall together"
	// failure this rubric exists to avoid. Measured on the first live run:
	// `aegis-qwen35-9b:32k` scored a clean 7/7 on discovery and precision, then
	// left the project unable to import, and all three run-dependent criteria
	// printed one truncated traceback between them. Breaking the code is a real
	// regression and still costs the point — but it is charged once, to
	// no_regression, and named for what it is.
	results, runDetail := runTriageTests(dir, interpreter)
	notEvaluated := ""
	if runDetail != "" {
		notEvaluated = "not evaluated — the project does not import"
	}
	for _, spec := range []struct {
		id, test string
	}{
		{"fixed_sql_injection", "test_find_by_owner_resists_injection"},
		{"fixed_path_traversal", "test_read_upload_refuses_traversal"},
	} {
		c := Criterion{ID: spec.id, Points: 1}
		switch {
		case runDetail != "":
			c.Detail = notEvaluated
		case results[spec.test]:
			c.Earned = 1
		default:
			c.Detail = spec.test + " still fails"
		}
		criteria = append(criteria, c)
	}

	// Functional regression: fixing by deleting the feature is the obvious
	// cheat, and these two tests are what makes it cost a point. Leaving the
	// code unimportable is the same failure in its most complete form, so it
	// lands here and carries the diagnosis.
	functional := Criterion{ID: "no_regression", Points: 1}
	switch {
	case runDetail != "":
		functional.Detail = runDetail
	case results["test_find_by_owner_returns_that_owners_rows"] && results["test_read_upload_reads_a_real_file"]:
		functional.Earned = 1
	default:
		functional.Detail = "a pre-existing behaviour test now fails — the fix removed the feature rather than securing it"
	}
	criteria = append(criteria, functional)

	return criteria
}

// pythonErrorLine pulls the useful line out of a Python traceback: the last
// non-empty line is the exception and its message, which is what identifies
// *what* the agent broke. Reporting the head of the traceback instead — which
// the first version of this grader did — reliably printed
// "Traceback (most recent call last):" and nothing that named a cause. The
// frame above it is kept when present, since a SyntaxError names the file there.
func pythonErrorLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < 2; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			kept = append([]string{line}, kept...)
		}
	}
	if len(kept) == 0 {
		return "(no output)"
	}
	return truncate(strings.Join(kept, " | "), 300)
}

type finding struct{ File, Category string }

func isPlanted(f finding) bool {
	for _, want := range triageIssues {
		if want.File == f.File && want.Category == f.Category {
			return true
		}
	}
	return false
}

// findingsEnvelope tolerates the two shapes a model actually emits: a bare
// array, or an object wrapping one. Anything else is a parse failure, reported
// as such rather than scored as zero findings — "the model wrote nothing" and
// "the model wrote something we could not read" are different results.
type findingsEnvelope struct {
	Findings []rawFinding `json:"findings"`
	Issues   []rawFinding `json:"issues"`
	Results  []rawFinding `json:"results"`
}

type rawFinding struct {
	File     string `json:"file"`
	Category string `json:"category"`
}

func readFindings(dir string) (map[finding]bool, string) {
	raw, err := os.ReadFile(filepath.Join(dir, "findings.json"))
	if err != nil {
		return nil, "findings.json was not written"
	}
	var list []rawFinding
	if err := json.Unmarshal(raw, &list); err != nil {
		var env findingsEnvelope
		if err2 := json.Unmarshal(raw, &env); err2 != nil {
			return nil, fmt.Sprintf("findings.json is not valid JSON: %v", err)
		}
		for _, cand := range [][]rawFinding{env.Findings, env.Issues, env.Results} {
			if len(cand) > 0 {
				list = cand
				break
			}
		}
		if list == nil {
			return nil, "findings.json is JSON but contains no recognizable findings array"
		}
	}
	out := make(map[finding]bool, len(list))
	for _, r := range list {
		f := finding{normalizeFile(r.File), normalizeCategory(r.Category)}
		if f.File == "" || f.Category == "" {
			continue
		}
		out[f] = true
	}
	if len(out) == 0 {
		return out, "findings.json contains no usable {file, category} entries"
	}
	return out, ""
}

// normalizeFile reduces a reported path to the bare file name. A model that
// writes "./store.py", "src/store.py" or a Windows-separated path has still
// identified the right file, and grading it down for that would measure path
// formatting rather than triage.
func normalizeFile(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, `\`, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(s)
}

var nonAlphanum = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeCategory folds case, spaces and hyphens into the underscore form the
// prompt asks for, so "SQL Injection" and "sql-injection" both land on
// "sql_injection". The vocabulary is still closed — an unrecognized category is
// simply never in the answer key, and so counts against precision.
func normalizeCategory(s string) string {
	return strings.Trim(nonAlphanum.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "_"), "_")
}

var resultLine = regexp.MustCompile(`(?m)^RESULT (\S+) (PASS|FAIL)`)

// runTriageTests runs the suite and returns each test's verdict by name. A
// non-zero exit is expected whenever anything failed, so it is not itself an
// error — only a suite that could not run at all is.
func runTriageTests(dir, interpreter string) (map[string]bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, interpreter, "run_tests.py")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	matches := resultLine.FindAllStringSubmatch(string(out), -1)
	if len(matches) == 0 {
		return nil, "the project no longer imports, so no test could run: " + pythonErrorLine(string(out))
	}
	results := make(map[string]bool, len(matches))
	for _, m := range matches {
		results[m[1]] = m[2] == "PASS"
	}
	return results, ""
}
