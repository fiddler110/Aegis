// Package compaction keeps conversations within a token budget by summarizing
// older turns with an auxiliary model call (lineage-style compression, as in
// Hermes). Recent turns are preserved verbatim.
package compaction

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
)

const (
	// largeContextWindowThreshold is the token count above which a context window
	// is considered "large" and uses an absolute buffer instead of a ratio. The
	// compaction *trigger* no longer reads it — that rule moved to
	// tokenest.CompactionTrigger under P66.14, where the engine can share it —
	// but the summarization-request fit check below still has its own two
	// regimes.
	largeContextWindowThreshold = tokenest.LargeContextWindow

	// summarizeReserveBuffer / summarizeReserveRatio size the safety reserve
	// held back when checking that the summarization request itself fits the
	// window it exists to stay inside (P53.3). They mirror the trigger
	// constants above — an absolute floor for large windows, a ratio for small
	// ones — at half their size, because they cover a different and smaller
	// error: the trigger buffer absorbs *future* growth, this reserve only has
	// to absorb how wrong `tokenest` can be about text already in hand.
	// tokenest is a script-aware heuristic, not a tokenizer, so an estimate
	// that says a request fits can still be under the truth by a noticeable
	// margin (unusual scripts, dense JSON/base64 tool payloads); without slack
	// the "fits" verdict would be exactly as trustworthy as the estimator, and
	// the whole point of the check is to not issue a request that turns out to
	// be too large.
	summarizeReserveBuffer = 10_000
	summarizeReserveRatio  = 0.10

	// summarizeSystemPrompt and summarizePreamble are the fixed parts of the
	// summarization request. They are constants (rather than inline literals)
	// so the fit check can price the *whole* request, not just the transcript.
	// P65.2 replaced a free-form instruction ("Use terse bullet points") with a
	// fixed skeleton the summarizing model fills in. The reason is not style. The
	// two task shapes this repo already separates are generation and completion:
	// every measurement in the P38.x line says a local model degrades on the
	// first and holds up on the second, which is why scaffold.py pre-writes
	// structure with per-section markers instead of asking for a document. The
	// summarizer was the last place in the engine still asking a local model for
	// unstructured prose, and asking at the worst possible moment — when the
	// context is nearly full and the model has least room to think.
	//
	// The section list is deliberately short. A skeleton is output tokens, and
	// summaryTokens bounds the reply, so a long one crowds out the content it was
	// meant to organize; the fix for that is fewer sections, not a bigger budget.
	// These five are the ones a resumed run cannot reconstruct from the tail:
	// what the run is trying to do, what it must not do, where it got to, and
	// what is next. Anything derivable from the surviving messages was left out.
	summarizeSystemPrompt = "You compress conversation history. Fill in this exact skeleton, keeping every heading " +
		"and omitting nothing the run would need to continue:\n" +
		"## Goal\n## Constraints\n## Progress\n(use Done / In Progress / Blocked)\n## Key Decisions\n## Next Steps\n" +
		"Under each heading use terse bullet points. Preserve decisions made, facts established, " +
		"file paths and identifiers, tool results that matter, and any open or unresolved questions. " +
		"Do not add headings that are not listed, and do not write an introduction or a conclusion."
	summarizePreamble = "Summarize this conversation so far:\n\n"

	// fileListPreamble labels the carried-forward path lists inside the
	// summarization request (P65.2). It tells the summarizer the lists are
	// context rather than transcript, and that it must not restate them — they
	// are re-emitted verbatim by the code, so a model repeating them would spend
	// its bounded reply on bytes it did not have to produce.
	fileListPreamble = "Files this session has already touched (do NOT repeat these in your summary; they are recorded separately):\n"

	// toolResultRuneLimit is the long-standing per-tool-result cap applied when
	// rendering a transcript, independent of any fit-driven shrinking.
	toolResultRuneLimit = 800
)

// blockTruncationLadder is the descending ladder of per-block rune caps tried,
// in order, when the summarization request does not fit its budget. Truncating
// oversized individual blocks comes before dropping whole messages: the failure
// shape this guards against is one very large tool result, and dropping
// messages to deal with that would discard many small useful ones to fix a
// single fat one.
var blockTruncationLadder = []int{4000, 2000, 1000, 500, 250}

