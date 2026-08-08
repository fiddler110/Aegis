// Package compaction keeps conversations within a token budget by summarizing
// older turns with an auxiliary model call (lineage-style compression, as in
// Hermes). Recent turns are preserved verbatim.
package compaction

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync/atomic"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
)

const (
	// largeContextWindowThreshold is the token count above which a context window
	// is considered "large" and uses an absolute buffer instead of a ratio.
	largeContextWindowThreshold = 200_000
	// largeContextWindowBuffer is the minimum remaining tokens before compaction
	// triggers for large context windows.
	largeContextWindowBuffer = 20_000
	// smallContextWindowRatio is the fraction of the context window that must
	// remain free before compaction triggers for small context windows.
	smallContextWindowRatio = 0.20

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
	summarizeSystemPrompt = "You compress conversation history. Produce a concise but complete summary that preserves: decisions made, facts established, file paths and identifiers, tool results that matter, and any open tasks or unresolved questions. Use terse bullet points."
	summarizePreamble     = "Summarize this conversation so far:\n\n"

	// toolResultRuneLimit is the long-standing per-tool-result cap applied when
	// rendering a transcript, independent of any fit-driven shrinking.
	toolResultRuneLimit = 800

	// prunePrefixCacheBuffer / prunePrefixCacheRatio gate the pre-pass when
	// PreservePrefixCache is set. They deliberately mirror the compaction
	// trigger constants above but one step earlier — an absolute 40k floor for
	// large windows, a 25%-free ratio for small ones, against compaction's 20k
	// and 20% — so the pre-pass still gets its chance to bring the conversation
	// back under budget *before* the LLM summarizer is reached, which is the
	// whole point of running it as a pre-pass. See shouldPrune for why the gate
	// exists at all.
	prunePrefixCacheBuffer = 2 * largeContextWindowBuffer
	prunePrefixCacheRatio  = 0.25
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

	// estimateOverhead and estimateScale carry the engine's learned correction
	// for the shared token estimate (P62.4). Atomic for the same reason
	// contextWindow is: they are updated turn by turn while Compact may be
	// running. estimateScale is a float64 held as its IEEE-754 bits, since
	// sync/atomic has no float type; zero means "never set" and leaves the raw
	// estimate untouched, which is this package's pre-P62.4 behaviour.
	estimateOverhead atomic.Int64
	estimateScale    atomic.Uint64
}

// Options configures a Summarizer.
type Options struct {
	Adapter provider.Adapter
	Model   string
	// ContextWindow is the model's context window in tokens. When > 0 it drives
	// smart compaction thresholds. When 0, MaxBudget is used as a fixed fallback.
	ContextWindow int
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
	// be worth a prefill recompute (see shouldPrune). Off by default: for a
	// per-token-billed cloud provider the pre-pass really is free and the
	// existing unconditional behaviour is correct.
	//
	// Callers set this from config.LocalBackend(provider, base URL). It is a
	// plain bool rather than a config type on purpose — internal/compaction
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
	return s
}

// SetContextWindow updates the context window driving compaction thresholds.
// Safe to call while Compact is running; used when the effective window is
// only learned after construction (e.g. Ollama reports the loaded model's
// real allocation once the first request has loaded it).
func (s *Summarizer) SetContextWindow(tokens int) {
	s.contextWindow.Store(int64(tokens))
}

// ContextWindow reports the window currently driving compaction thresholds (0
// when none is known and the fixed MaxBudget applies instead). It exists so a
// caller that retunes the summarizer can assert which model's window it ended
// up with — the daemon tunes it from the model compaction actually runs on,
// which is not necessarily the global one (P52.1).
func (s *Summarizer) ContextWindow() int {
	return int(s.contextWindow.Load())
}

// SetEstimateCorrection applies the caller's learned correction for this
// package's token estimate: an additive overhead for prompt content the
// transcript does not contain (the tool schemas, which ride the request
// alongside it) and a multiplicative scale for the heuristic's residual error.
// Safe to call while Compact is running.
//
// It implements engine.CalibratedCompactor, and the reason that interface
// exists is worth keeping next to the setter: the engine and this package run
// two separate gates over the same messages, so a correction applied to only
// one of them puts them back into the disagreement P41.1 unified them to end.
// A scale <= 0 clears the correction rather than being stored, so a caller
// cannot accidentally zero out every estimate.
func (s *Summarizer) SetEstimateCorrection(overhead int, scale float64) {
	if overhead < 0 {
		overhead = 0
	}
	s.estimateOverhead.Store(int64(overhead))
	if scale <= 0 {
		s.estimateScale.Store(0)
		return
	}
	s.estimateScale.Store(math.Float64bits(scale))
}

