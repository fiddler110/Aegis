package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/tool"
)

// testRunnerTool closes GAP-08: a first-class test-runner concept distinct
// from the generic shell tool, with structured pass/fail parsing so a model
// doesn't have to re-derive "did the tests pass, and which ones failed" from
// raw stdout every time. It reuses shellTool's own execution path (sandbox,
// checkpoint capture, output spill) rather than reimplementing any of it —
// only detection and result parsing are new.
type testRunnerTool struct {
	root       string
	timeoutSec int
	sb         sandbox.Backend
}

func newTestRunnerTool(root string, timeoutSec int, sb sandbox.Backend) *testRunnerTool {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &testRunnerTool{root: root, timeoutSec: timeoutSec, sb: sb}
}

func (t *testRunnerTool) Name() string                { return "run_tests" }
func (t *testRunnerTool) Capability() tool.Capability { return tool.CapExecute }
func (t *testRunnerTool) Description() string {
	return "Run the project's test suite and report structured pass/fail counts and failing test names. " +
		"Auto-detects the test command from project files (go.mod, package.json, pyproject.toml/pytest.ini/setup.py, Cargo.toml) " +
		"unless an explicit command is given. Prefer this over shell for running tests — it parses the summary for you."
}
func (t *testRunnerTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"command":{"type":"string","description":"explicit test command to run instead of auto-detecting one"},"timeout_sec":{"type":"integer","description":"optional per-call timeout override in seconds"}},"required":[]}`)
}
func (t *testRunnerTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"framework":{"type":"string"},"passed":{"type":"integer"},"failed":{"type":"integer"},"total":{"type":"integer"},"failing_names":{"type":"array","items":{"type":"string"}},"output":{"type":"string"}},"required":["output"]}`)
}