// Summarizer implements engine.Compactor.
type Summarizer struct {
	adapter       provider.Adapter
	model         string
	maxBudget     int          // fallback fixed budget when contextWindow == 0; 0 = skip
	contextWindow atomic.Int64 // model context window in tokens; 0 = use maxBudget. Atomic: updatable after construction (late Ollama detection) while Compact runs concurrently.
	keepRecent    int          // minimum number of trailing messages kept verbatim
	summaryTokens int
	// preservePrefixCache makes the deterministic pre-pass headroom-gated
	// instead of unconditional; see shouldPrune.
	preservePrefixCache bool

	// maxTokens is the completion budget the compaction trigger has to reserve
	// room for (P66.14/LLM-02). Atomic for the same reason contextWindow is: the
	// daemon retunes both on a model switch (P52.1) while Compact may be running.
	// 0 means unknown, and yields the flat 85% ceiling — a caller that has a
	// maxTokens should either configure it or pass its own trigger, since the two
	// gates disagreeing is the whole defect this closes.
	maxTokens atomic.Int64
}

// Options configures a Summarizer.
type Options struct {
	Adapter provider.Adapter
	Model   string
	// ContextWindow is the model's context window in tokens. When > 0 it drives
	// smart compaction thresholds. When 0, MaxBudget is used as a fixed fallback.
	ContextWindow int
	// MaxTokens is the per-request completion budget the caller configures on
	// the provider (provider.max_tokens). The compaction trigger reserves room
	// for it, because on a local backend num_ctx covers prompt and completion out
	// of one budget — a prompt that merely fits is not a prompt that can be
	// answered (P59.1, unified with the engine's gate by P66.14).
	//
	// Callers should set it whenever they know it. Left at 0 the trigger falls
	// back to a flat 85% of the window, which is later than the engine's own gate
	// on a small window — and the two gates disagreeing is exactly what P66.14
	// closed.
	MaxTokens int
	// MaxBudget is a fixed token budget. Used only when ContextWindow == 0.
	// A value of 0 means skip auto-compaction entirely (e.g. for local models
	// whose context size is not known). Defaults to 120 000 when ContextWindow
	// is also 0, for backward-compat with cloud providers.
	MaxBudget     int
	KeepRecent    int // default 8
	SummaryTokens int // default 1024
	// PreservePrefixCache tells the summarizer it is talking to a backend that
	// caches the KV of the longest common prefix of each request — a local
	// llama.cpp/Ollama server — where rewriting a message in the middle of the
	// conversation is not free the way it is against a cloud API. When set, the
	// deterministic pre-pass stops running unconditionally and only fires once
	// the conversation is close enough to the window for the space it frees to
	// be worth a prefill recompute (see shouldPrune).
	//
	// Callers set this from config.LocalBackend(provider, base URL), overridable
	// via compaction.preserve_prefix_cache. Measured worth ~1.7x on wall clock —
	// but only once P62.4 corrected the token estimate the compaction trigger
	// runs on; measured against the uncorrected estimate it looked 2.2x worse.
	// See shouldPrune for both numbers and why they differ.
	//
	// It is a plain bool rather than a config type on purpose — internal/compaction
	// answers a question about the *transport's* cost model and has no other
	// reason to know what a Config is.
	PreservePrefixCache bool
}

// New constructs a Summarizer.
func New(opts Options) *Summarizer {
	if opts.ContextWindow <= 0 && opts.MaxBudget <= 0 {
		// Neither set: keep the existing default for cloud providers.
		opts.MaxBudget = 120_000
	}
	if opts.KeepRecent <= 0 {
		opts.KeepRecent = 8
	}
	if opts.SummaryTokens <= 0 {
		opts.SummaryTokens = 1024
	}
	s := &Summarizer{
		adapter:             opts.Adapter,
		model:               opts.Model,
		maxBudget:           opts.MaxBudget,
		keepRecent:          opts.KeepRecent,
		summaryTokens:       opts.SummaryTokens,
		preservePrefixCache: opts.PreservePrefixCache,
	}
	s.contextWindow.Store(int64(opts.ContextWindow))
	s.maxTokens.Store(int64(opts.MaxTokens))
	return s
}

