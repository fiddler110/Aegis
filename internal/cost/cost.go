// Package cost tracks token spend and converts it to an estimated USD cost
// using a built-in model pricing catalog. It powers the running-cost display in
// the TUI and the optional budget gate that stops a run before it overspends.
//
// Prices are expressed in USD per million tokens and reflect published list
// prices; they are approximate and may drift. Unknown models contribute zero
// cost (tokens are still counted) rather than guessing.
package cost

import (
	"strings"
	"sync"

	"github.com/scottymacleod/agentharness/internal/provider"
)

// Pricing holds per-million-token USD rates for one model.
type Pricing struct {
	Input      float64 // uncached input tokens
	Output     float64 // output tokens
	CacheWrite float64 // cache-creation input tokens (Anthropic: ~1.25x input)
	CacheRead  float64 // cache-read input tokens (Anthropic: ~0.1x input)
}

// catalog maps a model-id prefix to its pricing. Lookup uses longest-prefix
// match so e.g. "claude-opus-4-8" resolves via the "claude-opus" entry.
var catalog = map[string]Pricing{
	// Anthropic (USD / Mtok)
	"claude-opus":   {Input: 15, Output: 75, CacheWrite: 18.75, CacheRead: 1.50},
	"claude-sonnet": {Input: 3, Output: 15, CacheWrite: 3.75, CacheRead: 0.30},
	"claude-haiku":  {Input: 1, Output: 5, CacheWrite: 1.25, CacheRead: 0.10},
	"claude-3-opus": {Input: 15, Output: 75, CacheWrite: 18.75, CacheRead: 1.50},

	// OpenAI (USD / Mtok); cache read where applicable, no separate write rate.
	"gpt-4o-mini": {Input: 0.15, Output: 0.60, CacheRead: 0.075},
	"gpt-4o":      {Input: 2.50, Output: 10, CacheRead: 1.25},
	"gpt-4.1":     {Input: 2, Output: 8, CacheRead: 0.50},
	"o3":          {Input: 2, Output: 8, CacheRead: 0.50},
}

// PricingFor returns the pricing for a model id and whether it was found.
func PricingFor(model string) (Pricing, bool) {
	var (
		best    Pricing
		bestLen int
		found   bool
	)
	for prefix, p := range catalog {
		if strings.HasPrefix(model, prefix) && len(prefix) > bestLen {
			best, bestLen, found = p, len(prefix), true
		}
	}
	return best, found
}

// CostUSD computes the estimated cost of usage under pricing.
func (p Pricing) CostUSD(u provider.Usage) float64 {
	const mtok = 1_000_000.0
	return float64(u.InputTokens)*p.Input/mtok +
		float64(u.OutputTokens)*p.Output/mtok +
		float64(u.CacheCreationTokens)*p.CacheWrite/mtok +
		float64(u.CacheReadTokens)*p.CacheRead/mtok
}

// Tracker accumulates usage and cost across an arbitrary number of turns. It is
// safe for concurrent use.
type Tracker struct {
	mu       sync.Mutex
	totalUSD float64
	usage    provider.Usage
	turns    int
	unpriced int // turns whose model was not in the catalog
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker { return &Tracker{} }

// Add records one turn's usage for the given model and returns the cumulative
// cost in USD.
func (t *Tracker) Add(model string, u provider.Usage) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns++
	t.usage.InputTokens += u.InputTokens
	t.usage.OutputTokens += u.OutputTokens
	t.usage.CacheCreationTokens += u.CacheCreationTokens
	t.usage.CacheReadTokens += u.CacheReadTokens
	if p, ok := PricingFor(model); ok {
		t.totalUSD += p.CostUSD(u)
	} else {
		t.unpriced++
	}
	return t.totalUSD
}

// TotalUSD returns the cumulative estimated cost.
func (t *Tracker) TotalUSD() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalUSD
}

// Snapshot is a point-in-time view of accumulated spend.
type Snapshot struct {
	TotalUSD float64
	Usage    provider.Usage
	Turns    int
	Unpriced int // turns with an unknown (unpriced) model
}

// Snapshot returns the current totals.
func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Snapshot{TotalUSD: t.totalUSD, Usage: t.usage, Turns: t.turns, Unpriced: t.unpriced}
}
