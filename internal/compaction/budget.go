package compaction

import (
	"context"

	"github.com/fiddler110/aegis/internal/tokenest"
)

// P66.14 — the caller's view of the token budget, per call.
//
// This replaces SetEstimateCorrection, and the reason is the one filecontext.go
// already argues for the file lists: a Summarizer is built once per *server* and
// shared by every session, so anything session- or run-scoped that lands on the
// struct is a cross-session leak rather than merely a stale value. The old
// setter looked exempt because a *calibration scale* is arguably process-wide —
// but it carried an `overhead` alongside it, which is the estimate of the
// **calling run's exposed tool schemas** (ARCH-07). Two sessions with different
// personas, different skills activated, or one of them mid-`tool_search`, wrote
// each other's overhead; and PERF-03's fix, which lets that number move within a
// single run, would have made them do it every turn.
//
// The trigger travels the same way, and closes LLM-02. The engine and this
// package run two gates over the same messages, and until P66.14 they compared
// against *different thresholds*: the engine reserved room for the completion
// (P59.1) while shouldCompact applied a flat 20%-free rule that never saw
// maxTokens, so at a 4,096-token window the engine asked at 2,048 and this
// package refused until 3,277 — summarizing with 819 tokens left for a
// completion the request had asked 32,768 for. Carrying the caller's own trigger
// makes disagreement structurally impossible, and tokenest.CompactionTrigger is
// the fallback for a caller that has none.

// budgetKey is the context key carrying one call's token budget.
type budgetKey struct{}

// budget is the caller-supplied view of what this call is gating against.
type budget struct {
	overhead int
	scale    float64
	trigger  int
}

// WithTokenBudget attaches the caller's estimate correction and compaction
// trigger to ctx for the next Compact call. It is the method half of
// engine.BudgetedCompactor — a method rather than only a package function so the
// engine can discover the capability by type assertion without importing this
// package, which it must not do (engine's own tests import compaction, so the
// dependency would close a cycle).
//
// Every argument degrades safely on its own: overhead <= 0 leaves the raw
// estimate unadjusted for content the transcript does not hold, scale <= 0
// leaves the heuristic uncorrected (this package's pre-P62.4 behaviour), and
// trigger <= 0 means "you decide" — shouldCompact falls back to
// tokenest.CompactionTrigger over the window and MaxTokens this Summarizer was
// configured with. Omitting the decoration entirely is the same as passing all
// three as zero.
func (s *Summarizer) WithTokenBudget(ctx context.Context, overhead int, scale float64, trigger int) context.Context {
	return WithTokenBudget(ctx, overhead, scale, trigger)
}

// WithTokenBudget is the package-level form, for a caller holding no Summarizer.
func WithTokenBudget(ctx context.Context, overhead int, scale float64, trigger int) context.Context {
	if overhead <= 0 && scale <= 0 && trigger <= 0 {
		return ctx
	}
	if overhead < 0 {
		overhead = 0
	}
	if scale < 0 {
		scale = 0
	}
	return context.WithValue(ctx, budgetKey{}, budget{overhead: overhead, scale: scale, trigger: trigger})
}

func budgetFrom(ctx context.Context) budget {
	b, _ := ctx.Value(budgetKey{}).(budget)
	return b
}

// triggerOr reports the estimate at which this call should compact: the caller's
// own threshold when it supplied one, otherwise the shared function over the
// window and completion budget this Summarizer knows about.
//
// A zero window means no window is known and the fixed MaxBudget path applies
// instead; the caller-supplied trigger is still honoured there, because a caller
// that named a threshold knows something this Summarizer does not.
func (b budget) triggerOr(window, maxTokens int) int {
	if b.trigger > 0 {
		return b.trigger
	}
	return tokenest.CompactionTrigger(window, maxTokens)
}

// pruneTriggerOr mirrors triggerOr for the deterministic pre-pass gate, one step
// ahead of the compaction trigger. When the caller supplied a trigger, the lead
// is taken off *its* number, so the two gates keep their relative order whatever
// threshold the caller is running.
func (b budget) pruneTriggerOr(window, maxTokens int) int {
	if b.trigger <= 0 {
		return tokenest.CompactionPruneTrigger(window, maxTokens)
	}
	if p := b.trigger - tokenest.CompactionPruneLead(window); p > 0 {
		return p
	}
	return 0
}