// SetContextWindow updates the context window driving compaction thresholds.
// Safe to call while Compact is running; used when the effective window is
// only learned after construction (e.g. Ollama reports the loaded model's
// real allocation once the first request has loaded it).
func (s *Summarizer) SetContextWindow(tokens int) {
	s.contextWindow.Store(int64(tokens))
}

// SetMaxTokens updates the completion budget the compaction trigger reserves
// room for. Safe to call while Compact is running, and for the same reason
// SetContextWindow is: the daemon retunes the summarizer on a model switch
// (P52.1), and max_tokens is resolved per model alongside the window.
func (s *Summarizer) SetMaxTokens(tokens int) {
	if tokens < 0 {
		tokens = 0
	}
	s.maxTokens.Store(int64(tokens))
}

// ContextWindow reports the window currently driving compaction thresholds (0
// when none is known and the fixed MaxBudget applies instead). It exists so a
// caller that retunes the summarizer can assert which model's window it ended
// up with — the daemon tunes it from the model compaction actually runs on,
// which is not necessarily the global one (P52.1).
func (s *Summarizer) ContextWindow() int {
	return int(s.contextWindow.Load())
}

// estimate prices system+msgs the way the backend will: the raw heuristic, plus
// the request overhead the transcript cannot see, times the learned scale. With
// no budget attached to the call it is exactly EstimateTokens, so a caller that
// supplies none reads the same number this package always did.
//
// The correction arrives per call rather than through a setter — see budget.go
// for why (P66.14/ARCH-07): the overhead is the *calling run's* tool schemas, and
// a Summarizer is shared by every session on the server.
func (s *Summarizer) estimate(b budget, system string, msgs []provider.Message) int {
	n := EstimateTokens(system, msgs) + b.overhead
	if b.scale <= 0 {
		return n
	}
	scaled := float64(n) * b.scale
	out := int(scaled)
	if float64(out) < scaled {
		out++ // round up; see tokenest.Calibrator.Apply
	}
	return out
}

// shouldCompact reports whether the current estimated token count warrants
// compaction given the configured context window or fixed budget.
//
// The window path gates on the *shared* trigger (P66.14/LLM-02): the caller's
// own number when it supplied one, otherwise tokenest.CompactionTrigger over the
// window and completion budget this Summarizer was configured with. It used to
// apply a flat 20%-free rule that never saw maxTokens, which is how the engine
// came to ask for a compaction 1,229 tokens before this package was willing to
// perform one on a stock 4,096-token window. See budget.go.
func (s *Summarizer) shouldCompact(b budget, estimated int) bool {
	if win := int(s.contextWindow.Load()); win > 0 {
		return estimated > b.triggerOr(win, int(s.maxTokens.Load()))
	}
	if s.maxBudget <= 0 {
		return false
	}
	return estimated > s.maxBudget
}

