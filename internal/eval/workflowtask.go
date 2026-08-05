package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Harness-portable workflow task, P60.4.
//
// TestLiveWorkflow drives a real daemon over the HTTP+SSE seam against a real
// local model, and it is the tier that actually caught P25.1-P25.6. Its
// limitation is that it measures Aegis and the model *fused together*: when a
// run fails its workflow-shape assertions, nothing in the result distinguishes
// "this local model is too weak for the task" from "our scaffolding regressed."
// That distinction was only ever knowable because the same model had passed
// before — which is no help at all for a model being tried for the first time.
//
// The control group is the same task, in the same environment, run through a
// second harness against the same model: a task both harnesses fail is the
// model; a task only Aegis fails is us. That is the one genuinely portable idea
// in Orchard's evaluation discipline (hold the environment fixed, swap the
// harness), and taking it requires the task definition to live *outside* the
// test that asserts on Aegis's event stream.
//
// So the split here is deliberate and is the whole point of the file:
//
//   - Portable (this file): the fixture, the prompt, and the *outcome*
//     assertions — did the program end up correct, by re-running it rather
//     than by believing the agent. Any CLI agent can be measured on these.
//   - Aegis-only (live_workflow_test.go): the SSE-shape assertions — tool-call
//     count, no `find /` detours, no guard meta-text leakage, token accounting.
//     These read our own event stream and have no meaning for another harness.
//
// Nothing in this file talks to a model, a daemon or the network, so it is
// ordinary code compiled and unit-tested by `go test ./...` — the live tiers
// that use it stay behind their build tags.

// WorkflowTask is one agent task expressed independently of the harness that
// runs it: files to materialize, a prompt to send, and a check on the resulting
// directory.
type WorkflowTask struct {
	// Name identifies the task in reports.
	Name string
	// Files maps a relative path to its initial content.
	Files map[string]string
	// prompt builds the instruction sent to the agent. It takes the resolved
	// interpreter path because the task's whole point is running a script, and
	// on Windows "python3" frequently is not the interpreter.
	prompt func(interpreter string) string
	// verify re-derives the outcome from the directory itself, returning one
	// string per failed expectation (empty = the task was actually completed).
	verify func(dir, interpreter string) []string
}

// SeededBugTask is the two-file temps.py/temps.csv project from the P25
// live-eval writeup: the "temp" column arrives from the CSV reader as a string
// and is added to an int accumulator without conversion. It is a good control
// task precisely because it is small and unambiguous — a harness that fails it
// is failing at driving the model, not at a hard problem.
func SeededBugTask() WorkflowTask {
	return WorkflowTask{
		Name: "seeded-bug-fix",
		Files: map[string]string{
			"temps.csv": "city,temp\nNYC,72\nLA,85\nChicago,68\n",
			"temps.py": `import csv


def main():
    total = 0
    count = 0
    with open("temps.csv", newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            total += row["temp"]
            count += 1
    print(f"Average temp: {total / count}")


if __name__ == "__main__":
    main()
`,
		},
		prompt: func(interpreter string) string {
			return "Run `" + interpreter + " temps.py`, diagnose and fix the bug, then re-run it to confirm the fix works."
		},
		verify: verifySeededBugFix,
	}
}

// Materialize writes the task's files into dir.
func (t WorkflowTask) Materialize(dir string) error {
	for name, content := range t.Files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Prompt returns the instruction to send to the agent.
func (t WorkflowTask) Prompt(interpreter string) string { return t.prompt(interpreter) }

// Outcome re-derives whether the task was actually completed, by inspecting and
// re-running what is on disk. It never consults the agent's own account of what
// it did — a claim of success is the thing being measured, not evidence.
func (t WorkflowTask) Outcome(dir, interpreter string) TaskOutcome {
	failures := t.verify(dir, interpreter)
	return TaskOutcome{Task: t.Name, Passed: len(failures) == 0, Failures: failures}
}

// TaskOutcome is one harness's result on one task.
type TaskOutcome struct {
	Task     string
	Harness  string
	Passed   bool
	Failures []string
	Elapsed  time.Duration
	// Err is set when the harness itself could not be run to completion
	// (missing binary, timeout, non-zero exit). Distinct from a failed
	// outcome: an agent that ran and got it wrong is a data point, an agent
	// that never ran is not.
	Err error
}

// String renders a one-line summary for a test log.
func (o TaskOutcome) String() string {
	status := "FAIL"
	if o.Passed {
		status = "PASS"
	}
	s := fmt.Sprintf("%s[%s] %s in %s", status, o.Harness, o.Task, o.Elapsed.Round(time.Second))
	if o.Err != nil {
		s += fmt.Sprintf(" (harness error: %v)", o.Err)
	}
	for _, f := range o.Failures {
		s += "\n    - " + f
	}
	return s
}

// verifySeededBugFix checks the three things that make the seeded bug actually
// fixed, in the order they fail informatively.
func verifySeededBugFix(dir, interpreter string) []string {
	var failures []string
	src, err := os.ReadFile(filepath.Join(dir, "temps.py"))
	if err != nil {
		return []string{fmt.Sprintf("temps.py is unreadable after the run: %v", err)}
	}
	// The obvious way to make a failing script pass is to stop it doing the
	// work. Both cheats are worth catching explicitly, because a harness
	// comparison is worthless if "passed" can mean "deleted the problem".
	text := string(src)
	if !strings.Contains(text, "temps.csv") {
		failures = append(failures, "temps.py no longer reads temps.csv — the data source was removed rather than the bug fixed")
	}
	if !strings.Contains(text, "csv") {
		failures = append(failures, "temps.py no longer uses the csv module — the values were likely hardcoded")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, interpreter, "temps.py")
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return append(failures, fmt.Sprintf("temps.py still fails when re-run: %v\n%s", runErr, strings.TrimSpace(string(out))))
	}
	// 72, 85 and 68 average to exactly 75, so the correct answer is checkable
	// without parsing float formatting.
	if !strings.Contains(string(out), "75") {
		failures = append(failures, fmt.Sprintf("temps.py runs but does not print the correct average (expected 75): %q", strings.TrimSpace(string(out))))
	}
	return failures
}

