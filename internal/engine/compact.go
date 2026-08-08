package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/toolshim"
)

// summarizerGiveUpThreshold is the cumulative number of LLM-summarizer failures
// in one run after which the engine stops calling the summarizer and compacts
// deterministically for the rest of the run (P39.8). Set above the P28.4
// consecutive-failure fallback trigger (2) so a run gets a couple of real
// attempts — enough to ride out a transient error — before concluding the model
// simply can't summarize and latching the LLM call off.
const summarizerGiveUpThreshold = 4

// compactionGuard is the third concern lifted out of Engine.Run under P63.9:
// proactive context compaction, the summarizer's failure/latch bookkeeping, and
// the one context-nearly-full notice a run is allowed to emit.
//
// P63.9 filed this one as half of "the hard pair" on the grounds that compaction
// *mutates conv mid-run*, which the pass-2 technique — name the per-turn state
// and return it as a value instead of storing it — cannot encapsulate. That is
// true and it is also not the obstacle it looked like, which is the finding of
// this pass:
//
//	Mutating shared data is not the same as sharing state.
//
// Every write this concern makes to conv.Messages is its own output, not state
// another concern also writes. So there is nothing here to return as a value,
// because there is no per-turn variable escaping into the next iteration in the
// first place: `pct` and `compacted` were already block-scoped, and the five
// variables that did sit in Run's preamble — the shim's prompt cost, the
// consecutive-failure count, the cumulative LLM-failure count, the summarizer
// latch and the context-full warned flag — all genuinely live for the whole run.
//
// The defect was therefore ownership rather than scope. Five variables declared
// where 600 lines could reach them, touched by one 70-line block, is how Run
// accumulates the appearance of interactions it does not have. Naming the owner
// is the whole fix; the conv rewriting travels with it as a method parameter
// because the rewriting *is* the concern.
//
// # What this concern does genuinely touch that others read
//
// One coupling is real and is deliberately left alone here, because closing it
// is a behavior change and this pass is gated on there being none. Compaction
// rewrites the middle of conv.Messages; nudge retraction (nudgeState.retractAll
// and friends) finds its correctives by scanning that same list for a marker
// prefix. A compaction pass that summarizes away an outstanding nudge leaves
// retraction a no-op while nudgeState still believes the corrective is in the
// transcript — nudgeState.toolFailureOutstanding in particular stays latched,
// suppressing re-injection for the rest of the run. It is benign today (the
// worst case is one corrective not re-sent) and it is the coupling to think
// about before the guard-retry pass, which is the other half of the hard pair
// and keys on the same marker-in-the-transcript mechanism.
//
// A *nil compactor is a run with no summarizer, not a disabled guard: the 95%
// context-full notice is emitted by exactly this concern and fires whether or
// not anything can be compacted — that case (a local server about to silently
// drop the oldest tokens, including the system prompt) is the one it exists for.
// So unlike loopGuard there is no nil-guard form; the nil check is on the
// compactor field, inside.
type compactionGuard struct {
	compactor Compactor
	// window is re-read every turn rather than captured once (P59.7): a mid-run
	// escalation has to reach the trigger, or the recovery it is part of pays
	// for compaction it no longer needs.
	window    func() int
	maxTokens int
	logger    *slog.Logger

	// requestOverhead is what every request carries that conv.System and
	// conv.Messages do not, measured once per run.
	//
	// It lives here rather than in Run because the headroom check is its only
	// consumer: this content rides every request but is attached in turn(),
	// outside conv, so an estimate that omits it undercounts the real prompt by
	// exactly that much — the wrong direction for a check whose whole job is to
	// compact *before* a local server silently truncates.
	//
	// Two things land in it, and for two releases only the first was counted:
	//
	//   - the P53.6 shim's rendered tool catalog, when the shim is on;
	//   - the native tool schemas (Request.Tools), when it is off.
	//
	// The second was the P62.4 defect. The shim path appends its catalog to
	// req.System, so it was visibly missing from an estimate over conv.System
	// and someone accounted for it. The native path attaches the same
	// information to a *different field*, where nothing about the estimate looked
	// wrong — and a backend counts it in prompt_eval_count either way. With 50+
	// builtin tools that is thousands of tokens missing from every request on
	// the path local models actually use.
	//
	// Estimated once rather than per turn: rendering the schemas just to measure
	// them every turn would cost more than the precision is worth, and a mid-run
	// exposure change moves it by less than the estimate's own error.
	requestOverhead int

	// calib learns the residual multiplicative error of the estimate from the
	// prompt token counts the provider reports (P62.4). It corrects the
	// heuristic's own inaccuracy — the part left over once requestOverhead has
	// accounted for content the estimate structurally could not see.
	calib tokenest.Calibrator
	// lastRaw is the uncorrected estimate over conv as it stood when the request
	// for the current turn was assembled, held so afterTurn can pair it with the
	// count that request came back with. Zero means this turn must not be learned
	// from — see afterTurn.
	lastRaw int

	// failures counts *consecutive* proactive-compaction failures (P28.4), reset
	// to 0 on any successful compaction — LLM-summarized or deterministic
	// fallback. Per-run by construction: a single Run already loops through
	// every tool round of a long local-model task, which is the failure mode
	// this guards against.
	failures int
	// llmFailuresTotal is the cumulative (never reset) LLM-summarizer failure
	// count, and latchedOff records that we have given up on the summarizer for
	// the rest of the run (P39.8). A weak local model that reliably returns
	// empty output from the summarization prompt would otherwise be re-tried on
	// every compaction cycle — two wasted LLM calls each time before the
	// deterministic fallback fires (42x "summarizer returned empty output" in
	// one observed run).
	llmFailuresTotal int
	latchedOff       bool

	// fullWarned bounds the context-nearly-full notice to one per run rather
	// than one per turn.
	fullWarned bool
}