// shouldPrune reports whether the deterministic pre-pass is worth running on
// this call, given the current estimated token count.
//
// Without preservePrefixCache the answer is always yes, which is the behaviour
// this package has always had and the right one for a cloud API: the pass costs
// no LLM call and no I/O, only a scan over messages already in memory, so
// deferring it until the conversation is near the window would keep resending
// every already-committed write payload for however many turns it takes to
// first cross the threshold.
//
// That accounting inverts on a local backend. llama.cpp/Ollama cache the KV of
// the longest common prefix of a request, so an append-only conversation is
// nearly free to prefill, but rewriting anything in the middle discards every
// cached token after that point. The pre-pass rewrites the middle by
// construction — it edits tool results and tool_use inputs in the *prefix*,
// ahead of the keepRecent tail. Instrumenting a 142-minute unattended drive
// against a local Ollama model made the price concrete: of 238 turns, 163 hit
// the prefix cache and prefilled in under 3 seconds (~70-100k tok/s implied),
// and the only two turns whose context *shrank* were also the two slowest
// prefills in the run —
//
//	turn 119: 60,471 -> 57,518 tokens (-2,953), 186.4s prefill (~309 tok/s)
//	turn 171: 82,577 -> 79,751 tokens (-2,826), 312.2s prefill (~255 tok/s)
//
// 8.3 minutes, ~6% of total wall clock, to reclaim ~3.5% of a context that had
// plenty of room left. Every other slow prefill in that run had a large
// *positive* delta, i.e. real new tokens to compute.
//
// So when preservePrefixCache is set the pass is gated on headroom: skip it
// while the conversation is comfortably below the window, run it once the space
// it frees actually buys something. The gate is set one step ahead of the
// compaction trigger (25% free vs 20% free; 40k vs 20k on a large window) so
// pruning still gets its chance to avoid an LLM summarization call rather than
// landing at the same moment. That ordering matters — the same run did hit a
// genuine context overflow later on, and the goal here is "prune when it buys
// real headroom", not "stop pruning".
//
// # The measurement, which had to be taken twice
//
// Measured 2026-08-08 on a forced-compaction fixture
// (TestLiveWorkflowCompactionPrefixCacheGate, qwen3:14b, 24,576-token window,
// same workload both arms), the gate lost badly — 3m19s against 1m32s, with
// every deferred turn costing ~23.7s. Both arms ran byte-identical to turn 10,
// then gate-on sat at a prompt pinned near 23,758 paying a full reprocess every
// turn.
//
// That reading was real and its conclusion was wrong, because the instrument was
// broken. The engine's compaction trigger ran on an estimate that undercounted
// the true prompt by 20-33% (P62.4: it never counted the tool schemas, which
// ride every request and which the backend prices with the transcript). Firing
// that late put *both* arms inside the window where Ollama context-shifts —
// dropping the oldest tokens and reprocessing from scratch on every turn — and
// in that regime the prefix cache is already gone, so the gate had nothing left
// to protect and its deferral was pure cost.
//
// Re-measured after P62.4, same fixture and model, twice:
//
//	gate on:  1m16s / 1m27s wall, ~54,287ms prefill, 3 shrinking turns
//	gate off: 2m7s  / 2m7s  wall, ~98,481ms prefill, 1 shrinking turn
//
// The gate wins ~1.7x on wall clock and ~1.8x on prefill, with no overflow in
// either arm and no turn above 18,654 tokens. The per-turn trace shows the
// mechanism directly: past the trigger, gate-off prunes on *every* turn for a
// yield too small to drop back under it (message counts unchanged — 11->11,
// 13->13, 15->15) and pays a ~9s full prefill each time, while gate-on stays
// append-only at ~2.5s and takes that hit three times.
//
// Two things worth carrying forward. Deferring the prune is only cheap while the
// conversation is genuinely below the window, which is exactly what a correct
// estimate now guarantees — so this gate's value is *contingent* on P62.4 and
// the two should not be reasoned about separately. And the thrash gate-off
// exposes (compaction invoked every turn once past the trigger, pruning for
// almost nothing) is a defect in its own right that this gate only rate-limits
// by accident.
//
// One regime still unmeasured: above largeContextWindowThreshold the gate uses a
// fixed 40k buffer instead of a ratio, so its deferral may end well short of
// saturation. That needs a >200k-token window to test.
//
// A Summarizer with no known context window returns true (prune) even under
// preservePrefixCache: with no window there is no headroom to measure, and
// guessing wrong in that direction only costs a prefill, while guessing wrong
// in the other costs an overflow.
func (s *Summarizer) shouldPrune(b budget, estimated int) bool {
	if !s.preservePrefixCache {
		return true
	}
	win := int(s.contextWindow.Load())
	if win <= 0 {
		return true
	}
	return estimated > b.pruneTriggerOr(win, int(s.maxTokens.Load()))
}

// EstimateTokens approximates token count using the shared script-aware
// heuristic (tokenest). It previously maintained a separate flat chars/4
// estimate, which undercounted CJK/non-ASCII-heavy conversations and could
// silently no-op a compaction the engine's own script-aware estimator had
// already decided was needed (P41.1) — so both now share one implementation.
func EstimateTokens(system string, msgs []provider.Message) int {
	return tokenest.Messages(system, msgs)
}

