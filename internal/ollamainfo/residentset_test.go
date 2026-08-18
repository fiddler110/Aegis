package ollamainfo

import (
	"strings"
	"testing"
)

// qwen35 is the measured calibration geometry from kvfit_test.go's anchor:
// 135,168 bytes (132 KiB) per token at f16, training context 262144.
func qwen35() KVGeometry {
	return KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256, ContextMax: 262144}
}

// cheapKV is a deliberately smaller geometry — 64 KiB/token — standing in for
// the "different family in the arbiter seat" case. Its exact identity does not
// matter to any assertion here; what matters is that it costs about half what
// qwen35 does per token, so an equal-byte split and an equal-token split give
// visibly different answers.
func cheapKV(contextMax int) KVGeometry {
	return KVGeometry{BlockCount: 32, HeadCountKV: 8, KeyLength: 64, ValueLength: 64, ContextMax: contextMax}
}

// Two seats reading the same debate transcript need comparable room to hold it,
// so the split is by equal token windows even when one model's cache costs twice
// the other's per token. An equal-*byte* split would give the cheap model twice
// the window and starve the expensive one on the identical prompt.
func TestPlanResidentSetSplitsByEqualTokens(t *testing.T) {
	// 4 and 8 bytes per token — small enough that the arithmetic is checkable
	// by hand, which is the point of a policy test.
	a := KVGeometry{BlockCount: 1, HeadCountKV: 1, KeyLength: 1, ValueLength: 1}
	b := KVGeometry{BlockCount: 1, HeadCountKV: 1, KeyLength: 1, ValueLength: 3}
	if bpt, _ := a.BytesPerToken(KVTypeF16); bpt != 4 {
		t.Fatalf("fixture drifted: a costs %d bytes/token, want 4", bpt)
	}
	if bpt, _ := b.BytesPerToken(KVTypeF16); bpt != 8 {
		t.Fatalf("fixture drifted: b costs %d bytes/token, want 8", bpt)
	}

	members := []Member{
		{Model: "a", Geometry: a, WeightsBytes: 1000},
		{Model: "b", Geometry: b, WeightsBytes: 1000},
	}
	// Exactly 100000 tokens' worth of headroom for the pair.
	budget := int64(2000 + 12*100000)

	p, ok, reason := PlanResidentSet(members, budget, KVTypeF16)
	if !ok {
		t.Fatalf("no plan: %s", reason)
	}
	if p.Windows["a"] != p.Windows["b"] {
		t.Errorf("windows differ: a=%d b=%d; the split is by tokens, not bytes", p.Windows["a"], p.Windows["b"])
	}
	if want := 100000 / fitStep * fitStep; p.Windows["a"] != want {
		t.Errorf("window = %d, want %d", p.Windows["a"], want)
	}
	if p.Total > p.Budget {
		t.Errorf("plan overspends: total %d > budget %d", p.Total, p.Budget)
	}
	if p.Weights != 2000 {
		t.Errorf("weights = %d, want 2000", p.Weights)
	}
}

// A member that reaches its training maximum stops consuming budget as the
// search climbs, so the rest of the set keeps growing past the point an
// unclamped equal split would have stopped at. That redistribution is what the
// min(T, ContextMax) clamp buys, and it is worth pinning because losing it
// would silently cap a large-context model at a small one's ceiling.
func TestPlanResidentSetRedistributesPastACappedMember(t *testing.T) {
	a := KVGeometry{BlockCount: 1, HeadCountKV: 1, KeyLength: 1, ValueLength: 1}                    // 4 B/token, uncapped
	b := KVGeometry{BlockCount: 1, HeadCountKV: 1, KeyLength: 1, ValueLength: 3, ContextMax: 10000} // 8 B/token
	members := []Member{
		{Model: "big", Geometry: a, WeightsBytes: 1000},
		{Model: "capped", Geometry: b, WeightsBytes: 1000},
	}
	budget := int64(2000 + 1_200_000)

	p, ok, reason := PlanResidentSet(members, budget, KVTypeF16)
	if !ok {
		t.Fatalf("no plan: %s", reason)
	}
	// 4T + 8*10000 <= 1_200_000  =>  T <= 280000.
	if want := 280000 / fitStep * fitStep; p.Windows["big"] != want {
		t.Errorf("uncapped member got %d, want %d — the capped member's unspent budget did not redistribute", p.Windows["big"], want)
	}
	if want := 10000 / fitStep * fitStep; p.Windows["capped"] != want {
		t.Errorf("capped member got %d, want %d (its training maximum, floored to the step)", p.Windows["capped"], want)
	}
	if p.Total > p.Budget {
		t.Errorf("plan overspends: total %d > budget %d", p.Total, p.Budget)
	}
}

