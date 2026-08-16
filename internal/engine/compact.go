package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/toolshim"
	"github.com/fiddler110/aegis/internal/trace"
)

// summarizerGiveUpThreshold is the cumulative number of LLM-summarizer failures
// in one run after which the engine stops calling the summarizer and compacts
// deterministically for the rest of the run (P39.8). Set above the P28.4
// consecutive-failure fallback trigger (2) so a run gets a couple of real
// attempts — enough to ride out a transient error — before concluding the model
// simply can't summarize and latching the LLM call off.
const summarizerGiveUpThreshold = 4

// minPruneYieldFraction is the share of the gap between the estimate and the
// compaction trigger that a *prune-only* compaction has to free to be worth its
// price (P62.7). Below it the guard records the estimate it happened at and
// stops attempting compaction until the conversation has grown by at least that
// gap again.
//
// The price is what makes this necessary: applying a compaction rewrites the
// middle of the conversation, which invalidates the cached token total, forces
// the caller to re-persist, discards a prefix-caching backend's KV for
// everything after the edit (a measured ~2.5s prefill became ~9s), and emits a
// user-visible notice. A pre-pass that freed a handful of characters pays all of
// that and does not move the conversation back under the trigger, so the next
// turn does it again.
//
// The fraction is 0.25 because the measured distributions are nowhere near each
// other. On the P62.7 fixture (TestPruneYieldPerTurnMeasurement, a 24,576-token
// window sized like the live run) eleven consecutive prune-only compactions each
// freed 180 characters / 45 estimated tokens against a gap that grew from 1,462
// to 4,332 — a yield of 0.03 down to 0.01 of the gap — while the one compaction
// that reached the summarizer freed 18,419 tokens, 3.99x the gap. Any threshold
// between about 0.05 and 3 separates them; a quarter sits in the middle of that
// void and matches the shape of internal/compaction's own prunePrefixCacheRatio.
// It is deliberately well under 1.0: a prune that frees a quarter of the excess
// four turns running does bring the conversation back, and should not be
// suppressed.
const minPruneYieldFraction = 0.25

// oversizedSystemPercent is the share of the served context window that the
// *uncompactable* part of a request — the system prompt plus the tool schemas
// that ride with it — may occupy before the run emits a one-off notice (P66.7,
// review finding LLM-16).
//
// The two sources this was written from disagreed: the review recommended "~60%"
// and the roadmap wrote it as `tokenest(system) > 0.5 x window`. 50% wins, and
// not as a compromise — it is the only one of the two that names a real
// boundary. compactionTrigger is floored at window/2 (see engine.go), so
// window/2 is the *lowest* estimate at which proactive compaction can ever fire.
// A fixed prompt at or past that point means every turn of the run is over the
// trigger from its first message, and compaction cannot help because it may not
// touch conv.System: the guard then spends a summarizer call per turn to free
// nothing. That is exactly the condition worth naming, and 60% would let a band
// of runs sit in it unwarned. Being slightly early costs one notice on a run
// with a genuinely tight-but-workable window, which is the cheaper error.
const oversizedSystemPercent = 50

// compactionSuppressCeiling is the estimate past which a low-yield prune may no
// longer defer a compaction attempt: the midpoint between the trigger and the
// window.
//
// Suppression trades headroom for prefill, and past this point there is no
// longer headroom to trade — the trigger fires early precisely to leave room for
// the completion (P59.1), and half of that reservation is as much as a
// yield heuristic gets to spend. It also subsumes the 95%-context-full notice
// path rather than competing with it: compactionTrigger never exceeds 85% of the
// window, so 95% of the window is always above this ceiling, and a run at 95%
// therefore always still attempts a compaction and still reaches the notice when
// that attempt reports nothing to do.
func compactionSuppressCeiling(window, trigger int) int {
	return trigger + (window-trigger)/2
}