// Compact runs the cheap deterministic prune pass (stale tool results,
// already-committed write/edit payloads — see pruneStaleToolResults) and
// additionally summarizes the older prefix of the conversation with an LLM call
// if it still exceeds the budget after that pass, returning the rewritten
// message list. It chooses a boundary that preserves tool_use/tool_result
// pairing by cutting only before an assistant message. The prune pass runs on
// every call unless Options.PreservePrefixCache is set, in which case it is
// gated on headroom — see shouldPrune.
func (s *Summarizer) Compact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	out, changed, _, _, err := s.compact(ctx, system, msgs, false)
	return out, changed, err
}

// CompactYield is Compact plus the two facts `changed` cannot carry: whether
// the LLM summarizer actually ran, and how many estimated tokens the call
// freed. It implements engine.YieldReportingCompactor (P62.7).
//
// The distinction is the whole point. `changed=true` is returned both by a
// summarization that removed half the conversation and by a pre-pass that
// blanked one stale search dump — and the caller pays the same price for
// either: a rewritten message list, a cache invalidation and a user-visible
// notice. Measured on the P62.7 fixture, the second kind freed 45 estimated
// tokens against a 1,462-4,332-token gap to the caller's trigger, every turn
// for eleven turns, while the one real summarization freed 18,419. Only the
// caller knows what its gap is, so this reports the yield and lets it decide.
func (s *Summarizer) CompactYield(ctx context.Context, system string, msgs []provider.Message) (out []provider.Message, changed, summarized bool, freedTokens int, err error) {
	return s.compact(ctx, system, msgs, false)
}

// ForceCompact runs the same compaction pass unconditionally, skipping the
// budget checks Compact uses to decide whether compaction is warranted — for
// a user-triggered manual compaction (e.g. the TUI `/compact` command) ahead
// of a tool-heavy stretch the user knows is coming, rather than the automatic
// budget-driven path.
func (s *Summarizer) ForceCompact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	out, changed, _, _, err := s.compact(ctx, system, msgs, true)
	return out, changed, err
}

// compact is the one implementation behind Compact/ForceCompact/CompactYield.
// It reports, alongside the rewritten list: whether anything changed, whether
// the change came from the LLM summarizer (as opposed to the deterministic
// pre-pass alone), and how many estimated tokens the call freed. The two
// public no-yield entry points drop the extra results, so there is exactly one
// place where any of this is decided.
func (s *Summarizer) compact(ctx context.Context, system string, msgs []provider.Message, force bool) (out []provider.Message, changed, summarized bool, freedTokens int, err error) {
	// Deterministic pre-pass — drop stale tool results (superseded file reads,
	// old search dumps, already-committed write/edit payloads) ahead of the
	// budget gate below, so a long tool-heavy run does not keep resending every
	// already-committed payload verbatim for however many turns it takes to
	// first cross the threshold. By default it runs on every call; under
	// PreservePrefixCache it runs only when it buys real headroom, because on a
	// prefix-caching local backend rewriting the middle of the conversation
	// costs a full prefill recompute. shouldPrune carries the reasoning and the
	// measurements. A forced compaction always prunes: the caller has asked for
	// space explicitly and is paying for a summarization call anyway.
	//
	// estBefore is measured once, up front, and serves three readers: the
	// pre-pass gate, the budget gate below (when nothing was pruned), and the
	// yield P62.7 reports. It costs one estimate the pre-P62.7 code did not pay
	// on the *cloud* path when the pre-pass actually freed something — the
	// short-circuited || used to skip it there — and that is the price of being
	// able to say how much a prune freed rather than only that it freed
	// something. Nothing else changed: with no prune the single estimate below
	// is reused, exactly as before.
	b := budgetFrom(ctx)
	estBefore := s.estimate(b, system, msgs)
	if force || !s.preservePrefixCache || s.shouldPrune(b, estBefore) {
		var prunedChars int
		msgs, prunedChars = pruneStaleToolResults(msgs, s.keepRecent)
		changed = prunedChars > 0
	}

	est := estBefore
	if changed {
		est = s.estimate(b, system, msgs)
		if freedTokens = estBefore - est; freedTokens < 0 {
			freedTokens = 0
		}
	}

	if !force && !s.shouldCompact(b, est) {
		return msgs, changed, false, freedTokens, nil
	}

	boundary := s.boundary(msgs)
	if boundary <= 0 {
		return msgs, changed, false, freedTokens, nil // nothing more safe to compact
	}

	prefix := msgs[:boundary]
	summary, err := s.summarize(ctx, prefix)
	if err != nil {
		return msgs, changed, false, freedTokens, err
	}

	out = make([]provider.Message, 0, len(msgs)-boundary+1)
	out = append(out, provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "Summary of earlier conversation (older turns were compacted):\n\n" + summary}},
	})
	out = append(out, msgs[boundary:]...)
	if freedTokens = estBefore - s.estimate(b, system, out); freedTokens < 0 {
		freedTokens = 0
	}
	return out, true, true, freedTokens, nil
}