func (t *testRunnerTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Command    string `json:"command"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	root := effectiveRoot(ctx, t.root)

	command := strings.TrimSpace(args.Command)
	if command == "" {
		var err string
		command, err = detectTestCommand(root)
		if command == "" {
			return tool.Result{Content: err, IsError: true}, nil
		}
	}

	timeout := time.Duration(t.timeoutSec) * time.Second
	if args.TimeoutSec > 0 {
		timeout = time.Duration(min(args.TimeoutSec, maxShellTimeoutSec)) * time.Second
	}

	text, err := captureShellWrites(ctx, checkpoint.SnapshotterFrom(ctx), root, func() (string, error) {
		return t.exec(ctx, root, command, timeout)
	})
	text = SpillTail(ctx, root, "run_tests", text, maxShellOutput, "use shell with background:true for a longer-running suite")

	summary := parseTestOutput(command, text)
	header := summary.header()
	content := text
	if header != "" {
		content = header + "\n\n" + text
	}
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("%v\n%s", err, content), IsError: true}, nil
	}
	return tool.Result{Content: content}, nil
}

func (t *testRunnerTool) exec(ctx context.Context, root, command string, timeout time.Duration) (string, error) {
	if t.sb != nil {
		return t.sb.Exec(ctx, command, sandbox.ExecOpts{Dir: root, Timeout: timeout})
	}
	return sandbox.NewLocalBackend().Exec(ctx, command, sandbox.ExecOpts{Dir: root, Timeout: timeout})
}

// detectTestCommand picks a test command from files present at root. Returns
// ("", errMsg) when nothing recognizable is present.
func detectTestCommand(root string) (string, string) {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	switch {
	case exists("go.mod"):
		return "go test ./...", ""
	case exists("Cargo.toml"):
		return "cargo test", ""
	case exists("pyproject.toml"), exists("pytest.ini"), exists("setup.py"):
		return "pytest", ""
	case exists("package.json"):
		switch {
		case exists("pnpm-lock.yaml"):
			return "pnpm test", ""
		case exists("yarn.lock"):
			return "yarn test", ""
		default:
			return "npm test", ""
		}
	default:
		return "", "could not auto-detect a test command (no go.mod, package.json, pyproject.toml/pytest.ini/setup.py, or Cargo.toml at the workspace root) — pass an explicit command"
	}
}

// testSummary is the structured result of parsing one framework's test
// output. Zero value (Total == 0 with no failing names) means nothing was
// recognized, in which case header() returns "" and the tool falls back to
// raw output only — never blocking or misreporting on an unrecognized format.
type testSummary struct {
	framework    string
	passed       int
	failed       int
	total        int
	failingNames []string
	recognized   bool
}

func (s testSummary) header() string {
	if !s.recognized {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d failed, %d passed (%s)", s.failed, s.passed, s.framework)
	for _, n := range s.failingNames {
		fmt.Fprintf(&sb, "\n  - %s", n)
	}
	return sb.String()
}

var (
	goFailRe     = regexp.MustCompile(`(?m)^--- FAIL: (\S+)`)
	goPassRe     = regexp.MustCompile(`(?m)^--- PASS: (\S+)`)
	goOKPkgRe    = regexp.MustCompile(`(?m)^ok\s+\S+`)
	goFailPkgRe  = regexp.MustCompile(`(?m)^FAIL\s+\S+`)
	pytestLineRe = regexp.MustCompile(`(\d+) failed(?:, (\d+) passed)?|(\d+) passed`)
	jestLineRe   = regexp.MustCompile(`Tests:\s+(?:(\d+) failed, )?(?:(\d+) skipped, )?(\d+) passed, (\d+) total`)
	cargoLineRe  = regexp.MustCompile(`test result: (ok|FAILED)\. (\d+) passed; (\d+) failed`)
)

// parseTestOutput dispatches to the parser matching the command that was
// run, falling back to an unrecognized (zero-value) summary when nothing
// matches — the raw output is still returned in that case.
func parseTestOutput(command, output string) testSummary {
	switch {
	case strings.HasPrefix(command, "go "), strings.Contains(command, "go test"):
		return parseGoTestOutput(output)
	case strings.HasPrefix(command, "cargo"):
		return parseCargoTestOutput(output)
	case strings.HasPrefix(command, "pytest"):
		return parsePytestOutput(output)
	case strings.HasPrefix(command, "npm"), strings.HasPrefix(command, "yarn"), strings.HasPrefix(command, "pnpm"):
		return parseJestOutput(output)
	default:
		return testSummary{}
	}
}

func parseGoTestOutput(output string) testSummary {
	fails := goFailRe.FindAllStringSubmatch(output, -1)
	passes := goPassRe.FindAllStringSubmatch(output, -1)
	names := make([]string, 0, len(fails))
	for _, m := range fails {
		names = append(names, m[1])
	}
	if len(passes) > 0 {
		// Verbose (-v) output: exact per-test counts.
		return testSummary{
			framework: "go test", recognized: true,
			passed: len(passes), failed: len(fails), total: len(passes) + len(fails),
			failingNames: names,
		}
	}
	// Non-verbose `go test`: no per-test PASS lines are printed on success, only
	// per-package "ok"/"FAIL" summary lines (plus "--- FAIL:" for any failing
	// test regardless of -v). Fall back to a package-level count so a clean run
	// still reports something recognized instead of silently matching nothing.
	okPkgs := goOKPkgRe.FindAllString(output, -1)
	failPkgs := goFailPkgRe.FindAllString(output, -1)
	if len(fails) == 0 && len(okPkgs) == 0 && len(failPkgs) == 0 {
		return testSummary{}
	}
	return testSummary{
		framework: "go test", recognized: true,
		passed: len(okPkgs), failed: len(fails), total: len(okPkgs) + len(fails),
		failingNames: names,
	}
}

func parseCargoTestOutput(output string) testSummary {
	m := cargoLineRe.FindStringSubmatch(output)
	if m == nil {
		return testSummary{}
	}
	passed, _ := strconv.Atoi(m[2])
	failed, _ := strconv.Atoi(m[3])
	return testSummary{
		framework: "cargo test", recognized: true,
		passed: passed, failed: failed, total: passed + failed,
	}
}

func parsePytestOutput(output string) testSummary {
	m := pytestLineRe.FindStringSubmatch(output)
	if m == nil {
		return testSummary{}
	}
	var passed, failed int
	if m[1] != "" { // "N failed" (optionally ", N passed")
		failed, _ = strconv.Atoi(m[1])
		if m[2] != "" {
			passed, _ = strconv.Atoi(m[2])
		}
	} else if m[3] != "" { // "N passed" alone (no failures)
		passed, _ = strconv.Atoi(m[3])
	}
	return testSummary{
		framework: "pytest", recognized: true,
		passed: passed, failed: failed, total: passed + failed,
	}
}

func parseJestOutput(output string) testSummary {
	m := jestLineRe.FindStringSubmatch(output)
	if m == nil {
		return testSummary{}
	}
	var failed int
	if m[1] != "" {
		failed, _ = strconv.Atoi(m[1])
	}
	passed, _ := strconv.Atoi(m[3])
	total, _ := strconv.Atoi(m[4])
	return testSummary{
		framework: "jest", recognized: true,
		passed: passed, failed: failed, total: total,
	}
}
