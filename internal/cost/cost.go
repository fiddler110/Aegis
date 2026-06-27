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

	"github.com/scottymacleod/aegis/internal/provider"
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
	"gpt-4o-mini":  {Input: 0.15, Output: 0.60, CacheRead: 0.075},
	"gpt-4o":       {Input: 2.50, Output: 10, CacheRead: 1.25},
	"gpt-4.1-nano": {Input: 0.10, Output: 0.40, CacheRead: 0.025},
	"gpt-4.1-mini": {Input: 0.40, Output: 1.60, CacheRead: 0.10},
	"gpt-4.1":      {Input: 2, Output: 8, CacheRead: 0.50},
	"gpt-4-turbo":  {Input: 10, Output: 30},
	"o3-mini":      {Input: 1.10, Output: 4.40, CacheRead: 0.55},
	"o3":           {Input: 2, Output: 8, CacheRead: 0.50},
	"o1-mini":      {Input: 1.10, Output: 4.40, CacheRead: 0.55},
	"o1":           {Input: 15, Output: 60, CacheRead: 7.50},

	// Google Gemini (USD / Mtok); approximate list prices.
	"gemini-2.0-flash": {Input: 0.10, Output: 0.40},
	"gemini-1.5-flash": {Input: 0.075, Output: 0.30},
	"gemini-1.5-pro":   {Input: 1.25, Output: 5},

	// Groq (USD / Mtok); open models served cheaply.
	"llama-3.3-70b": {Input: 0.59, Output: 0.79},
	"llama-3.1-8b":  {Input: 0.05, Output: 0.08},
	"mixtral-8x7b":  {Input: 0.24, Output: 0.24},
	"gemma2-9b":     {Input: 0.20, Output: 0.20},
}

// PricingFor returns the pricing for a model id and whether it was found. It
// uses longest-prefix matching, and for vendor-prefixed ids (e.g. OpenRouter's
// "openai/gpt-4o" or "meta-llama/llama-3.3-70b-instruct") it retries against the
// segment after the final "/".
func PricingFor(model string) (Pricing, bool) {
	if p, ok := matchPrefix(model); ok {
		return p, true
	}
	if i := strings.LastIndex(model, "/"); i >= 0 && i+1 < len(model) {
		return matchPrefix(model[i+1:])
	}
	return Pricing{}, false
}

func matchPrefix(model string) (Pricing, bool) {
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