// FallbackCompact deterministically shortens the conversation without an LLM
// call, for use when the LLM summarizer has failed repeatedly (P28.4 —
// observed live against local models like qwythos:latest/gpt-oss:20b, which
// intermittently return empty output from the summarization prompt). It
// reuses the same boundary selection as Compact/ForceCompact — protecting
// the keepRecent tail and never splitting a tool_use/tool_result pair — but
// replaces the summarized prefix with a terse, deterministically generated
// note (message/tool-call counts) instead of an AI-generated summary, so a
// broken summarizer can never block context from shrinking. Unlike Compact,
// this cannot itself fail: worst case is changed=false when there is nothing
// safe to cut, matching Compact/ForceCompact's own no-op case.
func (s *Summarizer) FallbackCompact(msgs []provider.Message) ([]provider.Message, bool) {
	boundary := s.boundary(msgs)
	if boundary <= 0 {
		return msgs, false
	}
	prefix := msgs[:boundary]
	note := fallbackNote(prefix)
	// P65.2: carry the accumulated file lists through the fallback too. This
	// path fires precisely when a local summarizer is failing, which is the same
	// population the carried lists exist to help — and because it replaces the
	// prefix outright, not re-emitting them here would *permanently* destroy a
	// set that had accumulated across every prior compaction. It cannot include
	// the current run's paths (FallbackCompact takes no context, by the
	// FallbackCompactor interface), so it carries forward what is on record and
	// nothing more, which is strictly better than dropping it.
	if carriedRead, carriedModified := collectCarriedFiles(prefixText(prefix), fileContext{}); len(carriedRead)+len(carriedModified) > 0 {
		note += "\n\n" + renderFileLists(carriedRead, carriedModified)
	}
	out := make([]provider.Message, 0, len(msgs)-boundary+1)
	out = append(out, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "Earlier conversation was dropped by deterministic fallback " +
			"compaction (the AI summarizer failed repeatedly, so no AI-generated summary is available):\n\n" + note}},
	})
	out = append(out, msgs[boundary:]...)
	return out, true
}

// fallbackNote describes what a deterministic fallback compaction dropped —
// message and tool-call counts, and which tools were used — so the model at
// least knows something happened here, even without the verbatim content or
// an LLM's interpretation of it.
func fallbackNote(msgs []provider.Message) string {
	var userTurns, assistantTurns, toolCalls int
	var toolNames []string
	seen := make(map[string]bool)
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleUser:
			userTurns++
		case provider.RoleAssistant:
			assistantTurns++
		}
		for _, blk := range m.Content {
			if tu, ok := blk.(provider.ToolUseBlock); ok {
				toolCalls++
				if !seen[tu.Name] {
					seen[tu.Name] = true
					toolNames = append(toolNames, tu.Name)
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d earlier message(s) (%d user, %d assistant) covering %d tool call(s) were dropped.",
		len(msgs), userTurns, assistantTurns, toolCalls)
	if len(toolNames) > 0 {
		fmt.Fprintf(&b, " Tools used: %s.", strings.Join(toolNames, ", "))
	}
	b.WriteString(" Earlier file contents, decisions, and open tasks from this span are no longer available verbatim — re-read files or ask the user if needed.")
	return b.String()
}

// boundary returns the index of the first assistant message at or after the
// keep-recent cutoff, so the kept suffix starts cleanly and the summarized
// prefix never splits a tool_use/tool_result pair.
func (s *Summarizer) boundary(msgs []provider.Message) int {
	start := len(msgs) - s.keepRecent
	if start < 1 {
		return 0
	}
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role == provider.RoleAssistant {
			return i
		}
	}
	return 0
}

