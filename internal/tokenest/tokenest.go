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

// ImageBlockTokens is the flat per-image charge Message applies to an
// ImageBlock. It is a documented constant rather than a function of the block's
// size, because the block carries base64-encoded *compressed* bytes and every
// vision provider prices an image by its pixel dimensions: a 40KB JPEG and a
// 40KB PNG of the same scene tokenize identically and cost wildly different
// numbers of bytes, so a length-derived number would be precision this estimator
// has no basis for. The value is the ceiling both major vision providers land
// on for a full-size image (Anthropic caps a single image near 1,600 tokens;
// OpenAI's high-detail tiling tops out around 1,100), chosen at the top of that
// range on the same rule the rest of this file follows: an over-estimate
// compacts early, an under-estimate lets the backend silently drop the oldest
// turns. It is exported so a caller reasoning about headroom can name the same
// number rather than re-deriving one.
const ImageBlockTokens = 1600

// Message estimates the token count of a single message's content blocks.
//
// Every block type the wire carries is priced here. Images and thinking used to
// be counted as free (LLM-07), which is an undercount in the one direction that
// matters: a vision turn or a reasoning model's replayed thinking is real prompt
// the backend charges for, and a transcript the estimator believes is smaller
// than it is compacts late — after the server has already truncated it.
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
		case provider.ImageBlock:
			n += ImageBlockTokens
		case provider.ThinkingBlock:
			// The reasoning text is ordinary text and prices as such. The
			// signature is opaque provider bytes, but it does go on the wire
			// (anthropic's toWireMessages replays it verbatim, and it must, or
			// the provider rejects the next tool use), so it is counted rather
			// than assumed free.
			n += Estimate(v.Text) + Estimate(v.Signature)
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

// MinCompletionTokens is the floor ClampCompletionTokens never goes below,
// shared by the OpenAI-compat and native Ollama adapters' completion-length
// clamps (P59.1/P61.4). A prompt that has already eaten its whole window is a
// situation compaction was supposed to prevent, and the honest completion
// budget there is a negative number — which a shared-context-window backend
// reads as "generate until the context is full," the exact behavior being
// avoided. Asking for a short answer instead at least leaves the model able
// to say something (typically "I can't fit this"), which is more recoverable
// than a generation that truncates mid-sentence.
const MinCompletionTokens = 512

// ClampCompletionTokens bounds a requested completion length (maxTokens) by
// the room actually left in a served window that covers prompt *and*
// completion out of one budget — Ollama's num_ctx, whether reached through the
// native adapter's num_predict or the OpenAI-compat adapter's max_tokens.
//
// Without this, provider.max_tokens (default 32768) rides through untouched
// against e.g. a stock 4096-token window — 8x the whole window in requested
// output. The model then runs into the ceiling mid-generation, which comes
// back as a "length" stop reason, the engine's "continue from where you left
// off" retry, and context growth until the run burns to its iteration cap:
// front-truncation reached through generation instead of through prompt
// growth, which is the one direction the context subsystem was not watching.
//
// The estimate is the same script-aware one the engine compacts against
// (Messages), so every caller agrees about how full a window is. It is only
// an estimate, hence the 5% margin; the clamp is deliberately
// one-directional — it never *raises* maxTokens, so a caller asking for a
// short answer keeps getting one. Callers are responsible for their own
// gating on top of this (whether the backend actually shares one budget
// across prompt and completion at all) — this function only does the
// arithmetic once both callers have decided the gate is open.
func ClampCompletionTokens(maxTokens, numCtx int, system string, msgs []provider.Message) int {
	if maxTokens <= 0 || numCtx <= 0 {
		return maxTokens
	}
	headroom := numCtx - Messages(system, msgs) - numCtx/20
	if headroom >= maxTokens {
		return maxTokens
	}
	if headroom < MinCompletionTokens {
		headroom = MinCompletionTokens
	}
	if headroom > numCtx {
		headroom = numCtx
	}
	return headroom
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
