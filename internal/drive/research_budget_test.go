package drive

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/skills"
)

// TestResearchBudgetShrinksForASmallWindow is the P71.11 regression: at
// context_window: 16000 — this project's own shipped local-profile default —
// the old flat numbers (8 rounds, 5-12 sources) were arithmetically
// impossible, per the item's own math (8 * 5 * ~5,000 tokens/source is one to
// two orders of magnitude past a window whose compaction trigger sits at
// 8,000 tokens). The derived budget must actually be smaller there, and must
// leave the old numbers alone at cloud scale and when the window is
// unresolved.
func TestResearchBudgetShrinksForASmallWindow(t *testing.T) {
	cases := []struct {
		window     int
		wantRounds int
		wantLow    int
		wantHigh   int
	}{
		{0, 8, 5, 12},    // unresolved — unchanged cloud-scale default
		{16000, 4, 3, 4}, // the project's own shipped local-profile default
		{32000, 5, 4, 6},
		{64000, 6, 4, 8},
		{128000, 8, 5, 12}, // cloud scale — today's numbers, unchanged
	}
	for _, c := range cases {
		rounds, low, high := researchBudget(c.window)
		if rounds != c.wantRounds || low != c.wantLow || high != c.wantHigh {
			t.Errorf("researchBudget(%d) = (%d, %d, %d), want (%d, %d, %d)",
				c.window, rounds, low, high, c.wantRounds, c.wantLow, c.wantHigh)
		}
	}
}

// TestDeclaredPhasePromptSubstitutesBudget proves the {budget} placeholder
// actually reaches the rendered prompt with a window-derived number, using
// deep-research's own real frontmatter rather than a synthetic fixture — a
// change to the skill's prompt wording that drops {budget} entirely would
// still pass TestDeepResearchDeclaresAPlan (which never renders the prompt)
// but must fail here.
func TestDeclaredPhasePromptSubstitutesBudget(t *testing.T) {
	workDir := t.TempDir()
	if err := skills.MaterializeBuiltinsToProject(workDir, []string{"deep-research"}); err != nil {
		t.Fatal(err)
	}
	sk, ok := skills.Load(workDir, "", []string{"deep-research"}, "deep-research")
	if !ok {
		t.Fatal("deep-research skill not found")
	}
	plan := PlanFor(sk.Name, sk.Phases)
	if len(plan) == 0 {
		t.Fatal("deep-research must have a phase plan")
	}
	research := plan[0]

	small := research.promptFn(PhaseParams{task: "t", skillDir: sk.Dir, cwd: "/ws", contextWindow: 16000})
	if !strings.Contains(small, "Round cap: 4") {
		t.Errorf("16k-window prompt missing the derived round cap; got:\n%s", small)
	}
	if strings.Contains(small, "Round cap: 8") {
		t.Errorf("16k-window prompt should not carry the cloud-scale round cap; got:\n%s", small)
	}

	large := research.promptFn(PhaseParams{task: "t", skillDir: sk.Dir, cwd: "/ws", contextWindow: 128000})
	if !strings.Contains(large, "Round cap: 8") {
		t.Errorf("128k-window prompt missing the cloud-scale round cap; got:\n%s", large)
	}

	if strings.Contains(small, "{budget}") || strings.Contains(large, "{budget}") {
		t.Error("the {budget} placeholder must not survive substitution")
	}
}