// summarizeFitBudget reports the maximum estimated token size the
// summarization request (system prompt + preamble + transcript) may have, or 0
// when no meaningful budget is known and the fit check is skipped entirely. It
// falls back from the context window to maxBudget the same way the rest of the
// file does. A budget no larger than the reserved summary output cannot be a
// real context window (no model's is) — it is a fixed trigger budget, which
// says nothing about what a request may weigh — so there is nothing coherent
// to fit inside and the check is skipped rather than invented. A negative
// result means even an empty transcript cannot fit; the caller turns that into
// a (non-fatal) error rather than issuing the request anyway.
func (s *Summarizer) summarizeFitBudget() int {
	budget := int(s.contextWindow.Load())
	if budget <= 0 {
		budget = s.maxBudget
	}
	if budget <= s.summaryTokens {
		return 0
	}
	reserve := summarizeReserveBuffer
	if budget <= largeContextWindowThreshold {
		reserve = int(float64(budget) * summarizeReserveRatio)
	}
	return budget - s.summaryTokens - reserve
}

// summarizeRequestTokens estimates the full summarization request — system
// prompt and preamble included, not just the transcript — using the shared
// tokenest estimator (the single token heuristic in this repo).
//
// Deliberately *not* run through the P62.4 correction, which is about the
// conversation request rather than this one. The additive half would be plainly
// wrong: that overhead is the tool schemas, and a summarization request carries
// no tools. The multiplicative half would be defensible, but this path already
// holds back an explicit reserve (summarizeReserveBuffer / summarizeReserveRatio)
// whose stated job is to absorb how wrong tokenest can be about text in hand —
// so correcting here would be double-counting the same error, and would shrink
// the transcript the summary is built from for no measured reason.
// fixed is any caller-supplied text that rides in the same user message ahead of
// the preamble — today, P65.2's carried file lists. It is priced here rather
// than bolted on afterwards for the reason the reserve exists at all: a "fits"
// verdict must be about the request that is actually sent.
func summarizeRequestTokens(transcript, fixed string) int {
	return tokenest.Messages(summarizeSystemPrompt, []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: fixed + summarizePreamble + transcript}}},
	})
}

// fitTranscript renders the prefix into a transcript small enough that the
// summarization request built from it fits the budget from summarizeFitBudget,
// and reports how many of the oldest prefix messages had to be dropped to get
// there (0 in the common case). Shrinking is deterministic and runs in two
// stages: oversized individual blocks are truncated first (middle-out, with a
// visible elision marker), and only if that is still not enough are the oldest
// messages dropped, oldest-first, until it fits.
func (s *Summarizer) fitTranscript(prefix []provider.Message, fixed string) (transcript string, dropped int, err error) {
	full := renderTranscript(prefix)
	budget := s.summarizeFitBudget()
	if budget == 0 {
		return full, 0, nil // no budget to check against
	}
	fits := func(t string) bool { return summarizeRequestTokens(t, fixed) <= budget }
	if budget < 0 || !fits("") {
		// The reserve (or the fixed request scaffolding alone) already exceeds
		// the whole budget: no amount of shrinking can help. Non-fatal — the
		// caller logs this and the run continues uncompacted.
		return "", 0, fmt.Errorf("summarization request cannot fit the context budget (%d tokens available for the transcript)", budget)
	}
	if fits(full) {
		return full, 0, nil
	}

	// Stage 1: truncate oversized blocks, tightening the cap until it fits.
	limit := blockTruncationLadder[len(blockTruncationLadder)-1]
	for _, capRunes := range blockTruncationLadder {
		limit = capRunes
		truncated := renderTranscriptCapped(prefix, capRunes)
		if fits(truncated) {
			return truncated, 0, nil
		}
	}

	// Stage 2: still too large — drop the oldest messages, oldest-first.
	for n := 1; n < len(prefix); n++ {
		truncated := renderTranscriptCapped(prefix[n:], limit)
		if fits(truncated) {
			return truncated, n, nil
		}
	}
	return "", 0, fmt.Errorf("summarization request cannot be shrunk to fit the context budget (%d tokens available for the transcript)", budget)
}