// The proposer and critic seats run the same model in the measured topology, and
// Ollama holds one runner per model *name* — one copy of the weights, one KV
// cache. A planner that budgets them separately double-counts the shared weights
// and refuses a set that fits. This is the correctness case, not a tidiness one:
// the un-deduplicated arithmetic below does not fit the budget the deduplicated
// one has room to spare in.
func TestPlanResidentSetCollapsesSeatsSharingAModel(t *testing.T) {
	q := Member{Model: "aegis-qwen35-9b", Geometry: qwen35(), WeightsBytes: gib(4)}
	arb := Member{Model: "arbiter", Geometry: cheapKV(131072), WeightsBytes: gib(2)}
	budget := gib(8)

	p, ok, reason := PlanResidentSet([]Member{q, q, arb}, budget, KVTypeF16)
	if !ok {
		t.Fatalf("no plan for a shared-model trio: %s", reason)
	}
	if len(p.Models) != 2 {
		t.Fatalf("planned %d models (%v), want 2 — the shared seat did not collapse", len(p.Models), p.Models)
	}
	if p.Collapsed != 1 {
		t.Errorf("Collapsed = %d, want 1", p.Collapsed)
	}
	if p.Weights != gib(6) {
		t.Errorf("weights = %s, want 6.00 GiB — the shared model was counted twice", FormatGiB(p.Weights))
	}

	// The same three seats, given three distinct names, do not fit: 10 GiB of
	// weights against an 8 GiB budget. That is exactly the refusal deduplication
	// exists to avoid.
	q2 := q
	q2.Model = "aegis-qwen35-9b-copy"
	if _, ok, _ := PlanResidentSet([]Member{q, q2, arb}, budget, KVTypeF16); ok {
		t.Error("three distinct models fit an 8 GiB budget they should overflow; the dedupe test proves nothing")
	}
}

// The two ways a set can fail have different fixes — a budget too small for the
// weights needs a smaller model, one that merely squeezes the windows needs
// q8_0 or one fewer seat — so they must not share a reason string.
func TestPlanResidentSetRefusalReasonsAreDistinct(t *testing.T) {
	q := Member{Model: "q", Geometry: qwen35(), WeightsBytes: gib(4)}
	q2 := Member{Model: "q2", Geometry: qwen35(), WeightsBytes: gib(4)}

	_, ok, reason := PlanResidentSet([]Member{q, q2}, gib(1), KVTypeF16)
	if ok {
		t.Fatal("planned 8 GiB of weights into a 1 GiB budget")
	}
	if !strings.Contains(reason, "weights alone") {
		t.Errorf("weights-overflow reason = %q, want it to name the weights", reason)
	}

	// 8 GiB of weights plus 200 MiB leaves room for ~775 tokens across the pair.
	_, ok, reason = PlanResidentSet([]Member{q, q2}, gib(8)+gib(0.2), KVTypeF16)
	if ok {
		t.Fatal("planned a viable window out of 200 MiB of KV headroom for two 9B models")
	}
	if !strings.Contains(reason, "no window above") {
		t.Errorf("floor reason = %q, want it to name the window floor", reason)
	}
	if strings.Contains(reason, "weights alone") {
		t.Errorf("floor refusal is reported as a weights overflow: %q", reason)
	}
}

// One model is the degenerate resident set, and it must give the same answer Fit
// does — otherwise `aegis models --fit` and a one-member plan disagree about the
// same machine, which is the class of split-brain enginecfg.PersonaModel exists
// to prevent one layer up.
func TestPlanResidentSetAgreesWithFitForOneModel(t *testing.T) {
	g := qwen35()
	weights := gib(4)
	budget := gib(10.5)

	want, ok := Fit(g, budget, weights, KVTypeF16)
	if !ok {
		t.Fatal("Fit refused the calibration case")
	}
	if want != 51200 {
		t.Fatalf("calibration drifted: Fit = %d, want 51200 (the figure P69.5 measured)", want)
	}

	p, ok, reason := PlanResidentSet([]Member{{Model: "q", Geometry: g, WeightsBytes: weights}}, budget, KVTypeF16)
	if !ok {
		t.Fatalf("no plan: %s", reason)
	}
	if got := p.Windows["q"]; got != want {
		t.Errorf("plan gives %d for one model, Fit gives %d", got, want)
	}
}