// YieldReportingCompactor is an optional capability of a Compactor: it reports
// what a compaction actually achieved, not merely that it achieved something.
//
// It exists because `changed` conflates two outcomes with the same price and
// wildly different value — a summarization that removed half the conversation,
// and a deterministic pre-pass that blanked one stale search dump (P62.7). The
// engine cannot apply a minimum-yield rule to a bool, so the seam is widened
// here rather than in Compactor itself: an optional interface is how this file
// already widens the compactor seam (FallbackCompactor, BudgetedCompactor),
// and it keeps every other Compactor implementation — the test doubles, and any
// caller-supplied one — compiling and behaving exactly as before, which is
// precisely the pre-fix behaviour a guard with no yield information falls back
// to.
//
// It is declared here rather than beside those two in engine.go because P63.9
// made this file the home of the compaction concern, and this interface has no
// reader anywhere else.
//
// The results are plain values rather than a struct because internal/compaction
// must be able to implement this without importing internal/engine: engine's own
// tests import compaction, so a shared struct type would close an import cycle.
type YieldReportingCompactor interface {
	CompactYield(ctx context.Context, system string, msgs []provider.Message) (out []provider.Message, changed, summarized bool, freedTokens int, err error)
}

// FileContextCompactor is an optional capability of a Compactor: it can be told
// which files this run has read and modified, so a summary can carry that set
// forward instead of leaving the model to re-discover the workspace with glob
// and read after every compaction (P65.2).
//
// It is shaped as a context *decorator* rather than as a Compact variant or a
// setter, and both alternatives are wrong for reasons worth recording:
//
//   - A setter (`SetFiles`) cannot work. A Summarizer is built once per server
//     and shared by every session, so two sessions would overwrite each other's
//     paths — a cross-session leak, not merely a stale list. The calibration seam
//     was a setter on the argument that a calibration is process-wide; P66.14
//     (ARCH-07) found the argument false — it carried this run's tool-schema
//     overhead too — and made BudgetedCompactor a context decorator like this
//     one.
//   - A Compact variant would have to be written twice, since the guard calls
//     CompactYield when it can and Compact when it cannot, and every future
//     widening of the seam would double again.
//
// Decorating the context leaves both call paths untouched and keeps the data
// where per-call, caller-scoped data belongs. A Compactor that does not
// implement this is called with an undecorated context and behaves exactly as
// before.
type FileContextCompactor interface {
	WithFiles(ctx context.Context, read, modified []string) context.Context
}

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
	// sharedContextWindow marks a backend positively identified as spending one
	// budget on prompt and completion — an Ollama server on either adapter — and
	// is what makes this run's provider-reported prompt counts admissible as
	// calibration samples. See afterTurn for what it replaced and why.
	sharedContextWindow bool
	logger              *slog.Logger

	// touchedFiles reports the paths this run has read and modified, for a
	// FileContextCompactor to carry forward (P65.2). Never nil.
	touchedFiles func() (read, modified []string)

	// requestOverhead reports what every request carries that conv.System and
	// conv.Messages do not, re-measured whenever the exposed tool set changes.
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
	// It used to be measured once in the constructor, on the argument that a
	// mid-run exposure change moves it by less than the estimate's own error.
	// That argument was wrong by an order of magnitude and P66.14 (PERF-03)
	// retired it: `tool_search`'s reg.Load exposes a previously-deferred tool
	// mid-run, and a single deferred schema is up to 593 estimated tokens
	// against a 4,550-token budget on a small window — 13% of it, where the
	// calibrated estimate's residual error is a few percent. A snapshot taken
	// before the load undercounts the trigger for the rest of the run, in the
	// one direction this whole check exists to avoid.
	//
	// So it is a function, memoized on the registry's schema version: a turn
	// whose exposed set has not changed pays a version read, and one whose set
	// has changed pays the re-render it needs. nil for a run with no registry,
	// where the overhead is 0 by construction — call overhead(), never this.
	requestOverhead func() int

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
	// lastOverhead is the request overhead as it stood for that same request.
	// Recorded rather than re-read because the exposed tool set can now move
	// mid-run (P66.14/PERF-03): a turn that loaded a deferred tool would
	// otherwise be calibrated against the *next* turn's schemas, which is a
	// sample nothing sent.
	lastOverhead int

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

	// systemWarned latches the P66.7 oversized-system-prompt notice to one per
	// run. Its call site (noticeOversizedSystem, from Run's construction block)
	// already fires once, so this is belt-and-braces against a future caller
	// moving it into the turn loop the way beforeTurn is — the condition it
	// reports is constant for the whole run, so a per-turn repeat would be pure
	// noise.
	systemWarned bool

	// lastEvent records what this concern did on the current turn, for the turn
	// trace to pick up (P66.11/GAP-01). It is a *record* rather than an event
	// stream because the trace wants one answer per turn and this guard runs once
	// per turn; beforeTurn clears it, so a turn under the trigger reports nothing
	// rather than repeating the last turn's compaction.
	//
	// It exists because every number in it was already computed here and dropped:
	// LLM-02's closure condition is literally "the turn at which compaction
	// actually fires", and reading it out of Info logs is how that question was
	// answered before.
	lastEvent *trace.Compaction

	// retryEstimate is the estimate at which compaction may be attempted again
	// after a prune whose yield did not justify its cost (P62.7); 0 means no
	// suppression is in force. It is set to the estimate at which the low-yield
	// prune happened *plus* the gap that prune failed to close, so the
	// conversation has to grow by at least the amount that was standing between
	// it and the trigger before the guard spends another rewrite on it. Because
	// the gap doubles each time this fires, the re-attempts back off
	// geometrically instead of on a fixed schedule — a turn count is not the
	// thing that matters here, the distance to the trigger is.
	//
	// Per-run like every other field on this guard, and reset by any compaction
	// that does justify itself.
	retryEstimate int
}