// omissionNote is appended to a summary whose transcript had messages dropped.
// Dropped messages vanish permanently — Compact replaces the entire prefix with
// the summary — so a silently-lossy summary must never present itself as
// complete.
func omissionNote(dropped int) string {
	return fmt.Sprintf("[Compaction note: the %d earliest message(s) of this span were omitted from the summary — "+
		"the transcript was too large to summarize even after truncating oversized blocks, so nothing above reflects their content.]", dropped)
}

// summarize asks the model to condense the prefix transcript, first shrinking
// that transcript (P53.3) until the request it produces fits the window the
// compaction exists to stay inside.
func (s *Summarizer) summarize(ctx context.Context, prefix []provider.Message) (string, error) {
	// P65.2: the file set, merged from what a previous summary carried and what
	// this run reported. Computed *before* fitTranscript and handed to it, so
	// the block is priced by the same fit check as everything else — adding it
	// afterwards would push the request past the budget the reserve exists to
	// defend, which is the one way this feature could turn a working compaction
	// into a failing one.
	readList, modifiedList := collectCarriedFiles(prefixText(prefix), filesFrom(ctx))
	fileBlock := renderFileLists(readList, modifiedList)
	fixed := ""
	if fileBlock != "" {
		fixed = fileListPreamble + fileBlock + "\n"
	}

	transcript, dropped, err := s.fitTranscript(prefix, fixed)
	if err != nil {
		return "", err
	}

	userText := fixed + summarizePreamble + transcript
	req := provider.Request{
		Model:     s.model,
		MaxTokens: s.summaryTokens,
		System:    summarizeSystemPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: userText}}},
		},
		SuppressCache: true,
		// P67.3: a summary is not the user's turn, even though it is made
		// during one. Tagged per call rather than per run because the same
		// summarizer serves every session, and because P67.6 needs compaction
		// distinguishable from the conversation it is compacting.
		Purpose: provider.PurposeCompaction,
	}
	stream, err := s.adapter.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			b.WriteString(ev.Text)
		case provider.EventError:
			return "", ev.Err
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("summarizer returned empty output")
	}
	if dropped > 0 {
		out += "\n\n" + omissionNote(dropped)
	}
	// P65.2: re-emit the lists as code rather than trusting the model to have
	// copied them. This is the half that has to be *computed*: a model that
	// fumbles "## Key Decisions" still cannot lose a path list it was never
	// asked to reproduce, and it is what makes the set accumulate — the next
	// compaction reads these tags back out of this very message.
	if fileBlock != "" {
		out += "\n\n" + fileBlock
	}
	return out, nil
}

func renderTranscript(msgs []provider.Message) string {
	return renderTranscriptCapped(msgs, 0)
}

// renderTranscriptCapped renders the transcript, optionally capping every
// rendered block at cap runes (cap <= 0 means only the standing tool-result
// limit applies).
func renderTranscriptCapped(msgs []provider.Message, capRunes int) string {
	resultLimit := toolResultRuneLimit
	if capRunes > 0 && capRunes < resultLimit {
		resultLimit = capRunes
	}
	var b strings.Builder
	for _, m := range msgs {
		for _, blk := range m.Content {
			switch v := blk.(type) {
			case provider.TextBlock:
				fmt.Fprintf(&b, "%s: %s\n", m.Role, truncateForSummary(v.Text, capRunes))
			case provider.ToolUseBlock:
				fmt.Fprintf(&b, "%s called tool %s(%s)\n", m.Role, v.Name, truncateForSummary(string(v.Input), capRunes))
			case provider.ToolResultBlock:
				fmt.Fprintf(&b, "tool result: %s\n", truncateForSummary(v.Content, resultLimit))
			}
		}
	}
	return b.String()
}

// truncateForSummary shortens s to roughly limit runes, middle-out: head and
// tail are kept and the middle is replaced by an explicit marker. The marker is
// deliberately visible — a summarizing model must not mistake a truncated block
// for a complete one. limit <= 0 means no truncation.
func truncateForSummary(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	head := limit / 2
	tail := limit - head
	return string(r[:head]) +
		fmt.Sprintf("\n…[truncated by compaction: %d characters elided]…\n", len(r)-limit) +
		string(r[len(r)-tail:])
}
