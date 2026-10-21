package eval

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
}

func materializeGoFix(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := GoFixTask().Materialize(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func criterion(s TaskScore, id string) Criterion {
	for _, c := range s.Criteria {
		if c.ID == id {
			return c
		}
	}
	return Criterion{}
}

func TestGoFixTaskStartsBroken(t *testing.T) {
	requireGo(t)
	out := GoFixTask().Outcome(materializeGoFix(t, nil), "")
	if out.Passed {
		t.Fatalf("the untouched fixture must fail:\n%s", out.Score.Table())
	}
	if c := criterion(out.Score, "fix_correct"); c.Earned != 0 {
		t.Errorf("fix_correct earned on the unfixed fixture: %+v", c)
	}
}

const goFixGoodSource = `package stats

// Average returns the integer mean of nums, or 0 for an empty slice.
func Average(nums []int) int {
	if len(nums) == 0 {
		return 0
	}
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return sum / len(nums)
}
`

const goFixGoodTest = `package stats

import "testing"

func TestAverage(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want int
	}{
		{"empty", nil, 0},
		{"single", []int{5}, 5},
		{"several", []int{2, 4, 6}, 4},
	}
	for _, c := range cases {
		if got := Average(c.in); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
`

func TestGoFixTaskRecognizesARealFix(t *testing.T) {
	requireGo(t)
	out := GoFixTask().Outcome(materializeGoFix(t, map[string]string{
		"stats.go": goFixGoodSource, "stats_test.go": goFixGoodTest,
	}), "")
	if !out.Passed || out.Score.Earned != out.Score.Possible {
		t.Fatalf("a correct fix must earn every point:\n%s", out.Score.Table())
	}
}

// TestGoFixTaskSeparatesFixFromBrokenTest is the P88.2 shape: the fix is right
// but the test file the agent wrote does not compile. The score must say both.
func TestGoFixTaskSeparatesFixFromBrokenTest(t *testing.T) {
	requireGo(t)
	dir := materializeGoFix(t, map[string]string{
		"stats.go":      goFixGoodSource,
		"stats_test.go": `package stats\n\nimport "testing"\n\nfunc TestAverage(t *testing.T) {\n\tt.Log("x")\n}\n`,
	})
	out := GoFixTask().Outcome(dir, "")
	if out.Passed {
		t.Fatalf("an uncompilable test file must not pass:\n%s", out.Score.Table())
	}
	if c := criterion(out.Score, "fix_correct"); c.Earned != c.Points {
		t.Errorf("the correct fix must still be credited despite the broken test:\n%s", out.Score.Table())
	}
	if c := criterion(out.Score, "vet_clean"); c.Earned != 0 {
		t.Errorf("vet_clean must fail on an uncompilable file:\n%s", out.Score.Table())
	}
	// The oracle must leave the agent's file where it found it.
	if _, err := os.Stat(filepath.Join(dir, goFixOracleFile)); err == nil {
		t.Error("oracle file left behind")
	}
	if _, err := os.Stat(filepath.Join(dir, "stats_test.go")); err != nil {
		t.Errorf("the agent's test file was not restored: %v", err)
	}
}

func TestGoFixTaskRejectsCheats(t *testing.T) {
	requireGo(t)
	out := GoFixTask().Outcome(materializeGoFix(t, map[string]string{
		"stats.go":      "package stats\n\nfunc Average(nums []int) int { return 0 }\n",
		"stats_test.go": goFixGoodTest,
		"notes.txt":     "scratch",
	}), "")
	if c := criterion(out.Score, "no_cheat"); c.Earned != 0 {
		t.Errorf("an Average that no longer sums must lose no_cheat:\n%s", out.Score.Table())
	}
	if c := criterion(out.Score, "no_scope_creep"); c.Earned != 0 {
		t.Errorf("an extra file must lose no_scope_creep:\n%s", out.Score.Table())
	}
}
