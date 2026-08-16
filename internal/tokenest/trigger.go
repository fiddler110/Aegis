package tokenest

// The compaction thresholds, in one place because two packages gate on them.
//
// P66.14 (finding LLM-02): they used to live in two. `engine.compactionTrigger`
// sized its trigger against the completion the request may ask for (P59.1),
// while `compaction.Summarizer.shouldCompact` applied a flat 20%-free rule that
// never saw maxTokens — so at a 4,096-token window the engine asked for a
// compaction at 2,048 and the summarizer refused until 3,277, and summarization
// finally happened with 819 tokens left for a completion the request had asked
// 32,768 for. The two gates run over the same messages for the same reason, and
// `SetEstimateCorrection` (now WithTokenBudget) exists precisely because they
// must not disagree; the argument had simply never been applied to the trigger
// itself.
//
// This file is that one place. It lives in tokenest rather than in either
// package because both already import it — internal/engine must not import
// internal/compaction (engine's own tests import compaction, so the dependency
// would close a cycle) and internal/compaction must not import internal/engine
// for the same reason. A threshold expressed in estimated tokens belongs beside
// the estimator anyway.
const (
	// LargeContextWindow is the window size above which the thresholds below
	// switch from a ratio to an absolute number of tokens: past a few hundred
	// thousand tokens, a percentage of the window is far more headroom than any
	// prompt-growth or estimator error needs.
	LargeContextWindow = 200_000

	// largeContextPruneLead / smallContextPruneLeadPercent are how far ahead of
	// the compaction trigger the deterministic pre-pass gate sits. They preserve
	// the pre-P66.14 relation between the two gates exactly — the pre-pass ran
	// one step earlier than compaction (25%-free vs 20%-free on a small window,
	// 40k vs 20k on a large one), i.e. 5% of the window or 20k tokens ahead of
	// it — so the pre-pass still gets its chance to bring the conversation back
	// under budget before an LLM summarization call is reached, which is the
	// whole point of running it as a pre-pass.
	largeContextPruneLead        = 20_000
	smallContextPruneLeadPercent = 5

	// triggerCeilingPercent is the flat share of the window that compaction
	// never fires later than, whatever the completion reservation works out to.
	// It is the pre-P59.1 rule, kept as a ceiling: a generous window with a
	// modest max_tokens (a cloud model) behaves exactly as it always did.
	triggerCeilingPercent = 85
)

// CompactionTrigger returns the estimated prompt size at which proactive
// compaction fires for a (window, maxTokens) pair, or 0 when no window is known.
//
// It used to be a flat 85% of the window. That number reserves headroom for
// *prompt growth* and was never sized against generation — but on Ollama
// num_ctx covers prompt and completion out of one budget, so the completion has
// to fit in whatever the prompt leaves. At a 4096 window (Ollama's own server
// default, and a routinely detected one) a flat 85% leaves ~614 tokens for a
// max_tokens configured at 32768, and the run then hits the ceiling mid-answer,
// takes the "continue from where you left off" path, and grows the context
// again on every retry until it burns to maxIterations.
//
// So the trigger is sized against the generation the request may actually ask
// for: window - min(maxTokens, window/2) - a small margin, floored at half the
// window and capped at the old 85%. The min() is what keeps a large max_tokens
// from reserving the entire window and compacting on an empty conversation —
// past the halfway point, reserving more space for output than for the
// conversation is never the right trade.
//
// maxTokens <= 0 means "no completion budget configured" and yields the 85%
// ceiling. Callers that have a maxTokens should pass it: the whole point of
// P66.14 is that a caller which omits it gates later than the one that does.
func CompactionTrigger(window, maxTokens int) int {
	if window <= 0 {
		return 0
	}
	trigger := window * triggerCeilingPercent / 100
	if maxTokens > 0 {
		reserve := maxTokens
		if half := window / 2; reserve > half {
			reserve = half
		}
		// A margin over the reservation itself: the prompt figure this is
		// compared against is an estimate, not a token count.
		if sized := window - reserve - window/20; sized < trigger {
			trigger = sized
		}
	}
	if floor := window / 2; trigger < floor {
		trigger = floor
	}
	return trigger
}

// CompactionPruneTrigger returns the estimated prompt size at which a
// prefix-cache-preserving backend's deterministic pre-pass becomes worth its
// price — one step ahead of CompactionTrigger, so pruning still gets a chance
// to avoid the summarization call rather than landing at the same moment.
//
// See compaction.Summarizer.shouldPrune for why the pre-pass is gated at all on
// a backend that caches the KV of each request's longest common prefix, and for
// the measurements behind it.
func CompactionPruneTrigger(window, maxTokens int) int {
	trigger := CompactionTrigger(window, maxTokens)
	if trigger <= 0 {
		return 0
	}
	if p := trigger - CompactionPruneLead(window); p > 0 {
		return p
	}
	return 0
}

// CompactionPruneLead is how far below the compaction trigger the pre-pass gate
// sits, for a caller that has a trigger of its own and only needs the lead.
func CompactionPruneLead(window int) int {
	if window > LargeContextWindow {
		return largeContextPruneLead
	}
	return window * smallContextPruneLeadPercent / 100
}
