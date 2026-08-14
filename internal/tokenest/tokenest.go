// Package tokenest provides a shared, script-aware token-count heuristic used
// by both the engine (proactive-compaction thresholds, P10.5 estimated usage
// for providers that report none) and the compaction package (its own
// should-compact gate). Keeping a single implementation here is deliberate:
// the two used to maintain separate estimators — the engine a script-aware one
// and compaction a flat chars/4 one — so a CJK/non-ASCII-heavy conversation
// the engine had already decided to compact could be silently no-op'd by
// compaction's cruder gate (P41.1). Neither is a real tokenizer; providers
// that report real usage never go through this path at all.
package tokenest

import "github.com/fiddler110/aegis/internal/provider"

// Estimate approximates a token count for s with a script-aware heuristic
// instead of a flat chars/4. Plain ASCII (the common case for code and English
// prose) is priced at the conventional ~4 characters per token, but CJK
// (Chinese/Japanese/Korean) text tokenizes far denser than that — often close
// to one token per character — so a flat chars/4 estimate silently undercounts
// a CJK-heavy conversation. Other non-ASCII scripts (Cyrillic, Greek, Arabic,
// emoji, ...) are priced at ~2 characters per token as a middle-ground
// approximation.
func Estimate(s string) int {
	var ascii, dense, other int
	for _, r := range s {
		switch {
		case r < 0x80:
			ascii++
		case isDenseScript(r):
			dense++
		default:
			other++
		}
	}
	return (ascii+3)/4 + dense + (other+1)/2
}

// Message estimates the token count of a single message's content blocks.
func Message(m provider.Message) int {
	n := 0
	for _, b := range m.Content {
		switch v := b.(type) {
		case provider.TextBlock:
			n += Estimate(v.Text)
		case provider.ToolUseBlock:
			n += Estimate(v.Name) + Estimate(string(v.Input))
		case provider.ToolResultBlock:
			n += Estimate(v.Content)
		}
	}
	return n
}

// Messages estimates the token count of a system prompt plus a full message
// list — the whole-conversation estimate compaction's should-compact gate runs
// against.
//
// Note what this does *not* cover: a request also carries the tool schemas, and
// the backend counts them in the prompt just like the transcript. See Tools.
func Messages(system string, msgs []provider.Message) int {
	n := Estimate(system)
	for _, m := range msgs {
		n += Message(m)
	}
	return n
}

// toolSchemaEnvelope prices the JSON scaffolding each tool schema is wrapped in
// on the wire — the key names, quotes, braces and separators around the four
// fields below. Each adapter serializes that envelope slightly differently
// (Anthropic nests input_schema, OpenAI wraps the whole thing in a "function"
// object), so this is deliberately a small flat constant rather than an attempt
// to model any one of them: the fields themselves dominate by two orders of
// magnitude, and a per-adapter renderer would be precision the surrounding
// estimate cannot use.
const toolSchemaEnvelope = 8

// Tools estimates the token cost of the tool schemas a request carries.
//
// This exists because leaving it out was a measured defect, not a rounding
// error (P62.4). The engine sets Request.Tools from the registry on every
// native-tool-calling turn, and a local backend counts those schemas in
// prompt_eval_count exactly like transcript text — but Messages above only ever
// sees System and Messages, so the estimate driving proactive compaction
// omitted the schemas entirely. With 50+ builtin tools that is thousands of
// tokens present in every single request and invisible to the one check whose
// job is to compact *before* the server silently drops the oldest turns.
//
// The tool-shim path (P53.6) never had this hole: under the shim the schemas
// are rendered into the system prompt, so Messages counted them all along, and
// the engine measured toolshim.Prompt separately on top. The native path had no
// equivalent — which is why the gap only showed up on a backend using native
// tool calls.
//
// OutputSchema is deliberately *not* counted (P62.6), and the reason is that no
// adapter puts it on the wire: the Anthropic adapter builds its wireTool from
// Name/Description/InputSchema only, the OpenAI adapter sets nothing but
// Function.Parameters, and toolshim.Prompt renders only the input schema.
// ToolSchema.OutputSchema exists for clients and validators (P3.6), not for the
// model. Counting it added ~339 phantom tokens on the local profile's 27
// exposed tools — 4.4% of that base prompt — which this estimator's one
// production caller (engine's compactionGuard.requestOverhead) spent as real
// headroom, firing compaction that much early. TestToolsIgnoresOutputSchema
// pins the omission against the adapters' actual wire shape, because the day an
// adapter starts sending it this becomes an undercount instead.
func Tools(schemas []provider.ToolSchema) int {
	n := 0
	for _, s := range schemas {
		n += Estimate(s.Name) + Estimate(s.Description)
		n += Estimate(string(s.InputSchema))
		n += toolSchemaEnvelope
	}
	return n
}

// isDenseScript reports whether r belongs to a script whose written characters
// each carry roughly a full token's worth of information (CJK Unified
// Ideographs and common extensions, Hiragana, Katakana, Hangul syllables) —
// the scripts responsible for chars/4 most badly underestimating token count.
func isDenseScript(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Unified Ideographs Extension A
		return true
	case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // Hangul syllables
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility Ideographs
		return true
	default:
		return false
	}
}