// newCompactionGuard builds the guard for a run.
//
// It never returns nil — see the type comment on why the no-compactor case is
// still this concern's business.
func (e *Engine) newCompactionGuard() *compactionGuard {
	g := &compactionGuard{
		compactor: e.compactor,
		window:    e.effectiveContextWindow,
		maxTokens: e.maxTokens,
		logger:    e.logger,
	}
	if e.tools != nil {
		// Mirrors turn(): under the shim the schemas are rendered into the
		// system prompt, otherwise they ride Request.Tools. Either way they are
		// in the prompt the backend counts, and either way conv does not hold
		// them.
		if e.toolShim {
			g.requestOverhead = tokenest.Estimate(toolshim.Prompt(e.tools.Schemas()))
		} else {
			g.requestOverhead = tokenest.Tools(e.tools.Schemas())
		}
	}
	return g
}

// compactOnEntry runs the unconditional pass at the top of a run, before the
// first turn: a conversation resumed from a previous session can already be over
// the window, and the Compactor decides for itself whether there is anything to
// do (changed=false when there is not).
//
// It deliberately shares none of the guard's failure bookkeeping. A failure here
// does not count toward the P39.8 latch, so a run whose entry pass fails still
// pays summarizerGiveUpThreshold further LLM calls before giving up. That
// asymmetry is preserved rather than fixed: it is a behavior change, and this
// pass is gated on there being none.
func (g *compactionGuard) compactOnEntry(ctx context.Context, conv *Conversation) {
	if g.compactor == nil {
		return
	}
	out, changed, err := g.compactor.Compact(ctx, conv.System, conv.Messages)
	if err != nil {
		g.logger.Warn("context compaction failed", "err", err)
		return
	}
	if !changed {
		return
	}
	g.logger.Info("compacted conversation", "before", len(conv.Messages), "after", len(out))
	conv.Messages = out
	conv.invalidate()
}

