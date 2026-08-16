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

// workflowMinContextWindow is the smallest served context window in which the
// seeded-bug task can be *attempted*. The local base prompt alone measures
// ~4,300 tokens (TestBasePromptComposition_localProfile), and the task needs
// room on top of it for a repo map, a Python traceback fed back as a tool
// result, and three or four turns of history. Below this the run is not a
// measurement of anything: Ollama truncates from the front and the model
// answers a prompt it was only shown part of.
//
// 8192 rather than something closer to the base prompt because the failure this
// guards against is not "the prompt did not fit" — it is "the prompt fit and
// nothing else did", which looks exactly like a model that lost the plot.
const workflowMinContextWindow = 8192

// insufficientWindowReason reports why a served context window makes the
// workflow task unrunnable, or "" when there is enough room. src is the
// daemon's own account of where the number came from (config, loaded, modelfile,
// …), which is the first thing to check when the number is a surprise.
//
// Observed 2026-08-14: with provider.context_window unset and the model not yet
// loaded, detection falls back to ollamainfo.DefaultServeContext (4096) — a
// deliberately conservative floor, since assuming too much window means silent
// truncation. FixSeededBug then failed with the model issuing one nonsensical
// shell call, which reads as a model or harness failure and is neither. Note
// that an OLLAMA_CONTEXT_LENGTH pin on the server does *not* prevent this: it is
// invisible to /api/show, so Aegis cannot see it until /api/ps has a loaded
// model to report. A Modelfile `PARAMETER num_ctx` is visible before load, which
// is why the message names it first.
//
// A window of 0 (undetermined) is not treated as insufficient — that is a
// non-Ollama backend or a detection failure, and refusing to run the tier on it
// would be a guess dressed as a check.
func insufficientWindowReason(window int, src string) string {
	if window <= 0 || window >= workflowMinContextWindow {
		return ""
	}
	if src == "" {
		src = "unknown"
	}
	return fmt.Sprintf("the daemon is planning against a %d-token context window (source: %s), below the %d this task needs — "+
		"the prompt will be truncated from the front and the run would measure that, not the agent. "+
		"Pin the window where Aegis can see it before the model loads: `ollama create <model>-32k -f` a Modelfile with "+
		"`PARAMETER num_ctx 32768`, or set provider.context_window. An OLLAMA_CONTEXT_LENGTH on the server is not enough "+
		"— /api/show does not report it, so detection only sees it once the model is loaded",
		window, src, workflowMinContextWindow)
}

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
