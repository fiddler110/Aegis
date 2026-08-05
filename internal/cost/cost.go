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

	"github.com/fiddler110/aegis/internal/provider"
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
	t.addTokensLocked(u)
	if p, ok := PricingFor(model); ok {
		t.totalUSD += p.CostUSD(u)
	} else {
		t.unpriced++
	}
	return t.totalUSD
}

// AddTokens records one turn's token counts without contributing to the
// dollar total (P10.5). Used for estimated usage (character-derived counts
// from providers that report no real usage, e.g. local/Ollama models): the
// estimate is too rough to price honestly, but it must still count toward
// TotalTokens or the token budget silently ignores every turn a local model
// runs — exactly the guardrail gap dollar-only tracking left open for the
// local-first default posture.
func (t *Tracker) AddTokens(u provider.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addTokensLocked(u)
}

func (t *Tracker) addTokensLocked(u provider.Usage) {
	t.turns++
	t.usage.InputTokens += u.InputTokens
	t.usage.OutputTokens += u.OutputTokens
	t.usage.CacheCreationTokens += u.CacheCreationTokens
	t.usage.CacheReadTokens += u.CacheReadTokens
}

// AddWorkerCost folds a subprocess sub-agent's self-reported cumulative spend
// into this tracker (P10.3). A subprocess worker runs in a separate process,
// so it can't share this *Tracker directly the way an in-process sub-agent
// does via ctx — it tracks its own totals locally and reports them back once
// it exits, so a sibling spawned afterward sees the updated totals when the
// parent computes its remaining budget. The token count is lumped into
// InputTokens since the input/output/cache breakdown isn't preserved across
// the process boundary; TotalTokens() is unaffected by how the total is
// distributed. TotalGeneratedTokens (P59.4) *is* affected — a subprocess
// worker's generated tokens are not recoverable from a single lumped total, so
// they count toward the parent's context budget and not toward its generation
// budget. Under-counting there is the safe direction: the worker enforced its
// own inherited generation cap in its own process.
func (t *Tracker) AddWorkerCost(costUSD float64, tokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns++
	t.totalUSD += costUSD
	t.usage.InputTokens += tokens
}

// TotalUSD returns the cumulative estimated cost.
func (t *Tracker) TotalUSD() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalUSD
}

// TotalTokens returns the cumulative token count (input + output + cache
// creation + cache read) across every turn recorded via Add or AddTokens —
// the always-enforceable budget primitive (P10.5): unlike TotalUSD, it is
// never zero just because a model is unpriced or its usage was estimated.
func (t *Tracker) TotalTokens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage.InputTokens + t.usage.OutputTokens + t.usage.CacheCreationTokens + t.usage.CacheReadTokens
}

// TotalGeneratedTokens returns the cumulative *output* token count across every
// turn recorded via Add or AddTokens — i.e. tokens the model actually produced,
// with no input, cache-read or cache-creation tokens folded in (P59.4).
//
// It exists because TotalTokens answers a billing question and users on a local
// backend ask a work question with the same words. On Ollama, prompt_eval_count
// is the *full* prompt every turn rather than a per-turn delta (see
// ollama.go's translate doc), so TotalTokens over an N-turn run is ~O(N²) in
// conversation length: a 20-turn run on an 8k window reports ~160k tokens while
// the model may have generated a few thousand. Summing the full prompt is
// exactly right when you are billed on it and misleading when you are not, so
// the two quantities get two accessors and two budget keys rather than one key
// whose meaning depends on the provider.
func (t *Tracker) TotalGeneratedTokens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage.OutputTokens
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
