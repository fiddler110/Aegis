package cost

import (
	"math"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

func TestPricingForLongestPrefix(t *testing.T) {
	p, ok := PricingFor("claude-opus-4-8")
	if !ok {
		t.Fatal("expected opus pricing")
	}
	if p.Input != 5 || p.Output != 25 {
		t.Errorf("opus 4.8 pricing = %+v", p)
	}
	if _, ok := PricingFor("some-unknown-model"); ok {
		t.Error("unknown model should not be priced")
	}
}

// TestClaudePricingByGeneration pins the per-generation rates. A family-only
// entry once priced every Opus at the 4.0 rate, overstating current spend
// ~3-4x and tripping cost.budget_usd early.
func TestClaudePricingByGeneration(t *testing.T) {
	cases := []struct {
		model         string
		input, output float64
	}{
		{"claude-opus-5-5", 4, 20},
		{"claude-opus-5", 5, 25},
		{"claude-opus-4-1", 15, 75},
		{"claude-opus-4-5-20251101", 5, 25},
		{"claude-sonnet-5-5", 2, 10},
		{"claude-sonnet-4-6", 3, 15},
		{"claude-haiku-5-5", 0.10, 0.50},
		{"claude-haiku-4-5-20251001", 1, 5},
		{"claude-fable-5-1", 10, 50},
		{"anthropic/claude-opus-5-5", 4, 20},
	}
	for _, c := range cases {
		p, ok := PricingFor(c.model)
		if !ok || p.Input != c.input || p.Output != c.output {
			t.Errorf("%s pricing = %+v ok=%v; want %v/%v", c.model, p, ok, c.input, c.output)
		}
	}
}

func TestPricingForVendorPrefixed(t *testing.T) {
	// OpenRouter-style ids resolve via the segment after the final "/".
	p, ok := PricingFor("openai/gpt-4o")
	if !ok || p.Input != 2.50 {
		t.Errorf("openai/gpt-4o pricing = %+v ok=%v", p, ok)
	}
	p, ok = PricingFor("meta-llama/llama-3.3-70b-instruct")
	if !ok || p.Input != 0.59 {
		t.Errorf("llama pricing = %+v ok=%v", p, ok)
	}
}

func TestCostUSD(t *testing.T) {
	p := Pricing{Input: 15, Output: 75, CacheWrite: 18.75, CacheRead: 1.50}
	u := provider.Usage{
		InputTokens:         1_000_000,
		OutputTokens:        1_000_000,
		CacheCreationTokens: 1_000_000,
		CacheReadTokens:     1_000_000,
	}
	got := p.CostUSD(u)
	want := 15.0 + 75.0 + 18.75 + 1.50
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("CostUSD = %v, want %v", got, want)
	}
}

func TestTrackerAccumulates(t *testing.T) {
	tr := NewTracker()
	tr.Add("claude-opus-4-8", provider.Usage{InputTokens: 1_000_000})           // $5
	total := tr.Add("claude-opus-4-8", provider.Usage{OutputTokens: 1_000_000}) // +$25
	if math.Abs(total-30) > 1e-9 {
		t.Errorf("cumulative = %v, want 30", total)
	}
	snap := tr.Snapshot()
	if snap.Turns != 2 {
		t.Errorf("turns = %d, want 2", snap.Turns)
	}
	if snap.Usage.InputTokens != 1_000_000 || snap.Usage.OutputTokens != 1_000_000 {
		t.Errorf("usage = %+v", snap.Usage)
	}
}

func TestTrackerUnpricedModel(t *testing.T) {
	tr := NewTracker()
	tr.Add("mystery-model", provider.Usage{InputTokens: 1_000_000})
	snap := tr.Snapshot()
	if snap.TotalUSD != 0 {
		t.Errorf("unpriced model should add no cost, got %v", snap.TotalUSD)
	}
	if snap.Unpriced != 1 {
		t.Errorf("unpriced count = %d, want 1", snap.Unpriced)
	}
	if snap.Usage.InputTokens != 1_000_000 {
		t.Error("tokens should still be counted for unpriced models")
	}
}

// TestAddTokensCountsWithoutCost is the P10.5 regression: estimated usage
// (from local/Ollama models reporting no real usage) must still accumulate
// into TotalTokens so a token budget can enforce it, even though it
// contributes no dollar figure.
func TestAddTokensCountsWithoutCost(t *testing.T) {
	tr := NewTracker()
	tr.AddTokens(provider.Usage{InputTokens: 500_000, OutputTokens: 500_000, IsEstimated: true})
	if got := tr.TotalUSD(); got != 0 {
		t.Errorf("AddTokens must not contribute cost, got %v", got)
	}
	if got := tr.TotalTokens(); got != 1_000_000 {
		t.Errorf("TotalTokens = %d, want 1000000", got)
	}
}

// TestTotalTokensIncludesPricedUsage verifies TotalTokens reflects tokens
// recorded via the normal (priced) Add path too.
func TestTotalTokensIncludesPricedUsage(t *testing.T) {
	tr := NewTracker()
	tr.Add("claude-opus-4-8", provider.Usage{InputTokens: 100, OutputTokens: 200, CacheCreationTokens: 10, CacheReadTokens: 20})
	if got := tr.TotalTokens(); got != 330 {
		t.Errorf("TotalTokens = %d, want 330", got)
	}
}

// TestAddWorkerCostFoldsIntoTotals is the P10.3 regression: a subprocess
// sub-agent worker's self-reported spend must land in both TotalUSD and
// TotalTokens exactly like a normal Add/AddTokens call, so a sibling spawned
// afterward sees it when computing its own remaining budget.
func TestAddWorkerCostFoldsIntoTotals(t *testing.T) {
	tr := NewTracker()
	tr.Add("claude-opus-4-8", provider.Usage{InputTokens: 100, OutputTokens: 100}) // some cost already spent
	before := tr.TotalUSD()

	tr.AddWorkerCost(2.5, 100)

	if got := tr.TotalUSD(); got != before+2.5 {
		t.Errorf("TotalUSD = %v, want %v", got, before+2.5)
	}
	if got := tr.TotalTokens(); got != 300 { // 100 + 100 + 100
		t.Errorf("TotalTokens = %d, want 300", got)
	}
}
