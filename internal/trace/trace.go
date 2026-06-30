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
}

// TurnTrace is the structured record of one engine turn: a model call plus any
// tool executions it triggered.
type TurnTrace struct {
	Index               int        `json:"index"` // turn index within the run (0-based)
	Model               string     `json:"model"`
	InputTokens         int        `json:"input_tokens"`
	OutputTokens        int        `json:"output_tokens"`
	CacheReadTokens     int        `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int        `json:"cache_creation_tokens,omitempty"`
	CostUSD             float64    `json:"cost_usd"`            // per-turn estimated cost (0 if model unpriced or usage estimated)
	Estimated           bool       `json:"estimated,omitempty"` // token counts were estimated (e.g. local models)
	ToolCalls           []ToolCall `json:"tool_calls,omitempty"`
	WallMS              int64      `json:"wall_ms"` // wall time for the turn (model call + tools)
	StartedAt           time.Time  `json:"started_at"`
}
