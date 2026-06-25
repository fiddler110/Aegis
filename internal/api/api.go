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
}

// EventKind mirrors engine.EventKind on the wire.
type EventKind string

const (
	KindText       EventKind = "text"
	KindToolCall   EventKind = "tool_call"
	KindToolResult EventKind = "tool_result"
	KindTurnDone   EventKind = "turn_done"
	KindDone       EventKind = "done"
	KindError      EventKind = "error"
)

// Event is one server-sent event during a message run.
type Event struct {
	Kind        EventKind       `json:"kind"`
	Text        string          `json:"text,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	ToolInput   json.RawMessage `json:"tool_input,omitempty"`
	ToolResult  string          `json:"tool_result,omitempty"`
	ToolIsError bool            `json:"tool_is_error,omitempty"`
	InputTokens  int             `json:"input_tokens,omitempty"`
	OutputTokens int             `json:"output_tokens,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	Error        string          `json:"error,omitempty"`
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

// ErrorResponse is the body returned for non-2xx responses.
type ErrorResponse struct {
	Error string `json:"error"`
}