// newCompactionGuard builds the guard for a run.
//
// It never returns nil — see the type comment on why the no-compactor case is
// still this concern's business.
func (e *Engine) newCompactionGuard() *compactionGuard {
	g := &compactionGuard{
		compactor:           e.compactor,
		window:              e.effectiveContextWindow,
		maxTokens:           e.maxTokens,
		sharedContextWindow: e.sharedContextWindow,
		logger:              e.logger,
		// P65.2: read at compaction time, not captured now — the whole point is
		// the set of files touched *before* the compaction, and a compaction
		// happens many turns into a run.
		touchedFiles: e.touchedFiles,
	}
	if e.tools != nil {
		g.requestOverhead = memoizedOverhead(e.tools, e.toolShim)
	}
	return g
}

// memoizedOverhead builds the guard's requestOverhead function: the estimate
// over whatever the exposed tool schemas contribute to a request, recomputed
// only when the registry says the exposed set has changed (P66.14/PERF-03).
//
// It mirrors turn(): under the shim the schemas are rendered into the system
// prompt, otherwise they ride Request.Tools. Either way they are in the prompt
// the backend counts, and either way conv does not hold them.
//
// The memo is not concurrency-safe and does not need to be — the guard is
// per-run and every caller of it (beforeTurn, afterTurn, noticeOversizedSystem)
// runs on the run's own goroutine, between turns rather than inside a parallel
// tool round.
func memoizedOverhead(reg *tool.Registry, shim bool) func() int {
	var (
		haveVersion bool
		version     uint64
		cached      int
	)
	return func() int {
		v := reg.SchemaVersion()
		if haveVersion && v == version {
			return cached
		}
		schemas := reg.Schemas()
		if shim {
			cached = tokenest.Estimate(toolshim.Prompt(schemas))
		} else {
			cached = tokenest.Tools(schemas)
		}
		haveVersion, version = true, v
		return cached
	}
}

// overhead is requestOverhead with the no-registry case folded in: a run with no
// tools attaches nothing to its requests beyond conv, so the overhead is 0.
func (g *compactionGuard) overhead() int {
	if g.requestOverhead == nil {
		return 0
	}
	return g.requestOverhead()
}

