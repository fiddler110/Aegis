package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sizes reports each result's length, which is what every assertion here is
// really about.
func sizes(results []string) []int {
	out := make([]int, len(results))
	for i, r := range results {
		out[i] = len(r)
	}
	return out
}

func totalLen(results []string) int {
	n := 0
	for _, r := range results {
		n += len(r)
	}
	return n
}

// TestRoundUnderBudgetIsUntouched: the bound must not cost anything on the
// ordinary round. Two or three results of a few KiB are nowhere near the budget,
// and a byte-identical return is what proves no spill file was written and no
// notice was added.
func TestRoundUnderBudgetIsUntouched(t *testing.T) {
	root := t.TempDir()
	results := []string{
		strings.Repeat("a", 4_000),
		strings.Repeat("b", 8_000),
		strings.Repeat("c", 100),
	}
	out := CapRound(context.Background(), root, results)
	for i := range results {
		if out[i] != results[i] {
			t.Errorf("result %d was rewritten despite the round fitting (%d bytes total)", i, totalLen(results))
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".aegis", "spill")); !os.IsNotExist(err) {
		t.Errorf("a round under budget wrote a spill directory")
	}
}

// TestParallelRoundIsBoundedInAggregate is P67.1's finding, stated as the case
// that produced it: eight concurrent read tools each landing at their own 32 KiB
// cap put 256 KiB — ~65,000 estimated tokens — into a single user message, with
// every per-call cap respected and nothing bounding the sum.
func TestParallelRoundIsBoundedInAggregate(t *testing.T) {
	root := t.TempDir()
	var results []string
	for i := 0; i < 8; i++ {
		results = append(results, strings.Repeat("x", 32<<10))
	}
	before := totalLen(results)

	out := CapRound(context.Background(), root, results)

	if got := totalLen(out); got > roundResultBudget {
		t.Errorf("round is %d bytes after capping, over the %d budget (was %d)", got, roundResultBudget, before)
	}
	if len(out) != len(results) {
		t.Fatalf("returned %d results for a round of %d — a tool_result would be paired with the wrong tool_use", len(out), len(results))
	}
	// Each surviving slice has to be worth reading, or the model has eight
	// locators and no reason to follow any of them.
	for i, r := range out {
		if len(r) < minInlineResult/2 {
			t.Errorf("result %d trimmed to %d bytes, below anything useful", i, len(r))
		}
	}
}

// TestSpillSelectsBySize is the first of the two details P67.1 asks to be pinned
// with a test rather than discovered later: a round of one huge result and four
// small ones spills the one, not all five.
func TestSpillSelectsBySize(t *testing.T) {
	root := t.TempDir()
	results := []string{
		strings.Repeat("s", 1_000),
		strings.Repeat("s", 1_000),
		strings.Repeat("H", 60<<10), // the one over-large result
		strings.Repeat("s", 1_000),
		strings.Repeat("s", 1_000),
	}
	out := CapRound(context.Background(), root, results)

	if totalLen(out) > roundResultBudget {
		t.Errorf("round still %d bytes, over the %d budget", totalLen(out), roundResultBudget)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if out[i] != results[i] {
			t.Errorf("small result %d was trimmed (%d -> %d bytes); only the large one should be",
				i, len(results[i]), len(out[i]))
		}
	}
	if out[2] == results[2] {
		t.Errorf("the large result was left at %d bytes", len(out[2]))
	}
	if !strings.Contains(out[2], spillDirRel) {
		t.Errorf("the trimmed result names no spill path, so its remainder is unrecoverable:\n%s", out[2])
	}
}

