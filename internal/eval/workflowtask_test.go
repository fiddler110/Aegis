package eval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// findInterpreter locates a usable Python, skipping (not failing) when none is
// present: the outcome checks here are about a real program's real behavior, and
// a machine without an interpreter is an environment gap, not a regression.
// Every candidate is invoked rather than merely resolved, because on Windows
// `python3` on PATH is commonly the App Execution Alias stub.
func findInterpreter(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "--version").Run(); err == nil {
			return path
		}
	}
	t.Skip("no working python3/python on PATH")
	return ""
}

// fakeHarness applies a canned edit to the task directory, standing in for an
// agent so the portable half of the P60.4 seam can be tested without a model.
type fakeHarness struct {
	name    string
	edit    func(dir string) error
	err     error
	gotDir  string
	gotText string
}

func (f *fakeHarness) Name() string { return f.name }
func (f *fakeHarness) Run(_ context.Context, dir, prompt string) error {
	f.gotDir, f.gotText = dir, prompt
	if f.edit != nil {
		if err := f.edit(dir); err != nil {
			return err
		}
	}
	return f.err
}

// TestSeededBugTaskStartsBroken: the fixture must actually fail before the
// agent touches it, or every harness "passes" and the comparison measures
// nothing.
func TestSeededBugTaskStartsBroken(t *testing.T) {
	py := findInterpreter(t)
	dir := t.TempDir()
	task := SeededBugTask()
	if err := task.Materialize(dir); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	outcome := task.Outcome(dir, py)
	if outcome.Passed {
		t.Fatal("the seeded-bug fixture passes its own outcome check unmodified")
	}
	if len(outcome.Failures) == 0 {
		t.Error("a failed outcome reported no failures")
	}
}

// TestSeededBugTaskRecognizesARealFix: the correct fix — converting the CSV
// value before accumulating — must pass.
func TestSeededBugTaskRecognizesARealFix(t *testing.T) {
	py := findInterpreter(t)
	dir := t.TempDir()
	task := SeededBugTask()
	if err := task.Materialize(dir); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	fixed := strings.Replace(task.Files["temps.py"], `total += row["temp"]`, `total += int(row["temp"])`, 1)
	if err := os.WriteFile(filepath.Join(dir, "temps.py"), []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	if outcome := task.Outcome(dir, py); !outcome.Passed {
		t.Errorf("the correct fix did not pass the outcome check: %v", outcome.Failures)
	}
}

// TestSeededBugTaskRejectsCheats is the property that makes a cross-harness
// comparison meaningful: "passed" must not be reachable by deleting the
// problem. Both cheats produce a script that runs cleanly and prints 75.
func TestSeededBugTaskRejectsCheats(t *testing.T) {
	py := findInterpreter(t)
	task := SeededBugTask()

	cases := map[string]string{
		"hardcoded answer": "print(\"Average temp: 75.0\")\n",
		"data source gone": "vals = [72, 85, 68]\nprint(f\"Average temp: {sum(vals) / len(vals)}\")\n",
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := task.Materialize(dir); err != nil {
				t.Fatalf("Materialize: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "temps.py"), []byte(replacement), 0o644); err != nil {
				t.Fatal(err)
			}
			if outcome := task.Outcome(dir, py); outcome.Passed {
				t.Errorf("cheat %q passed the outcome check", name)
			}
		})
	}
}

// TestRunTaskUsesTheHarness: RunTask must materialize, hand the harness the
// task's own prompt in the task's own directory, and derive the outcome from
// disk rather than from the harness's return.
func TestRunTaskUsesTheHarness(t *testing.T) {
	py := findInterpreter(t)
	dir := t.TempDir()
	task := SeededBugTask()
	h := &fakeHarness{name: "fake", edit: func(dir string) error {
		fixed := strings.Replace(task.Files["temps.py"], `total += row["temp"]`, `total += int(row["temp"])`, 1)
		return os.WriteFile(filepath.Join(dir, "temps.py"), []byte(fixed), 0o644)
	}}

	outcome := RunTask(context.Background(), h, task, dir, py)
	if !outcome.Passed {
		t.Errorf("outcome = failed, want passed: %v", outcome.Failures)
	}
	if outcome.Harness != "fake" {
		t.Errorf("harness = %q, want fake", outcome.Harness)
	}
	if h.gotDir != dir {
		t.Errorf("harness ran in %q, want %q", h.gotDir, dir)
	}
	if !strings.Contains(h.gotText, "temps.py") || !strings.Contains(h.gotText, py) {
		t.Errorf("harness prompt = %q, want the task prompt with the resolved interpreter", h.gotText)
	}
}