// Regression against the measured Topology 1 budget: two 9B-class seats on a
// 16 GB card (14.5 GiB usable after the WDDM reserve and the compositor) must be
// planned at no less than the 16000 tokens research/debate-topology-plan.md
// hand-fitted and measured as fully GPU-resident.
func TestPlanResidentSetFitsTheMeasuredDebatePair(t *testing.T) {
	members := []Member{
		{Model: "seat-a", Geometry: qwen35(), WeightsBytes: gib(4)},
		{Model: "seat-b", Geometry: qwen35(), WeightsBytes: gib(4)},
	}
	p, ok, reason := PlanResidentSet(members, gib(14.5), KVTypeF16)
	if !ok {
		t.Fatalf("no plan for the measured pair: %s", reason)
	}
	for _, m := range p.Models {
		if p.Windows[m] < 16000 {
			t.Errorf("%s planned at %d tokens, below the hand-fitted 16000 that measured as fully resident", m, p.Windows[m])
		}
	}
	if p.Total > p.Budget {
		t.Errorf("plan overspends: %s of %s", FormatGiB(p.Total), FormatGiB(p.Budget))
	}
}

// P69.6's standing constraint: the co-resident answer is reached without
// lowering BaselineContextWindow. The floor is not adjusted, it is not on this
// path — the multi-member planner floors at MinFittedContextWindow, and
// RecommendContextWindow keeps answering the single-model question exactly as
// before.
func TestPlanResidentSetDoesNotDisturbTheBaselineFloor(t *testing.T) {
	if BaselineContextWindow != 32768 {
		t.Fatalf("BaselineContextWindow = %d, want 32768 — P69.6 must not lower it", BaselineContextWindow)
	}
	if got := RecommendContextWindow(262144); got != 131072 {
		t.Errorf("RecommendContextWindow(262144) = %d, want 131072 unchanged", got)
	}

	members := []Member{
		{Model: "a", Geometry: qwen35(), WeightsBytes: gib(4)},
		{Model: "b", Geometry: cheapKV(131072), WeightsBytes: gib(2)},
	}
	p, ok, reason := PlanResidentSet(members, gib(8), KVTypeF16)
	if !ok {
		t.Fatalf("no plan: %s", reason)
	}
	if p.Windows["a"] >= BaselineContextWindow {
		t.Fatalf("this fixture no longer exercises a sub-baseline plan (window %d)", p.Windows["a"])
	}
	if p.Windows["a"] < MinFittedContextWindow {
		t.Errorf("planned window %d is below the fitted floor %d", p.Windows["a"], MinFittedContextWindow)
	}
}

func TestPlanResidentSetRejectsUnusableMembers(t *testing.T) {
	if _, ok, _ := PlanResidentSet(nil, gib(8), KVTypeF16); ok {
		t.Error("planned an empty set")
	}
	if _, ok, reason := PlanResidentSet([]Member{{Model: "q", Geometry: qwen35(), WeightsBytes: gib(4)}}, 0, KVTypeF16); ok {
		t.Errorf("planned with no budget (reason %q)", reason)
	}
	// An unmeasurable weight figure is a refusal, not a fallback: the tempting
	// substitute (/api/tags on-disk size) overstates a multimodal model by the
	// size of a vision projector that is never resident.
	_, ok, reason := PlanResidentSet([]Member{{Model: "q", Geometry: qwen35()}}, gib(8), KVTypeF16)
	if ok {
		t.Fatal("planned against an unmeasured weight size")
	}
	if !strings.Contains(reason, "no measured weight size") {
		t.Errorf("reason = %q, want it to name the missing measurement", reason)
	}
	// Incomplete geometry likewise yields no plan rather than a guess.
	_, ok, reason = PlanResidentSet([]Member{{Model: "q", Geometry: KVGeometry{BlockCount: 33}, WeightsBytes: gib(1)}}, gib(8), KVTypeF16)
	if ok {
		t.Fatal("planned against an incomplete geometry")
	}
	if !strings.Contains(reason, "no usable KV geometry") {
		t.Errorf("reason = %q, want it to name the geometry", reason)
	}
}

func TestBudgetBytes(t *testing.T) {
	if got := BudgetBytes(0); got != 0 {
		t.Errorf("BudgetBytes(0) = %d, want 0 (no budget stated)", got)
	}
	if got := BudgetBytes(-4); got != 0 {
		t.Errorf("BudgetBytes(-4) = %d, want 0", got)
	}
	if got, want := BudgetBytes(14.5), gib(14.5); got != want {
		t.Errorf("BudgetBytes(14.5) = %d, want %d", got, want)
	}
}
