package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectTestCommand(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{"go", "go.mod", "go test ./..."},
		{"cargo", "Cargo.toml", "cargo test"},
		{"pytest-pyproject", "pyproject.toml", "pytest"},
		{"pytest-ini", "pytest.ini", "pytest"},
		{"pytest-setup", "setup.py", "pytest"},
		{"npm", "package.json", "npm test"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, c.file), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd, errMsg := detectTestCommand(root)
			if cmd != c.want {
				t.Errorf("detectTestCommand() = %q, %q; want %q", cmd, errMsg, c.want)
			}
		})
	}
}

func TestDetectTestCommandPrefersYarnAndPnpmLockfiles(t *testing.T) {
	root := t.TempDir()
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json")
	write("pnpm-lock.yaml")
	if cmd, _ := detectTestCommand(root); cmd != "pnpm test" {
		t.Errorf("got %q, want pnpm test", cmd)
	}
}

func TestDetectTestCommandNoneRecognized(t *testing.T) {
	root := t.TempDir()
	cmd, errMsg := detectTestCommand(root)
	if cmd != "" {
		t.Errorf("expected no command, got %q", cmd)
	}
	if !strings.Contains(errMsg, "could not auto-detect") {
		t.Errorf("expected an explanatory error, got %q", errMsg)
	}
}

func TestParseGoTestOutput(t *testing.T) {
	out := `=== RUN   TestFoo
--- PASS: TestFoo (0.00s)
=== RUN   TestBar
--- FAIL: TestBar (0.00s)
    bar_test.go:10: assertion failed
FAIL
exit status 1
FAIL	example.com/pkg	0.003s
`
	s := parseGoTestOutput(out)
	if !s.recognized {
		t.Fatal("expected recognized go test output")
	}
	if s.passed != 1 || s.failed != 1 {
		t.Errorf("passed=%d failed=%d, want 1/1", s.passed, s.failed)
	}
	if len(s.failingNames) != 1 || s.failingNames[0] != "TestBar" {
		t.Errorf("failingNames = %v, want [TestBar]", s.failingNames)
	}
}

func TestParsePytestOutput(t *testing.T) {
	cases := []struct {
		name         string
		out          string
		wantPassed   int
		wantFailed   int
		wantRecogize bool
	}{
		{"mixed", "===== 2 failed, 10 passed in 1.23s =====", 10, 2, true},
		{"all passed", "===== 5 passed in 0.12s =====", 5, 0, true},
		{"unrecognized", "no summary here", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := parsePytestOutput(c.out)
			if s.recognized != c.wantRecogize {
				t.Fatalf("recognized = %v, want %v", s.recognized, c.wantRecogize)
			}
			if !c.wantRecogize {
				return
			}
			if s.passed != c.wantPassed || s.failed != c.wantFailed {
				t.Errorf("passed=%d failed=%d, want %d/%d", s.passed, s.failed, c.wantPassed, c.wantFailed)
			}
		})
	}
}

func TestParseJestOutput(t *testing.T) {
	out := "Tests:       2 failed, 10 passed, 12 total"
	s := parseJestOutput(out)
	if !s.recognized {
		t.Fatal("expected recognized jest output")
	}
	if s.passed != 10 || s.failed != 2 || s.total != 12 {
		t.Errorf("got passed=%d failed=%d total=%d, want 10/2/12", s.passed, s.failed, s.total)
	}
}

func TestParseCargoTestOutput(t *testing.T) {
	out := "test result: FAILED. 3 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out"
	s := parseCargoTestOutput(out)
	if !s.recognized {
		t.Fatal("expected recognized cargo output")
	}
	if s.passed != 3 || s.failed != 1 {
		t.Errorf("got passed=%d failed=%d, want 3/1", s.passed, s.failed)
	}
}

func TestTestSummaryHeaderUnrecognizedIsEmpty(t *testing.T) {
	var s testSummary
	if h := s.header(); h != "" {
		t.Errorf("expected empty header for unrecognized summary, got %q", h)
	}
}

// TestRunTestsIntegration runs the run_tests tool against this repo's own
// internal/logging package (a small, fast package) and checks the parsed
// go-test header appears alongside the raw output.
func TestRunTestsIntegration(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("repo root not found at %s: %v", root, err)
	}
	tool := newTestRunnerTool(root, 60, nil)
	res, err := tool.Execute(context.Background(), []byte(`{"command":"go test ./internal/logging/..."}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error result: %s", res.Content)
	}
	if !strings.Contains(res.Content, "passed") {
		t.Errorf("expected a parsed pass count in output, got: %s", res.Content)
	}
}
