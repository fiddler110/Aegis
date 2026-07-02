// Package provider defines the normalized model interface and the message
// types shared across provider adapters.
//
// The data model mirrors the Anthropic Messages shape (system prompt +
// alternating user/assistant messages, each a list of typed content blocks)
// because it maps cleanly onto tool use and is straightforward to translate
// to chat-completions-style providers in later adapters.
package provider

import (
	"context"
	"encoding/json"
)

// Role identifies the author of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn in a conversation.
type Message struct {
	Role    Role    `json:"role"`
	Content []Block `json:"content"`
}

// Block is a typed unit of message content (text, a tool call, or a tool result).
type Block interface{ blockType() string }

// TextBlock is plain text content.
type TextBlock struct {
	Text string `json:"text"`
}

func (TextBlock) blockType() string { return "text" }

// ImageBlock is image content attached to a user message, carried as
// base64-encoded bytes with their media type (e.g. "image/png"). Vision-capable
// providers map it to their native image content part; text-only providers
// reject it. Data is never logged.
type ImageBlock struct {
	MediaType string `json:"media_type"` // image/png, image/jpeg, image/gif, image/webp
	Data      string `json:"data"`       // base64-encoded image bytes (no data: prefix)
}

func (ImageBlock) blockType() string { return "image" }

// ToolUseBlock is an assistant request to invoke a tool.
type ToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (ToolUseBlock) blockType() string { return "tool_use" }

// ToolResultBlock carries the output of a tool back to the model. It is
// attached to a user-role message.
type ToolResultBlock struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

func (ToolResultBlock) blockType() string { return "tool_result" }

// ThinkingBlock is the model's extended reasoning. When a provider returns
// thinking (Anthropic extended thinking), the block — including its Signature —
// must be preserved in conversation history and replayed on the next request,
// or the provider rejects subsequent tool use.
type ThinkingBlock struct {
	Text      string `json:"text"`
	Signature string `json:"signature"`
}

func (ThinkingBlock) blockType() string { return "thinking" }

// ToolSchema describes a tool exposed to the model.
type ToolSchema struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"` // P3.6: typed output schema
}

// ThinkingConfig requests extended thinking with a token budget for the model's
// internal reasoning. Providers that don't support it ignore the field.
type ThinkingConfig struct {
	BudgetTokens int
}

// Request is a normalized model request.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSchema
	MaxTokens   int
	Temperature *float64
	Thinking    *ThinkingConfig // nil = disabled
}

// StopReason explains why the model stopped generating.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopOther     StopReason = "other"
)

// Usage reports token consumption for a request. The cache fields are populated
// by providers that support prompt caching (e.g. Anthropic) and are zero
// otherwise. CacheReadTokens are billed at a steep discount; CacheCreationTokens
// at a small premium over InputTokens.
//
// When IsEstimated is true the counts were derived from a character-count
// heuristic because the provider returned zero usage (common with local models).
// Cost calculations must be skipped for estimated usage.
type Usage struct {
	InputTokens         int  `json:"input_tokens"`
	OutputTokens        int  `json:"output_tokens"`
	CacheCreationTokens int  `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int  `json:"cache_read_tokens,omitempty"`
	IsEstimated         bool `json:"is_estimated,omitempty"`
}

// EventType enumerates streaming events emitted by an adapter.
type EventType string

const (
	// EventTextDelta carries an incremental chunk of assistant text.
	EventTextDelta EventType = "text_delta"
	// EventThinkingDelta carries an incremental chunk of extended-thinking text.
	EventThinkingDelta EventType = "thinking_delta"
	// EventThinking carries one fully-assembled thinking block (with signature).
	EventThinking EventType = "thinking"
	// EventToolUse carries one fully-assembled tool-use block.
	EventToolUse EventType = "tool_use"
	// EventDone is the final event: the stream completed successfully.
	EventDone EventType = "done"
	// EventError indicates the stream failed; Err is set.
	EventError EventType = "error"
)

// Event is a single item in an adapter's response stream.
type Event struct {
	Type     EventType
	Text     string         // set for EventTextDelta / EventThinkingDelta
	ToolUse  *ToolUseBlock  // set for EventToolUse
	Thinking *ThinkingBlock // set for EventThinking
	Stop     StopReason     // set for EventDone
	Usage    *Usage         // set for EventDone (best effort)
	Err      error          // set for EventError
}

// Adapter is implemented by each provider backend.
type Adapter interface {
	// Name returns the adapter identifier (e.g. "anthropic").
	Name() string
	// Stream issues a request and returns a channel of events. The channel is
	// closed after a terminal EventDone or EventError. Cancelling ctx aborts
	// the stream.
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}
