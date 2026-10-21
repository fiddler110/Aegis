package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GoFixTask is the P88.7 scenario: the one the 2026-10-01 trial drove by hand
// three times (one run per configuration, one prompt) and drew P88.1–P88.6
// from. A small Go module whose Average divides by len(nums) and so panics on
// an empty slice; the agent must fix it, add a table-driven test, and run
// `go test`.
//
// It exists so those findings stop resting on n=1. The trial's three failure
// modes each land on a different criterion here — an uncompilable test file
// (P88.2) fails vet_clean and tests_pass but can still earn fix_correct; an
// unfixed Average fails fix_correct but can still earn test_added — so a run
// yields a score, not one bit, and two models that both "failed" can be told
// apart.
//
// The interpreter argument the WorkflowTask shape carries is unused: the
// oracle is the go toolchain on PATH, the same one the agent is told to run.
func GoFixTask() WorkflowTask {
	return WorkflowTask{
		Name:     "go-fix-and-test",
		PassMark: 7,
		Files: map[string]string{
			"go.mod": "module example.com/stats\n\ngo 1.22\n",
			"stats.go": `package stats

// Average returns the integer mean of nums.
func Average(nums []int) int {
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return sum / len(nums)
}
`,
		},
		prompt: func(string) string {
			return "Average in stats.go panics on an empty slice. Fix it so an empty (or nil) slice returns 0, " +
				"add a table-driven test in stats_test.go covering an empty slice, a single value and several values, " +
				"then run `go test ./...` and confirm it passes. Finish with a short report of what you changed."
		},
		grade: gradeGoFix,
	}
}

// goFixOracle is written beside the agent's files only for the grading run
// and removed after: it checks behavior through the public function, so any
// correct fix passes it, whatever shape the agent chose.
const goFixOracle = `package stats

import "testing"

func TestAegisEvalOracle(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{nil, 0},
		{[]int{}, 0},
		{[]int{4}, 4},
		{[]int{1, 2, 3, 6}, 3},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Average(%v) panicked: %v", c.in, r)
				}
			}()
			if got := Average(c.in); got != c.want {
				t.Errorf("Average(%v) = %d, want %d", c.in, got, c.want)
			}
		}()
	}
}
`

const goFixOracleFile = "zz_aegis_eval_oracle_test.go"

var (
	goTestFuncRe   = regexp.MustCompile(`func Test\w*\(t \*testing\.T\)`)
	goTableRe      = regexp.MustCompile(`\[\]struct\s*\{`)
	goEmptyInputRe = regexp.MustCompile(`\bnil\b|\[\]int\{\s*\}`)
)

func gradeGoFix(dir, _ string) []Criterion {
	noCheat := Criterion{ID: "no_cheat", Points: 1, Earned: 1}
	fixCorrect := Criterion{ID: "fix_correct", Points: 2}
	testAdded := Criterion{ID: "test_added", Points: 1}
	testTable := Criterion{ID: "test_table_driven", Points: 1}
	testEmpty := Criterion{ID: "test_covers_empty", Points: 1}
	vetClean := Criterion{ID: "vet_clean", Points: 1}
	testsPass := Criterion{ID: "tests_pass", Points: 1}
	noScope := Criterion{ID: "no_scope_creep", Points: 1, Earned: 1}
	all := func() []Criterion {
		return []Criterion{noCheat, fixCorrect, testAdded, testTable, testEmpty, vetClean, testsPass, noScope}
	}

	src, err := os.ReadFile(filepath.Join(dir, "stats.go"))
	if err != nil || !strings.Contains(string(src), "func Average(") {
		noCheat.Earned, noCheat.Detail = 0, "stats.go or its Average function is gone"
		return all()
	}
	if !strings.Contains(string(src), "for ") {
		noCheat.Earned, noCheat.Detail = 0, "Average no longer sums its input — the work was removed rather than fixed"
	}

	if test, err := os.ReadFile(filepath.Join(dir, "stats_test.go")); err != nil {
		testAdded.Detail = "stats_test.go was not created"
		testTable.Detail, testEmpty.Detail = "no test file", "no test file"
	} else {
		text := string(test)
		if goTestFuncRe.MatchString(text) {
			testAdded.Earned = 1
		} else {
			testAdded.Detail = "stats_test.go has no func TestX(t *testing.T)"
		}
		if goTableRe.MatchString(text) {
			testTable.Earned = 1
		} else {
			testTable.Detail = "no []struct{...} case table"
		}
		if goEmptyInputRe.MatchString(text) {
			testEmpty.Earned = 1
		} else {
			testEmpty.Detail = "no nil or []int{} case"
		}
	}

	if out, err := runGo(dir, "vet", "./..."); err == nil {
		vetClean.Earned = 1
	} else {
		vetClean.Detail = "go vet fails: " + firstLines(out, 3)
	}
	if out, err := runGo(dir, "test", "./..."); err == nil {
		testsPass.Earned = 1
	} else {
		testsPass.Detail = "go test fails: " + firstLines(out, 3)
	}

	// The oracle runs on its own, so a broken agent test file does not hide a
	// correct fix: it is the agent's file that is set aside, not the oracle.
	fixCorrect.Earned, fixCorrect.Detail = runGoFixOracle(dir)

	if entries, err := os.ReadDir(dir); err == nil {
		allowed := map[string]bool{"go.mod": true, "go.sum": true, "stats.go": true, "stats_test.go": true}
		var extra []string
		for _, e := range entries {
			name := e.Name()
			if !allowed[name] && !strings.HasPrefix(name, ".") {
				extra = append(extra, name)
			}
		}
		if len(extra) > 0 {
			noScope.Earned, noScope.Detail = 0, "unexpected files: "+strings.Join(extra, ", ")
		}
	}
	return all()
}

// runGoFixOracle runs the oracle test with the agent's own test file moved
// aside, then restores everything.
func runGoFixOracle(dir string) (int, string) {
	agentTest := filepath.Join(dir, "stats_test.go")
	parked := agentTest + ".parked"
	if _, err := os.Stat(agentTest); err == nil {
		if err := os.Rename(agentTest, parked); err != nil {
			return 0, "could not set the agent's test aside: " + err.Error()
		}
		defer os.Rename(parked, agentTest) //nolint:errcheck // best effort restore in a scratch dir
	}
	oracle := filepath.Join(dir, goFixOracleFile)
	if err := os.WriteFile(oracle, []byte(goFixOracle), 0o644); err != nil {
		return 0, "could not write the oracle: " + err.Error()
	}
	defer os.Remove(oracle)
	out, err := runGo(dir, "test", "-run", "TestAegisEvalOracle", ".")
	if err != nil {
		return 0, "oracle fails: " + firstLines(out, 4)
	}
	return 2, ""
}

func runGo(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " | ")
}