// TestNoticeBytesCountAgainstTheRoundBudget is the second detail: notice bytes are
// reserved out of the cap, so a spilled result's replacement notice has to be
// counted against the round budget too. A budget computed over bodies and then
// handed a locator per result would be over by exactly the notices.
//
// Asserted against the *returned* sizes rather than by reading the notice, since
// that is the quantity the conversation pays for.
func TestNoticeBytesCountAgainstTheRoundBudget(t *testing.T) {
	root := t.TempDir()
	var results []string
	for i := 0; i < 6; i++ {
		results = append(results, strings.Repeat("y", 20<<10))
	}
	out := CapRound(context.Background(), root, results)

	total := totalLen(out)
	if total > roundResultBudget {
		t.Fatalf("round is %d bytes, over the %d budget — the notices are not being counted", total, roundResultBudget)
	}
	// And at least one notice is genuinely present, or the assertion above passed
	// for the wrong reason.
	var withNotice int
	for _, r := range out {
		if strings.Contains(r, "truncated") {
			withNotice++
		}
	}
	if withNotice == 0 {
		t.Errorf("nothing was trimmed, so this proves nothing about notice accounting: sizes %v", sizes(out))
	}
}

// TestEachRoundIsEvaluatedIndependently: a large result in this round and another
// in the next are both fine. The bound is on what arrives *together*, so calling
// it twice with the same one-and-a-bit-budget round must trim each the same
// amount — no state carried between rounds.
func TestEachRoundIsEvaluatedIndependently(t *testing.T) {
	root := t.TempDir()
	round := func() []string {
		return []string{strings.Repeat("p", 30<<10), strings.Repeat("q", 30<<10)}
	}
	first := CapRound(context.Background(), root, round())
	second := CapRound(context.Background(), root, round())

	if a, b := sizes(first), sizes(second); len(a) != len(b) || a[0] != b[0] || a[1] != b[1] {
		t.Errorf("the same round capped differently the second time: %v then %v — the bound is carrying state", a, b)
	}
}

// TestSingleResultRoundIsExempt: a round of one is already bounded by that tool's
// own cap, and the one thing that can exceed the budget alone is an explicit
// read_file window the posture table honors verbatim as the caller's own.
func TestSingleResultRoundIsExempt(t *testing.T) {
	root := t.TempDir()
	huge := strings.Repeat("z", 200<<10)
	out := CapRound(context.Background(), root, []string{huge})
	if len(out) != 1 || out[0] != huge {
		t.Errorf("a single-result round was trimmed to %v bytes; per-call posture owns that decision", sizes(out))
	}
}

// TestRoundCapIsBestEffortOnAnUnwritableWorkspace: the budget is a context bound,
// and a read-only checkout must not be able to lift it. The round is still trimmed
// to fit; only the locator is missing.
func TestRoundCapIsBestEffortOnAnUnwritableWorkspace(t *testing.T) {
	root := t.TempDir()
	// Occupy `.aegis` with a regular file so MkdirAll of .aegis/spill fails —
	// the same forcing TestSpillIsBestEffort uses.
	if err := os.WriteFile(filepath.Join(root, ".aegis"), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	results := []string{strings.Repeat("m", 40<<10), strings.Repeat("n", 40<<10)}
	out := CapRound(context.Background(), root, results)

	if total := totalLen(out); total > roundResultBudget {
		t.Errorf("round is %d bytes with an unwritable workspace, over the %d budget", total, roundResultBudget)
	}
	for i, r := range out {
		if strings.Contains(r, spillDirRel) {
			t.Errorf("result %d names a spill path that was never written:\n%s", i, r)
		}
	}
}

// TestRoundSpillIsReachableByTheModel closes the loop the notice promises: the
// path in the locator has to be one read_file can actually open, which is the
// property P64.1 measured rather than assumed for a tool's own remainder. A
// round-level spill lands in the same place for the same reason.
func TestRoundSpillIsReachableByTheModel(t *testing.T) {
	root := t.TempDir()
	body := strings.Repeat("unique-round-content\n", 4_000)
	out := CapRound(context.Background(), root, []string{body, strings.Repeat("o", 40<<10)})

	var rel string
	for _, r := range out {
		for _, field := range strings.Fields(r) {
			if strings.HasPrefix(field, spillDirRel) {
				rel = field
			}
		}
	}
	if rel == "" {
		t.Fatalf("no spill locator in either result: %v", sizes(out))
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("the locator names a path that cannot be opened: %v", err)
	}
	if len(data) == 0 {
		t.Error("the spill file is empty")
	}
}
