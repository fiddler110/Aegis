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
	InputTokens int             `json:"input_tokens,omitempty"`
	OutputTokens int            `json:"output_tokens,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// ErrorResponse is the body returned for non-2xx responses.
type ErrorResponse struct {
	Error string `json:"error"`
}
