// Package modelcatalog provides a small, curated list of recommended models —
// an OpenCode-Zen-style guide so users don't have to guess which model to point
// Aegis at. It is qualitative guidance (not a live benchmark): model IDs and
// availability change, so always confirm against the provider's own docs.
package modelcatalog

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
}

// Curated returns the recommendation list. Anthropic IDs are exact; local
// entries are model families (the exact tag depends on what you've pulled);
// other hosted providers are listed with guidance rather than possibly-stale IDs.
func Curated() []Model {
	return []Model{
		// Anthropic (exact IDs).
		{"anthropic", "claude-opus-4-8", TierFrontier, "200K", "Most capable; best for complex agentic and multi-step work."},
		{"anthropic", "claude-sonnet-4-6", TierBalanced, "200K", "Strong general coding/agentic quality at lower cost than Opus."},
		{"anthropic", "claude-haiku-4-5", TierBalanced, "200K", "Fast and inexpensive for routine edits and tool loops."},
		{"anthropic", "claude-fable-5", TierFrontier, "200K", "Creative/long-form strengths; latest Fable line."},

		// Hosted OpenAI-compatible (confirm current IDs with the provider).
		{"openai", "gpt-5.x (see provider)", TierFrontier, "—", "Set provider.model to the current GPT-5-class ID from OpenAI's docs."},
		{"gemini", "gemini-2.x (see provider)", TierFrontier, "1M", "Very large context; use the OpenAI-compatible endpoint or a gateway."},

		// Local via Ollama (model families; pull a specific tag).
		{"ollama", "qwen3", TierLocal, "32K+", "Solid local default; reasoning model — Aegis sets think=false by default."},
		{"ollama", "deepseek-r1", TierLocal, "64K+", "Strong local reasoning; heavier. Disable think for plain output."},
		{"ollama", "qwen2.5-coder", TierLocal, "32K+", "Code-focused; good for local editing tasks."},
		{"ollama", "llama3.1", TierLocal, "128K", "General-purpose local model with a large context."},
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
