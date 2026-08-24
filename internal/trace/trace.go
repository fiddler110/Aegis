// Package trace defines the per-turn observability record the engine emits
// after each model turn. A TurnTrace captures token usage, estimated cost, the
// tool calls the turn triggered (with durations), and wall time, so a session
// can be audited or replayed. Records are persisted alongside session messages
// and printed by `aegis sessions trace <id>`.
//
// This package is intentionally dependency-light (only the standard library) so
// both the engine (which produces traces) and the session store (which persists
// them) can import it without a cycle.
package trace

import "time"

// ToolCall records a single tool execution within a turn.
type ToolCall struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
	IsError    bool   `json:"is_error,omitempty"`
	// ErrorText is the tool result body when IsError is true, bounded (see
	// engine.boundToolError). Before P68.1 the trace carried only IsError and
	// the SSE stream logged just a character count, so a failing tool call —
	// P62.9's edit_section failure among them — was unreadable after the run
	// ended even though the daemon that produced it survived.
	ErrorText string `json:"error_text,omitempty"`
}

// Compaction records what proactive compaction did at the start of a turn, or
// decided not to do (P66.11/GAP-01).
//
// Every field here was already computed inside the engine's compaction guard and
// discarded one line later, which is what made the closure condition of several
// measurement items — "the turn at which compaction actually fires" — a thing you
// could only get by reading logs. Estimate and Trigger are included even when
// nothing was applied, because "how close was it" is the question a run that
// overflowed anyway raises.
type Compaction struct {
	Applied        bool `json:"applied"`                   // the conversation was rewritten
	Summarized     bool `json:"summarized,omitempty"`      // the LLM summarizer ran (not just the deterministic pre-pass)
	Suppressed     bool `json:"suppressed,omitempty"`      // over the trigger, but the P62.7 minimum-yield rule held the attempt off
	FreedTokens    int  `json:"freed_tokens,omitempty"`    // estimated tokens the pass freed, when the compactor reports yield
	MessagesBefore int  `json:"messages_before,omitempty"` // conversation length before the rewrite
	MessagesAfter  int  `json:"messages_after,omitempty"`  // and after
	Estimate       int  `json:"estimate"`                  // the calibrated prompt estimate the decision was made on
	Trigger        int  `json:"trigger"`                   // the shared compaction trigger it was compared against

	// ColdCleared is how many stale tool results the P67.6 cold-cache pass
	// dropped on this turn, and ColdFreedTokens what that freed. They are
	// separate from Applied/FreedTokens on purpose: that pass fires on cache
	// *temperature* rather than on context pressure, so a turn can clear
	// results while staying nowhere near the trigger — and a trace that folded
	// the two together could not answer "did compaction fire?", which is the
	// question LLM-02's closure condition is written as.
	ColdCleared     int `json:"cold_cleared,omitempty"`
	ColdFreedTokens int `json:"cold_freed_tokens,omitempty"`

	// SummaryText is the text of the summary message a summarizing compaction
	// actually produced (LLM or deterministic-fallback), bounded (see
	// engine.boundSummary). Set only when Summarized is true. Before P68.1 the
	// trace recorded that a compaction happened and how many messages it
	// folded, but never the summary itself — the one thing P65.2 (does a local
	// model fill the fixed skeleton without losing what terse bullets kept)
	// needs to judge.
	SummaryText string `json:"summary_text,omitempty"`
}

// Guard records the output guard's verdict on a turn's final answer
// (P66.11/GAP-01). Absent on any turn the guard did not judge — a tool round, an
// empty answer, or a run with no guard configured — which is itself information:
// a run whose traces carry no Guard at all had no guard, and that used to be
// indistinguishable from one whose guard silently never ran.
type Guard struct {
	// Status is guard.Status: "passed" | "failed" | "skipped_transport_error".
	// Kept alongside Passed rather than derived from it because a fail-open skip
	// also passes, and telling those apart is exactly what FIND-16 was about.
	Status   string `json:"status"`
	Passed   bool   `json:"passed"`
	Retrying bool   `json:"retrying,omitempty"` // a corrective was appended and the run continued
	// Reason is the verdict's explanation, bounded. It can quote the answer it
	// judged, which is why it is bounded — but not redacted: a trace is persisted
	// in the same store as the full transcript it describes, so it is not a new
	// disclosure surface. The export path is where redaction belongs (SEC-08).
	Reason string `json:"reason,omitempty"`
}

// TurnTrace is the structured record of one engine turn: a model call plus any
// tool executions it triggered.
//
// P66.11 widened it (GAP-01). Before that it carried tokens, cost, tool calls and
// wall time — enough to answer "what did this cost" and nothing about *why the
// turn ended the way it did*. The stop reason, the compaction event, the guard
// verdict and the correctives the engine injected were all computed and thrown
// away one line after they were produced, which for a project whose method is
// measurement is the gap most at odds with how it works.
type TurnTrace struct {
	Index int    `json:"index"` // turn index within the run (0-based)
	Model string `json:"model"`
	// RunID identifies the Run this turn belongs to. Traces from several runs
	// accumulate in one session (each request is a Run), so without it a
	// per-turn record cannot be attributed to the request that produced it —
	// which is the first thing anyone reconstructing a bad session needs.
	RunID               string     `json:"run_id,omitempty"`
	InputTokens         int        `json:"input_tokens"`
	OutputTokens        int        `json:"output_tokens"`
	CacheReadTokens     int        `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int        `json:"cache_creation_tokens,omitempty"`
	CostUSD             float64    `json:"cost_usd"`            // per-turn estimated cost (0 if model unpriced or usage estimated)
	Estimated           bool       `json:"estimated,omitempty"` // token counts were estimated (e.g. local models)
	ToolCalls           []ToolCall `json:"tool_calls,omitempty"`
	// StopReason is the provider's own reason for ending the turn
	// (provider.StopReason: end_turn, tool_use, max_tokens, …). A run that burned
	// to its iteration cap on max_tokens continuations looks identical to one that
	// worked without it.
	StopReason string `json:"stop_reason,omitempty"`
	// Compaction and Guard are nil on turns where neither happened.
	Compaction *Compaction `json:"compaction,omitempty"`
	Guard      *Guard      `json:"guard,omitempty"`
	// Correctives names the retry-shaped things the engine injected on this turn,
	// in the order it decided them: "max_tokens_continuation", "shim_format",
	// "zero_tool", "empty_answer", "guard", "loop". This is the retry record —
	// the engine's own corrective machinery is why a long local-model run has
	// turns nobody asked for, and until now the only trace of it was a notice
	// event that nothing persisted.
	Correctives []string `json:"correctives,omitempty"`
	// CalibrationSamples is how many samples the token-estimate correction has
	// accumulated as of this turn (P68.1). Present on every turn a shared
	// context window makes calibration meaningful, unlike Compaction, which is
	// nil on a turn that stayed under the trigger — "is the estimate this run
	// planned against still uncorrected" is a question worth asking on any
	// turn, not only one where compaction fired.
	CalibrationSamples int       `json:"calibration_samples,omitempty"`
	WallMS             int64     `json:"wall_ms"` // wall time for the turn (model call + tools)
	StartedAt          time.Time `json:"started_at"`
}