// Harness runs an agent against a prepared directory. Aegis implements it via
// its own daemon in the live tier; a baseline harness is any other CLI agent.
type Harness interface {
	Name() string
	Run(ctx context.Context, dir, prompt string) error
}

// CLIHarness runs an external agent as a subprocess in the task directory —
// the baseline side of the control group.
type CLIHarness struct {
	name string
	argv []string
}

// PromptPlaceholder is substituted with the task prompt in a harness command
// line. A harness that reads the prompt on stdin instead is out of scope here:
// every agent CLI worth comparing against accepts a one-shot prompt argument,
// and supporting both would double the surface for no extra signal.
const PromptPlaceholder = "{prompt}"

// NewCLIHarness parses a command line like `claude -p {prompt}` into a runnable
// baseline harness. Arguments are split on whitespace — deliberately simple,
// since this is a developer-supplied string in an on-demand test tier, not
// user input.
func NewCLIHarness(spec string) (*CLIHarness, error) {
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty harness command")
	}
	if !strings.Contains(spec, PromptPlaceholder) {
		return nil, fmt.Errorf("harness command %q contains no %s placeholder, so the task prompt would never reach it", spec, PromptPlaceholder)
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		return nil, fmt.Errorf("harness binary %q not found on PATH: %w", fields[0], err)
	}
	return &CLIHarness{name: fields[0], argv: fields}, nil
}

// Name reports the harness binary's name.
func (h *CLIHarness) Name() string { return h.name }

// Run invokes the agent with the prompt substituted, rooted at dir.
func (h *CLIHarness) Run(ctx context.Context, dir, prompt string) error {
	args := make([]string, 0, len(h.argv)-1)
	for _, a := range h.argv[1:] {
		args = append(args, strings.ReplaceAll(a, PromptPlaceholder, prompt))
	}
	cmd := exec.CommandContext(ctx, h.argv[0], args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", h.name, err, truncate(string(out), 2000))
	}
	return nil
}

// RunTask materializes task into its own directory, runs h against it, and
// re-derives the outcome. The directory is returned so a caller can inspect or
// keep it; cleanup is the caller's.
func RunTask(ctx context.Context, h Harness, task WorkflowTask, dir, interpreter string) TaskOutcome {
	start := time.Now()
	if err := task.Materialize(dir); err != nil {
		return TaskOutcome{Task: task.Name, Harness: h.Name(), Err: err}
	}
	runErr := h.Run(ctx, dir, task.Prompt(interpreter))
	outcome := task.Outcome(dir, interpreter)
	outcome.Harness = h.Name()
	outcome.Elapsed = time.Since(start)
	// A harness that errored still gets its outcome checked: several agent
	// CLIs exit non-zero on a hit budget or an unanswered prompt after having
	// already fixed the file, and the file is what the comparison is about.
	// The error is carried alongside so a genuinely failed invocation is not
	// silently read as "the model is weak".
	outcome.Err = runErr
	return outcome
}

// Verdict is what a cross-harness comparison concludes.
type Verdict string

const (
	// VerdictOK — Aegis completed the task. Nothing to attribute.
	VerdictOK Verdict = "ok"
	// VerdictScaffolding — Aegis failed a task the baseline harness completed
	// against the same model in the same environment. This is the finding the
	// control group exists to produce: it is us, not the model.
	VerdictScaffolding Verdict = "scaffolding"
	// VerdictModel — both harnesses failed. The model (or the task) is beyond
	// what this environment can do; a failing assertion here is not evidence
	// of an Aegis regression.
	VerdictModel Verdict = "model"
	// VerdictUnknown — no usable baseline result, so the failure cannot be
	// attributed either way. This is the honest state whenever no baseline
	// harness is configured, and it is why the comparison is a separate,
	// opt-in tier rather than a condition on the main one.
	VerdictUnknown Verdict = "unknown"
)

// Compare attributes an Aegis failure by holding it against a baseline result.
// A baseline that could not be run at all (Err set and the outcome failed)
// yields VerdictUnknown rather than a confident attribution — a baseline
// harness that crashed proves nothing about ours.
func Compare(aegis, baseline TaskOutcome) (Verdict, string) {
	if aegis.Passed {
		return VerdictOK, fmt.Sprintf("Aegis completed %s", aegis.Task)
	}
	switch {
	case baseline.Passed:
		return VerdictScaffolding, fmt.Sprintf(
			"Aegis failed %s but %s completed it against the same model in the same environment — this is a scaffolding problem, not a model limitation:\n%s",
			aegis.Task, baseline.Harness, aegis)
	case baseline.Err != nil:
		return VerdictUnknown, fmt.Sprintf(
			"Aegis failed %s and the %s baseline could not be run to completion (%v), so the failure cannot be attributed",
			aegis.Task, baseline.Harness, baseline.Err)
	default:
		return VerdictModel, fmt.Sprintf(
			"Aegis and %s both failed %s against this model — the environment, not the harness, is the limit",
			baseline.Harness, aegis.Task)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