// estimate prices system+msgs the way the backend will: the raw heuristic, plus
// the request overhead, times the learned scale. With no correction set it is
// exactly EstimateTokens, so every caller below reads the same number it always
// did until the engine has evidence to the contrary.
func (s *Summarizer) estimate(system string, msgs []provider.Message) int {
	n := EstimateTokens(system, msgs) + int(s.estimateOverhead.Load())
	bits := s.estimateScale.Load()
	if bits == 0 {
		return n
	}
	scaled := float64(n) * math.Float64frombits(bits)
	out := int(scaled)
	if float64(out) < scaled {
		out++ // round up; see tokenest.Calibrator.Apply
	}
	return out
}

// shouldCompact reports whether the current estimated token count warrants
// compaction given the configured context window or fixed budget.
func (s *Summarizer) shouldCompact(estimated int) bool {
	if win := int(s.contextWindow.Load()); win > 0 {
		remaining := win - estimated
		if win > largeContextWindowThreshold {
			return remaining < largeContextWindowBuffer
		}
		return remaining < int(float64(win)*smallContextWindowRatio)
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
// A Summarizer with no known context window returns true (prune) even under
// preservePrefixCache: with no window there is no headroom to measure, and
// guessing wrong in that direction only costs a prefill, while guessing wrong
// in the other costs an overflow.
func (s *Summarizer) shouldPrune(estimated int) bool {
	if !s.preservePrefixCache {
		return true
	}
	win := int(s.contextWindow.Load())
	if win <= 0 {
		return true
	}
	remaining := win - estimated
	if win > largeContextWindowThreshold {
		return remaining < prunePrefixCacheBuffer
	}
	return remaining < int(float64(win)*prunePrefixCacheRatio)
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
	return s.compact(ctx, system, msgs, false)
}

// ForceCompact runs the same compaction pass unconditionally, skipping the
// budget checks Compact uses to decide whether compaction is warranted — for
// a user-triggered manual compaction (e.g. the TUI `/compact` command) ahead
// of a tool-heavy stretch the user knows is coming, rather than the automatic
// budget-driven path.
func (s *Summarizer) ForceCompact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	return s.compact(ctx, system, msgs, true)
}

func (s *Summarizer) compact(ctx context.Context, system string, msgs []provider.Message, force bool) ([]provider.Message, bool, error) {
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
	// Note the ordering of the || — under the default (cloud) configuration the
	// short-circuit means EstimateTokens is not computed here at all, exactly as
	// before.
	var changedByPrune bool
	if force || !s.preservePrefixCache || s.shouldPrune(s.estimate(system, msgs)) {
		var prunedChars int
		msgs, prunedChars = pruneStaleToolResults(msgs, s.keepRecent)
		changedByPrune = prunedChars > 0
	}

	if !force && !s.shouldCompact(s.estimate(system, msgs)) {
		return msgs, changedByPrune, nil
	}

	boundary := s.boundary(msgs)
	if boundary <= 0 {
		return msgs, changedByPrune, nil // nothing more safe to compact
	}

	prefix := msgs[:boundary]
	summary, err := s.summarize(ctx, prefix)
	if err != nil {
		return msgs, changedByPrune, err
	}

	out := make([]provider.Message, 0, len(msgs)-boundary+1)
	out = append(out, provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "Summary of earlier conversation (older turns were compacted):\n\n" + summary}},
	})
	out = append(out, msgs[boundary:]...)
	return out, true, nil
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
	out := make([]provider.Message, 0, len(msgs)-boundary+1)
	out = append(out, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "Earlier conversation was dropped by deterministic fallback " +
			"compaction (the AI summarizer failed repeatedly, so no AI-generated summary is available):\n\n" + fallbackNote(prefix)}},
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
func summarizeRequestTokens(transcript string) int {
	return tokenest.Messages(summarizeSystemPrompt, []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: summarizePreamble + transcript}}},
	})
}

// fitTranscript renders the prefix into a transcript small enough that the
// summarization request built from it fits the budget from summarizeFitBudget,
// and reports how many of the oldest prefix messages had to be dropped to get
// there (0 in the common case). Shrinking is deterministic and runs in two
// stages: oversized individual blocks are truncated first (middle-out, with a
// visible elision marker), and only if that is still not enough are the oldest
// messages dropped, oldest-first, until it fits.
func (s *Summarizer) fitTranscript(prefix []provider.Message) (transcript string, dropped int, err error) {
	full := renderTranscript(prefix)
	budget := s.summarizeFitBudget()
	if budget == 0 {
		return full, 0, nil // no budget to check against
	}
	fits := func(t string) bool { return summarizeRequestTokens(t) <= budget }
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
	transcript, dropped, err := s.fitTranscript(prefix)
	if err != nil {
		return "", err
	}
	req := provider.Request{
		Model:     s.model,
		MaxTokens: s.summaryTokens,
		System:    summarizeSystemPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: summarizePreamble + transcript}}},
		},
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
