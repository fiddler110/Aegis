package eval

import "fmt"

// promptProfileNumCtx is the serving context window
// TestLiveWorkflow/LocalPromptProfileReducesFirstTurnTokens pins on its two
// daemons (P63.11). It has to be comfortably larger than either profile's
// first-turn prompt, because that subtest measures prompt size through the
// model's reported prompt_eval_count and a backend clamps that count at the
// window it served: at Ollama's default 4096 both profiles reported exactly
// 4095, so a genuine difference in prompt size became unobservable and the
// subtest blamed P25.6 for what was a saturated instrument.
//
// 16384 is chosen as the smallest power of two that clears the default profile's
// prompt (system prompt + full tool schemas + an oversized repo map, a few
// thousand tokens) with room to spare, while staying inside what a 16GB-VRAM
// box can serve for the small models this tier runs on.
const promptProfileNumCtx = 16384

// saturationReason reports why a pair of prompt-size measurements cannot be
// compared, or "" when both are trustworthy. It is the guard that keeps
// LocalPromptProfileReducesFirstTurnTokens from misreporting an instrument
// failure as a product regression (P63.11).
//
// Two conditions make a measurement unusable, and neither is a statement about
// prompt shape:
//
//   - A count at the served window. The backend truncated the prompt and
//     reported the clamp (Ollama reports num_ctx-1), so the count says how big
//     the window is, not how big the prompt was.
//   - Two identical counts with the window unknown. Independently produced
//     token counts landing on exactly the same integer is far likelier to be
//     two prompts hitting the same ceiling than a coincidence, and with no
//     window to check against, the honest answer is that it cannot be told
//     apart.
//
// A window of 0 means the daemon could not determine one (a non-Ollama backend,
// or detection failing), which disables the first check only.
//
// It lives outside the live_workflow build tag deliberately: the live tier's own
// guard assertions do not run under `go test ./...`, which is how a rotted
// fixture survived a green suite once already, so the decision logic is kept
// where the default suite can cover it.
func saturationReason(localTokens, localWindow, defaultTokens, defaultWindow int) string {
	if clamped(localTokens, localWindow) {
		return fmt.Sprintf("the local profile's input count (%d) is at its served context window (%d), so the prompt was truncated and the count reports the clamp", localTokens, localWindow)
	}
	if clamped(defaultTokens, defaultWindow) {
		return fmt.Sprintf("the default profile's input count (%d) is at its served context window (%d), so the prompt was truncated and the count reports the clamp", defaultTokens, defaultWindow)
	}
	if localTokens == defaultTokens && localWindow <= 0 && defaultWindow <= 0 {
		return fmt.Sprintf("both profiles reported exactly %d input tokens and no served context window is known, which is a ceiling far more often than a coincidence", localTokens)
	}
	return ""
}

// clamped reports whether a token count sits at the serving window. Ollama
// reports num_ctx-1 rather than num_ctx for a truncated prompt, so the boundary
// is inclusive of one below the window.
func clamped(tokens, window int) bool {
	return window > 0 && tokens >= window-1
}