// TestRunTaskChecksDiskEvenWhenTheHarnessErrors: several agent CLIs exit
// non-zero after having already done the work (a hit budget, an unanswered
// final prompt), and the file is what the comparison is about.
func TestRunTaskChecksDiskEvenWhenTheHarnessErrors(t *testing.T) {
	py := findInterpreter(t)
	dir := t.TempDir()
	task := SeededBugTask()
	h := &fakeHarness{name: "grumpy", err: errors.New("exit status 1"), edit: func(dir string) error {
		fixed := strings.Replace(task.Files["temps.py"], `total += row["temp"]`, `total += int(row["temp"])`, 1)
		return os.WriteFile(filepath.Join(dir, "temps.py"), []byte(fixed), 0o644)
	}}
	outcome := RunTask(context.Background(), h, task, dir, py)
	if !outcome.Passed {
		t.Errorf("outcome = failed, want passed — the work was done despite the exit code: %v", outcome.Failures)
	}
	if outcome.Err == nil {
		t.Error("the harness error was dropped; it must stay visible alongside the outcome")
	}
}

// TestCompareAttributesFailure is the P60.4 payload: the verdict table that
// turns "the run failed" into "it is us" or "it is the model".
func TestCompareAttributesFailure(t *testing.T) {
	pass := TaskOutcome{Task: "t", Harness: "aegis", Passed: true}
	fail := TaskOutcome{Task: "t", Harness: "aegis"}
	basePass := TaskOutcome{Task: "t", Harness: "other", Passed: true}
	baseFail := TaskOutcome{Task: "t", Harness: "other"}
	baseBroken := TaskOutcome{Task: "t", Harness: "other", Err: errors.New("binary crashed")}

	cases := []struct {
		name        string
		aegis, base TaskOutcome
		want        Verdict
	}{
		{"aegis passed", pass, baseFail, VerdictOK},
		{"only aegis failed", fail, basePass, VerdictScaffolding},
		{"both failed", fail, baseFail, VerdictModel},
		{"baseline unusable", fail, baseBroken, VerdictUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, explain := Compare(tc.aegis, tc.base)
			if got != tc.want {
				t.Errorf("verdict = %q, want %q", got, tc.want)
			}
			if explain == "" {
				t.Error("verdict carried no explanation")
			}
		})
	}
}

// TestNewCLIHarnessRejectsUnusableSpecs: a baseline command with no placeholder
// would run the agent with no task at all and then be scored on an untouched
// fixture — a guaranteed, meaningless "the baseline failed too".
func TestNewCLIHarnessRejectsUnusableSpecs(t *testing.T) {
	if _, err := NewCLIHarness(""); err == nil {
		t.Error("empty spec accepted")
	}
	if _, err := NewCLIHarness("some-agent --print"); err == nil {
		t.Error("spec without a prompt placeholder accepted")
	}
	if _, err := NewCLIHarness("definitely-not-a-real-binary-xyz -p {prompt}"); err == nil {
		t.Error("spec naming a binary that is not on PATH accepted")
	}
}

// TestCLIHarnessSubstitutesThePrompt: the placeholder is replaced inside the
// argument that carries it, not appended as a separate word.
func TestCLIHarnessSubstitutesThePrompt(t *testing.T) {
	shell, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go binary on PATH")
	}
	h, err := NewCLIHarness(filepath.Base(shell) + " env {prompt}")
	if err != nil {
		t.Fatalf("NewCLIHarness: %v", err)
	}
	if h.Name() != filepath.Base(shell) {
		t.Errorf("Name() = %q", h.Name())
	}
	// `go env GOOS` succeeds; the point is that "GOOS" arrived as the value of
	// the placeholder argument.
	if err := h.Run(context.Background(), t.TempDir(), "GOOS"); err != nil {
		t.Errorf("Run: %v", err)
	}
}
