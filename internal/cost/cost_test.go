package cost

import (
	"math"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
)

func TestPricingForLongestPrefix(t *testing.T) {
	p, ok := PricingFor("claude-opus-4-8")
	if !ok {
		t.Fatal("expected opus pricing")
	}
	if p.Input != 15 || p.Output != 75 {
		t.Errorf("opus pricing = %+v", p)
	}
	if _, ok := PricingFor("some-unknown-model"); ok {
		t.Error("unknown model should not be priced")
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
	tr.Add("claude-opus-4-8", provider.Usage{InputTokens: 1_000_000})           // $15
	total := tr.Add("claude-opus-4-8", provider.Usage{OutputTokens: 1_000_000}) // +$75
	if math.Abs(total-90) > 1e-9 {
		t.Errorf("cumulative = %v, want 90", total)
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
