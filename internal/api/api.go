// Package api defines the wire types shared by the harness daemon and its
// clients (the TUI and CLI).
package api

import (
	"encoding/json"
	"time"
)

// CreateSessionRequest creates a new session.
type CreateSessionRequest struct {
	Title   string `json:"title"`
	System  string `json:"system"`
	Mode    string `json:"mode"`
	Persona string `json:"persona"` // named persona; sets the system prompt when System is empty
}

// SessionMeta describes a session without its messages.
type SessionMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PostMessageRequest sends a user turn into a session.
type PostMessageRequest struct {
	Text string `json:"text"`
	// Images attaches images to the turn (vision-capable models only).
	Images []ImageInput `json:"images,omitempty"`
}

// ImageInput attaches an image to a user turn. Provide either a Path (the daemon
// reads and base64-encodes the file, detecting its media type) or inline base64
// Data with an explicit MediaType. Path is convenient for the local TUI/CLI;
// Data is for remote clients.
type ImageInput struct {
	Path      string `json:"path,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// EventKind mirrors engine.EventKind on the wire.
type EventKind string

const (
	KindText            EventKind = "text"
	KindThinking        EventKind = "thinking"
	KindToolCall        EventKind = "tool_call"
	KindToolResult      EventKind = "tool_result"
	KindTurnDone        EventKind = "turn_done"
	KindDone            EventKind = "done"
	KindError           EventKind = "error"
	KindApprovalRequest EventKind = "approval_request" // engine awaiting user approval
	KindSteer           EventKind = "steer"            // mid-run steering instruction injected
)

// Event is one server-sent event during a message run.
type Event struct {
	Kind         EventKind       `json:"kind"`
	Text         string          `json:"text,omitempty"`
	Tool         string          `json:"tool,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolResult   string          `json:"tool_result,omitempty"`
	ToolIsError  bool            `json:"tool_is_error,omitempty"`
	InputTokens  int             `json:"input_tokens,omitempty"`
	OutputTokens int             `json:"output_tokens,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	Error        string          `json:"error,omitempty"`
	// Cache token usage (Anthropic prompt caching), surfaced for observability.
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	// KindApprovalRequest fields
	ApprovalReason string `json:"approval_reason,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"` // run id to echo back when answering
}

// ApproveRequest is posted to /sessions/{id}/approve to answer a pending
// approval request. Approved true lets the tool run; false denies it. ID must
// match the approval_id from the KindApprovalRequest event.
type ApproveRequest struct {
	Approved bool   `json:"approved"`
	ID       string `json:"id,omitempty"`
}

// Teammate describes a sub-agent tracked by the swarm registry.
type Teammate struct {
	AgentID   string    `json:"agent_id"`
	Name      string    `json:"name"`
	Team      string    `json:"team"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
}

// UpdateSessionRequest patches a session's system prompt and/or mode.
type UpdateSessionRequest struct {
	System *string `json:"system,omitempty"`
	Mode   *string `json:"mode,omitempty"`
}

// MemoryResponse describes the current memory and skills state.
type MemoryResponse struct {
	ProjectMemory string   `json:"project_memory"`
	UserMemory    string   `json:"user_memory"`
	Skills        []string `json:"skills"`
}

// AppendMemoryRequest adds a memory entry.
type AppendMemoryRequest struct {
	Entry string `json:"entry"`
	Scope string `json:"scope"` // "project" (default) or "user"
}

// PersonaInfo describes an available persona.
type PersonaInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CommandInfo describes a custom slash command.
type CommandInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Args        []string `json:"args"`
}

// CheckpointInfo describes a rewind point captured at the start of a turn.
type CheckpointInfo struct {
	ID        string    `json:"id"`
	Seq       int       `json:"seq"`        // conversation message count at capture
	Label     string    `json:"label"`      // the user prompt that began the turn
	FileCount int       `json:"file_count"` // number of files snapshotted in the turn
	CreatedAt time.Time `json:"created_at"`
}

// RewindRequest restores a session to a checkpoint. Scope selects what to
// restore: "code" (files only), "conversation" (messages only), or "both"
// (default).
type RewindRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	Scope        string `json:"scope,omitempty"`
}

// RewindResponse reports the result of a rewind.
type RewindResponse struct {
	Scope         string `json:"scope"`
	FilesRestored int    `json:"files_restored"`
	MessagesKept  int    `json:"messages_kept"`
}

// RunInfo describes an in-flight message run, surfaced so concurrent parallel
// sessions are observable.
type RunInfo struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
	Tools     int       `json:"tools"`     // tool calls so far this run
	LastKind  string    `json:"last_kind"` // most recent event kind
}

// SteerRequest injects a mid-run instruction into an active session run.
type SteerRequest struct {
	Text string `json:"text"`
}

// ErrorResponse is the body returned for non-2xx responses.
type ErrorResponse struct {
	Error string `json:"error"`
}