// beforeTurn is the P2.7 proactive per-turn check: measure token headroom before
// every model turn so context-limit errors never interrupt a run mid-flight.
//
// Cloud providers reject an oversized prompt loudly; local servers (Ollama)
// silently drop the oldest tokens instead — including the system prompt — so
// when nothing can be compacted the user gets an explicit notice rather than a
// model that quietly forgot its instructions.
func (g *compactionGuard) beforeTurn(ctx context.Context, conv *Conversation, emit EmitFunc, toolsSuppressed bool) {
	// Record the basis for this turn's calibration sample before anything else,
	// and re-record it after compaction below: what afterTurn has to pair with
	// the provider's count is the conversation as *sent*, not as it looked on
	// entry. A turn whose schemas were suppressed carries no requestOverhead and
	// must not be learned from at all (see afterTurn), which lastRaw = 0 marks.
	g.lastRaw = 0
	if !toolsSuppressed {
		g.lastRaw = conv.estimatedTokens()
	}

	win := g.window()
	if win <= 0 {
		return
	}
	est := g.estimate(conv)
	// P59.1: the trigger reserves room for the *completion* as well as for
	// prompt growth — on a shared prompt+completion budget (Ollama's num_ctx) a
	// prompt that merely fits is not a prompt that can be answered.
	if est <= compactionTrigger(win, g.maxTokens) {
		return
	}
	pct := est * 100 / win
	if !g.compact(ctx, conv, emit, pct) && !g.fullWarned && pct >= 95 {
		g.fullWarned = true
		emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full and nothing left to compact — the model server may silently drop older turns; consider /compact or a fresh session", pct)})
	}
	if !toolsSuppressed {
		g.lastRaw = conv.estimatedTokens()
	}
}

// estimate is the engine's best guess at the prompt size the backend is about
// to count: the heuristic over conv, plus the content conv cannot see, times
// whatever correction the provider's own numbers have taught us so far.
//
// Every headroom decision in this file goes through here, including the 95%
// context-full notice. That the notice shares the estimate is not incidental —
// P62.4's run stayed silent at 96.7% of the window precisely because the one
// warning designed to fire when compaction *cannot* help was gated on the same
// undercounting number as compaction itself.
func (g *compactionGuard) estimate(conv *Conversation) int {
	return g.calib.Apply(conv.estimatedTokens() + g.requestOverhead)
}

// afterTurn folds the turn's provider-reported prompt size into the calibration
// (P62.4). win is passed in rather than re-read so the sample is checked against
// the window the request was actually served under.
//
// Two conditions decide whether a turn is evidence at all, and both are about
// what the reported number *means* rather than how large it is:
//
//   - PromptEvalDurationMS > 0 identifies the native-Ollama path, the only one
//     where InputTokens is documented to be the full prompt every turn rather
//     than a delta or a cache-adjusted figure (see provider.Usage). It is also
//     the only backend where this correction matters: a cloud API rejects an
//     oversized prompt loudly, while a local server truncates in silence.
//   - Cache accounting must be absent. A provider reporting CacheRead or
//     CacheCreation tokens is describing a prompt split across billing
//     categories, and InputTokens there is not comparable to an estimate over
//     the whole conversation.
//
// An estimated usage (IsEstimated) is the engine's own heuristic handed back to
// it — calibrating against it would be a closed loop that always reports perfect
// accuracy.
func (g *compactionGuard) afterTurn(usage *provider.Usage, win int) {
	if usage == nil || g.lastRaw <= 0 {
		return
	}
	if usage.IsEstimated || usage.PromptEvalDurationMS <= 0 {
		return
	}
	if usage.CacheReadTokens > 0 || usage.CacheCreationTokens > 0 {
		return
	}

	before, _ := g.calib.Scale()
	g.calib.Observe(g.lastRaw, g.requestOverhead, usage.InputTokens, win)
	after, samples := g.calib.Scale()
	if after != before {
		g.logger.Debug("token estimate recalibrated",
			"estimate", g.lastRaw+g.requestOverhead,
			"reported", usage.InputTokens,
			"scale", after,
			"samples", samples)
	}
	g.lastRaw = 0

	// Hand the correction to the Compactor so its own gate prices the
	// conversation the same way this one does. Pushed every sample rather than
	// on change only: the compactor may have been swapped or retuned (the daemon
	// re-tunes it on a model switch, P52.1), and re-stating a value it already
	// holds costs two atomic stores.
	if cc, ok := g.compactor.(CalibratedCompactor); ok && samples > 0 {
		cc.SetEstimateCorrection(g.requestOverhead, after)
	}
}

