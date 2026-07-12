// Package modelcatalog provides a small, curated list of recommended models —
// an OpenCode-Zen-style guide so users don't have to guess which model to point
// Aegis at. It is qualitative guidance (not a live benchmark): model IDs and
// availability change, so always confirm against the provider's own docs.
package modelcatalog

import "github.com/fiddler110/aegis/internal/hwinfo"

// Tier is a rough capability/cost bracket.
type Tier string

const (
	TierFrontier Tier = "frontier" // most capable, highest cost
	TierBalanced Tier = "balanced" // strong quality at lower cost/latency
	TierLocal    Tier = "local"    // runs on your own hardware (Ollama, etc.)
)

// Model is one curated recommendation.
type Model struct {
	Provider string
	ID       string
	Tier     Tier
	Context  string // advertised context window, human-readable
	Notes    string
	// MinRAMGB is a rough minimum total system RAM, in GB, below which this
	// TierLocal entry isn't worth recommending (heavy swapping, forced
	// aggressive quantization, or the family simply not shipping a small
	// enough variant). 0 for non-local entries, and for local entries
	// believed to run adequately on any machine worth running Aegis on.
	// Qualitative rule of thumb, not a measured benchmark — see
	// RecommendLocal, which uses this to narrow ForTier(TierLocal) against
	// detected hardware (internal/hwinfo).
	MinRAMGB int
}

// Curated returns the recommendation list. Anthropic IDs are exact; local
// entries are model families (the exact tag depends on what you've pulled);
// other hosted providers are listed with guidance rather than possibly-stale IDs.
func Curated() []Model {
	return []Model{
		// Anthropic (exact IDs).
		{Provider: "anthropic", ID: "claude-opus-4-8", Tier: TierFrontier, Context: "200K", Notes: "Most capable; best for complex agentic and multi-step work."},
		{Provider: "anthropic", ID: "claude-sonnet-4-6", Tier: TierBalanced, Context: "200K", Notes: "Strong general coding/agentic quality at lower cost than Opus."},
		{Provider: "anthropic", ID: "claude-haiku-4-5", Tier: TierBalanced, Context: "200K", Notes: "Fast and inexpensive for routine edits and tool loops."},
		{Provider: "anthropic", ID: "claude-fable-5", Tier: TierFrontier, Context: "200K", Notes: "Creative/long-form strengths; latest Fable line."},

		// Hosted OpenAI-compatible (confirm current IDs with the provider).
		{Provider: "openai", ID: "gpt-5.x (see provider)", Tier: TierFrontier, Context: "—", Notes: "Set provider.model to the current GPT-5-class ID from OpenAI's docs."},
		{Provider: "gemini", ID: "gemini-2.x (see provider)", Tier: TierFrontier, Context: "1M", Notes: "Very large context; use the OpenAI-compatible endpoint or a gateway."},

		// Local via Ollama (model families; pull a specific tag). MinRAMGB
		// reflects the smallest widely-used tag for that family: qwen3 and
		// qwen2.5-coder both ship distilled/quantized-friendly variants down
		// to a few GB, so a conservative "runs acceptably" floor is ~4GB;
		// llama3.1's smallest official tag is 8B, and its 128K context adds
		// real KV-cache memory on top of the weights, so ~8GB; deepseek-r1's
		// reasoning workloads and longer chains-of-thought push the usable
		// floor to ~16GB even on smaller tags. See RecommendLocal's doc
		// comment for how these floors turn into a recommendation.
		{Provider: "ollama", ID: "qwen3", Tier: TierLocal, Context: "32K+", Notes: "Solid local default; reasoning model — Aegis sets think=false by default.", MinRAMGB: 4},
		{Provider: "ollama", ID: "deepseek-r1", Tier: TierLocal, Context: "64K+", Notes: "Strong local reasoning; heavier. Disable think for plain output.", MinRAMGB: 16},
		{Provider: "ollama", ID: "qwen2.5-coder", Tier: TierLocal, Context: "32K+", Notes: "Code-focused; good for local editing tasks.", MinRAMGB: 4},
		{Provider: "ollama", ID: "llama3.1", Tier: TierLocal, Context: "128K", Notes: "General-purpose local model with a large context.", MinRAMGB: 8},
	}
}

// ForTier returns the curated entries in the given tier.
func ForTier(t Tier) []Model {
	var out []Model
	for _, m := range Curated() {
		if m.Tier == t {
			out = append(out, m)
		}
	}
	return out
}

// RecommendLocal narrows ForTier(TierLocal) to the entries whose MinRAMGB
// floor the detected hardware clears. This is qualitative guidance, not a
// measured benchmark (see the package doc comment) — MinRAMGB values are
// rough floors set per curated model in Curated() above, not the result of
// running anything.
//
// When RAM couldn't be detected (hw.RAMKnown() is false — see internal/hwinfo
// for why detection can fail, e.g. an unsupported platform or a sandboxed
// environment without /proc), this returns the full local list unnarrowed:
// it's better to over-offer than to silently hide options because detection
// failed. CPU core count is informational only and does not gate inclusion —
// every curated local model runs on any modern multi-core CPU; RAM (fitting
// weights + KV cache without heavy swapping) is what actually limits which
// local models are worth recommending.
func RecommendLocal(hw hwinfo.Info) []Model {
	local := ForTier(TierLocal)
	if !hw.RAMKnown() {
		return local
	}
	gb := hw.TotalRAMGB()
	out := make([]Model, 0, len(local))
	for _, m := range local {
		if float64(m.MinRAMGB) <= gb {
			out = append(out, m)
		}
	}
	return out
}