// withFileContext hands the compactor the paths this run has touched, when it
// can accept them (P65.2). A compactor that does not implement
// FileContextCompactor gets the context unchanged.
func (g *compactionGuard) withFileContext(ctx context.Context) context.Context {
	fc, ok := g.compactor.(FileContextCompactor)
	if !ok || g.touchedFiles == nil {
		return ctx
	}
	read, modified := g.touchedFiles()
	if len(read) == 0 && len(modified) == 0 {
		return ctx
	}
	return fc.WithFiles(ctx, read, modified)
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
	// The entry pass carries the same budget every later turn does (P66.14): a
	// conversation resumed from a previous session is exactly the case where the
	// two gates disagreeing is most visible, since it can already be over the
	// window before the first turn.
	ctx = g.withTokenBudget(g.withFileContext(ctx), compactionTrigger(g.window(), g.maxTokens))
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

// noticeOversizedSystem emits the P66.7 (finding LLM-16) run-start notice when
// the part of the request no compaction can ever shrink — the system prompt plus
// the tool schemas measured into requestOverhead — already fills
// oversizedSystemPercent of the served window.
//
// It exists because the only pre-existing signal for this was the
// 95%-context-full notice in beforeTurn: after the fact, after a wasted
// summarizer call, and describing a condition that was already true before the
// user's first message. `internal/eval` refuses to run at all under an 8k window
// (insufficientWindowReason); the daemon cannot refuse, so it says so instead.
//
// The remedy named here is the model server's, not Aegis's — hence the wording
// borrowed from ollamainfo.Result.Describe(), so /status and this notice read as
// one voice. A window of zero is "not known yet", not "tiny": warning on missing
// data would fire on every backend that reports its window late.
func (g *compactionGuard) noticeOversizedSystem(conv *Conversation, emit EmitFunc) {
	if g.systemWarned || conv == nil {
		return
	}
	win := g.window()
	if win <= 0 {
		return
	}
	// Uncalibrated on purpose: calib has no samples at run construction, and the
	// figure quoted to the user should be the same estimate for the same prompt
	// on every run.
	fixed := tokenest.Estimate(conv.System) + g.overhead()
	if fixed*100 <= oversizedSystemPercent*win {
		return
	}
	g.systemWarned = true
	emit(Event{Kind: KindNotice, Text: fmt.Sprintf(
		"system prompt and tool schemas are ~%d tokens of a %d-token context window — compaction can never shrink them, so little of the window is left for the conversation; set OLLAMA_CONTEXT_LENGTH or a modelfile num_ctx to raise it",
		fixed, win)})
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
	g.lastRaw, g.lastOverhead = 0, 0
	if !toolsSuppressed {
		g.lastRaw, g.lastOverhead = conv.estimatedTokens(), g.overhead()
	}

	g.lastEvent = nil
	win := g.window()
	if win <= 0 {
		return
	}
	est := g.estimate(conv)
	// P59.1: the trigger reserves room for the *completion* as well as for
	// prompt growth — on a shared prompt+completion budget (Ollama's num_ctx) a
	// prompt that merely fits is not a prompt that can be answered.
	trigger := compactionTrigger(win, g.maxTokens)
	if est <= trigger {
		return
	}
	// P62.7: a previous prune freed too little to be worth its cost and the
	// conversation has not yet grown back to where re-trying could plausibly do
	// better. Return before Compact is even called — the costs this item is about
	// (conv.invalidate() and the "compacted N→N messages" notice) are all
	// downstream of an *applied* compaction, so the only way not to pay them is
	// not to take the attempt.
	if g.suppressed(est, win, trigger) {
		g.lastEvent = &trace.Compaction{Suppressed: true, Estimate: est, Trigger: trigger}
		return
	}
	pct := est * 100 / win
	before := len(conv.Messages)
	// P66.14/LLM-02: the Compactor gates on the same threshold rather than on a
	// flat rule of its own, so hand it the number this turn actually used — see
	// BudgetedCompactor.
	oc := g.compact(g.withTokenBudget(ctx, trigger), conv, emit, pct)
	g.lastEvent = &trace.Compaction{
		Applied:        oc.applied,
		Summarized:     oc.summarized,
		FreedTokens:    oc.freedTokens,
		MessagesBefore: before,
		MessagesAfter:  len(conv.Messages),
		Estimate:       est,
		Trigger:        trigger,
	}
	if !oc.applied && !g.fullWarned && pct >= 95 {
		g.fullWarned = true
		emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full and nothing left to compact — the model server may silently drop older turns; consider /compact or a fresh session", pct)})
	}
	g.recordYield(oc, est, trigger)
	if !toolsSuppressed {
		g.lastRaw, g.lastOverhead = conv.estimatedTokens(), g.overhead()
	}
}

// withTokenBudget hands the compactor this run's view of the token budget — the
// learned estimate correction and the trigger the guard just gated on — when it
// can accept them (P66.14). A compactor that does not implement
// BudgetedCompactor gets the context unchanged and prices the conversation for
// itself, exactly as it did before P62.4.
//
// Pushed on every call rather than on change only, for the reason the old setter
// pushed every sample: the compactor may have been swapped or retuned (the
// daemon re-tunes it on a model switch, P52.1), and re-stating a value it
// already holds costs one context value.
func (g *compactionGuard) withTokenBudget(ctx context.Context, trigger int) context.Context {
	bc, ok := g.compactor.(BudgetedCompactor)
	if !ok {
		return ctx
	}
	scale, samples := g.calib.Scale()
	if samples == 0 {
		// No evidence yet: pass the overhead and the trigger, but leave the
		// scale unset rather than asserting the calibrator's uninitialised
		// default as a measurement.
		scale = 0
	}
	return bc.WithTokenBudget(ctx, g.overhead(), scale, trigger)
}

// event reports what this concern did on the current turn, for the trace. Nil on
// a turn that stayed under the trigger, and on a nil guard.
func (g *compactionGuard) event() *trace.Compaction {
	if g == nil {
		return nil
	}
	return g.lastEvent
}

// suppressed reports whether the P62.7 minimum-yield rule is currently holding
// compaction off for a conversation already over the trigger.
//
// Two conditions have to hold, and the ceiling is the one that keeps this a
// throughput optimization rather than a safety change: however little the last
// prune yielded, a conversation past compactionSuppressCeiling attempts
// compaction anyway. That is also why the 95%-context-full notice is untouched —
// see compactionSuppressCeiling.
func (g *compactionGuard) suppressed(est, win, trigger int) bool {
	if g.retryEstimate <= 0 || est >= g.retryEstimate {
		return false
	}
	if est >= compactionSuppressCeiling(win, trigger) {
		return false
	}
	g.logger.Debug("proactive compaction suppressed: last prune yielded too little",
		"estimate", est, "retry_at", g.retryEstimate, "trigger", trigger)
	return true
}

// recordYield applies the P62.7 minimum-yield rule to the compaction that just
// ran: a prune-only pass that freed less than minPruneYieldFraction of the gap
// it was supposed to close buys the guard a rest, anything better clears one.
//
// A compaction whose yield is unknown (a Compactor that does not implement
// YieldReportingCompactor) never suppresses and never clears — that guard keeps
// its pre-P62.7 behavior exactly, which is the promise the optional interface
// makes.
func (g *compactionGuard) recordYield(oc compactOutcome, est, trigger int) {
	if !oc.applied || !oc.yieldKnown {
		return
	}
	gap := est - trigger
	if oc.summarized || gap <= 0 || float64(oc.freedTokens) >= minPruneYieldFraction*float64(gap) {
		g.retryEstimate = 0
		return
	}
	g.retryEstimate = est + gap
	g.logger.Info("proactive compaction: prune yield below the minimum, deferring further attempts",
		"freed_tokens", oc.freedTokens, "gap", gap, "estimate", est, "retry_at", g.retryEstimate)
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
	return g.calib.Apply(conv.estimatedTokens() + g.overhead())
}

// afterTurn folds the turn's provider-reported prompt size into the calibration
// (P62.4). win is passed in rather than re-read so the sample is checked against
// the window the request was actually served under.
//
// Three conditions decide whether a turn is evidence at all, and all of them are
// about what the reported number *means* rather than how large it is:
//
//   - The backend must be positively identified as one whose InputTokens is the
//     full prompt every turn rather than a delta or a cache-adjusted figure, and
//     whose response to an oversized prompt is to truncate in silence rather than
//     to reject it. That is sharedContextWindow, set from
//     providerfactory.CertainlyOllama.
//
//     It used to be `PromptEvalDurationMS > 0`, which is a *telemetry* field only
//     the native adapter populates — so the correction was inert on the
//     OpenAI-compat path, i.e. on `provider.default: openai` with a `:11434/v1`
//     base_url, which is the configuration docs/providers.md recommends. Every
//     user following the documented setup ran the whole session on the
//     uncorrected 20-33% undercount, with no signal that the calibrator had
//     never taken a sample (P66.14/LLM-03). An adapter's telemetry is not a
//     backend identity, and this gate needed the latter.
//
//   - Cache accounting must be absent. A provider reporting CacheRead or
//     CacheCreation tokens is describing a prompt split across billing
//     categories, and InputTokens there is not comparable to an estimate over
//     the whole conversation.
//
//   - An estimated usage (IsEstimated) is the engine's own heuristic handed back
//     to it — calibrating against it would be a closed loop that always reports
//     perfect accuracy.
//
// A run with no backend identification calibrates nothing, which is the
// pre-P62.4 behaviour and the right default: a cloud API rejects an oversized
// prompt loudly, so the correction buys it nothing worth the risk of learning
// from a number that means something else.
func (g *compactionGuard) afterTurn(usage *provider.Usage, win int) {
	if usage == nil || g.lastRaw <= 0 {
		return
	}
	if usage.IsEstimated || !g.sharedContextWindow {
		return
	}
	if usage.CacheReadTokens > 0 || usage.CacheCreationTokens > 0 {
		return
	}

	before, _ := g.calib.Scale()
	g.calib.Observe(g.lastRaw, g.lastOverhead, usage.InputTokens, win)
	after, samples := g.calib.Scale()
	if after != before {
		g.logger.Debug("token estimate recalibrated",
			"estimate", g.lastRaw+g.lastOverhead,
			"reported", usage.InputTokens,
			"scale", after,
			"samples", samples)
	}
	g.lastRaw, g.lastOverhead = 0, 0
	// The Compactor is told about the correction on the next call rather than
	// here — see withTokenBudget, and BudgetedCompactor for why it is no longer
	// a setter (P66.14/ARCH-07).
}

// compactOutcome is what one compaction attempt achieved. applied is the old
// bool return — the conversation was rewritten, so the caller paid an
// invalidation and emitted a notice. The rest is the P62.7 widening: whether the
// LLM summarizer ran (a summarization is worth its price by construction and is
// never suppressed), how many estimated tokens were freed, and whether the
// compactor was able to say so at all.
type compactOutcome struct {
	applied     bool
	yieldKnown  bool
	summarized  bool
	freedTokens int
}

// compact performs one compaction attempt for a turn already known to be over
// the trigger, and reports what it achieved. pct is carried in only so the
// notices can quote the headroom that provoked them.
func (g *compactionGuard) compact(ctx context.Context, conv *Conversation, emit EmitFunc, pct int) compactOutcome {
	if g.compactor == nil {
		return compactOutcome{}
	}

	// P39.8: once the LLM summarizer has proven unreliable this run, stop
	// calling it — go straight to the deterministic fallback so we don't burn
	// two empty summary calls per compaction cycle on a model that will only
	// ever return empty. The latch is per-run.
	if g.latchedOff {
		out, changed := g.fallback(conv)
		if !changed {
			return compactOutcome{}
		}
		g.logger.Info("proactive compaction: summarizer latched off, using deterministic fallback",
			"before", len(conv.Messages), "after", len(out))
		emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — compacted %d→%d messages (deterministic; summarizer disabled for this run)", pct, len(conv.Messages), len(out))})
		conv.Messages = out
		conv.invalidate()
		// A deterministic fallback drops whole messages, so it is never the
		// low-yield case this file's minimum-yield rule is about — reported as a
		// summarization for that purpose.
		return compactOutcome{applied: true, yieldKnown: true, summarized: true}
	}

	var (
		out        []provider.Message
		changed    bool
		summarized bool
		freed      int
		err        error
		yieldKnown bool
	)
	fileCtx := g.withFileContext(ctx)
	if yc, ok := g.compactor.(YieldReportingCompactor); ok {
		out, changed, summarized, freed, err = yc.CompactYield(fileCtx, conv.System, conv.Messages)
		yieldKnown = true
	} else {
		out, changed, err = g.compactor.Compact(fileCtx, conv.System, conv.Messages)
	}
	if err != nil {
		return g.summarizerFailed(err, conv, emit, pct)
	}
	if !changed {
		return compactOutcome{}
	}
	g.logger.Info("proactive compaction", "before", len(conv.Messages), "after", len(out))
	emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — compacted %d→%d messages", pct, len(conv.Messages), len(out))})
	conv.Messages = out
	conv.invalidate()
	g.failures = 0
	return compactOutcome{applied: true, yieldKnown: yieldKnown, summarized: summarized, freedTokens: freed}
}

// summarizerFailed records one LLM-summarizer failure and applies the P28.4
// deterministic fallback if this was the second consecutive one. It reports
// whether the fallback actually shrank the conversation.
func (g *compactionGuard) summarizerFailed(err error, conv *Conversation, emit EmitFunc, pct int) compactOutcome {
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
		return compactOutcome{}
	}
	out, changed := g.fallback(conv)
	if !changed {
		return compactOutcome{}
	}
	g.logger.Warn("proactive compaction: summarizer failed twice in a row, applied deterministic fallback",
		"before", len(conv.Messages), "after", len(out))
	emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — summarizer unavailable, applied deterministic fallback compaction (%d→%d messages)", pct, len(conv.Messages), len(out))})
	conv.Messages = out
	conv.invalidate()
	g.failures = 0
	// Whole messages dropped, as in the latched-off path above: never the
	// low-yield case.
	return compactOutcome{applied: true, yieldKnown: true, summarized: true}
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