// compact performs one compaction attempt for a turn already known to be over
// the trigger, and reports whether the conversation actually shrank. pct is
// carried in only so the notices can quote the headroom that provoked them.
func (g *compactionGuard) compact(ctx context.Context, conv *Conversation, emit EmitFunc, pct int) bool {
	if g.compactor == nil {
		return false
	}

	// P39.8: once the LLM summarizer has proven unreliable this run, stop
	// calling it — go straight to the deterministic fallback so we don't burn
	// two empty summary calls per compaction cycle on a model that will only
	// ever return empty. The latch is per-run.
	if g.latchedOff {
		out, changed := g.fallback(conv)
		if !changed {
			return false
		}
		g.logger.Info("proactive compaction: summarizer latched off, using deterministic fallback",
			"before", len(conv.Messages), "after", len(out))
		emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — compacted %d→%d messages (deterministic; summarizer disabled for this run)", pct, len(conv.Messages), len(out))})
		conv.Messages = out
		conv.invalidate()
		return true
	}

	out, changed, err := g.compactor.Compact(ctx, conv.System, conv.Messages)
	if err != nil {
		return g.summarizerFailed(err, conv, emit, pct)
	}
	if !changed {
		return false
	}
	g.logger.Info("proactive compaction", "before", len(conv.Messages), "after", len(out))
	emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — compacted %d→%d messages", pct, len(conv.Messages), len(out))})
	conv.Messages = out
	conv.invalidate()
	g.failures = 0
	return true
}

// summarizerFailed records one LLM-summarizer failure and applies the P28.4
// deterministic fallback if this was the second consecutive one. It reports
// whether the fallback actually shrank the conversation.
func (g *compactionGuard) summarizerFailed(err error, conv *Conversation, emit EmitFunc, pct int) bool {
	g.logger.Warn("proactive compaction failed", "err", err)
	g.failures++
	g.llmFailuresTotal++
	// P39.8: after enough cumulative LLM-summarizer failures this run (not just
	// consecutive), give up on it entirely — a weak local model that reliably
	// returns empty output would otherwise be re-tried every compaction cycle
	// (42x in one observed run).
	if g.llmFailuresTotal >= summarizerGiveUpThreshold && !g.latchedOff {
		g.latchedOff = true
		g.logger.Warn("proactive compaction: disabling LLM summarizer for the rest of this run after repeated failures", "failures", g.llmFailuresTotal)
	}
	// P28.4: the LLM summarizer has now failed twice in a row for this run — a
	// local model unreliably returning empty output (the observed live-eval
	// failure mode) would otherwise skip compaction indefinitely and drift
	// toward the hard context-window ceiling with no safety valve. Fall back to
	// a deterministic, non-LLM shortening pass instead, if the configured
	// Compactor supports one.
	if g.failures < 2 {
		return false
	}
	out, changed := g.fallback(conv)
	if !changed {
		return false
	}
	g.logger.Warn("proactive compaction: summarizer failed twice in a row, applied deterministic fallback",
		"before", len(conv.Messages), "after", len(out))
	emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — summarizer unavailable, applied deterministic fallback compaction (%d→%d messages)", pct, len(conv.Messages), len(out))})
	conv.Messages = out
	conv.invalidate()
	g.failures = 0
	return true
}

// fallback runs the Compactor's deterministic pass, or reports no change when
// the configured Compactor does not implement one. A Compactor that only
// implements Compact keeps the pre-P28.4 warn-and-skip behavior on repeated
// failure.
func (g *compactionGuard) fallback(conv *Conversation) ([]provider.Message, bool) {
	fc, ok := g.compactor.(FallbackCompactor)
	if !ok {
		return nil, false
	}
	return fc.FallbackCompact(conv.Messages)
}
