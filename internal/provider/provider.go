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
	// NumCtx is the serving context window (Ollama's options.num_ctx) this
	// request asks the backend to allocate. It belongs on the request rather
	// than on the adapter because the *model* is per-request (P52.4): one
	// daemon-wide adapter serves the primary model, a persona-pinned model and
	// the small model task routing picks, and asking Ollama to serve a small
	// model with the primary model's KV allocation either wastes VRAM or
	// evicts the primary model between turns. Zero means "the adapter's own
	// configured window", which is what every caller that doesn't know or care
	// leaves it at — the engine never sets it directly (it has no business
	// knowing about num_ctx); the daemon supplies it per run via
	// provider.WithNumCtx, and the CLI drive leaves it unset entirely. Adapters
	// whose window is fixed by the remote model — the cloud providers — ignore it.
	NumCtx int
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
	// InputTokens is the number of prompt tokens the provider reports for this
	// request. On the native-Ollama path this is prompt_eval_count, which on
	// current Ollama (0.30.10, live-verified P35.13) is the FULL prompt/context
	// size every turn — NOT a cache-hit delta, even when KV-cache residency
	// (P35.4) makes the turn a prefix-cache hit (there, prompt_eval_duration
	// collapses but the count stays full). Older Ollama versions may have
	// reported deltas, so this is version-dependent: consumers that need a
	// reliable context size across backends (e.g. compaction) must use an
	// estimate — see engine.Conversation.estimatedTokens. To detect a cache
	// hit vs. a full reprocess, read PromptEvalDurationMS, not this count.
	InputTokens         int  `json:"input_tokens"`
	OutputTokens        int  `json:"output_tokens"`
	CacheCreationTokens int  `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int  `json:"cache_read_tokens,omitempty"`
	IsEstimated         bool `json:"is_estimated,omitempty"`
	// LoadDurationMS is how long the provider spent loading the model into
	// memory before inference began (Ollama's native `load_duration`,
	// nanoseconds on the wire, converted to milliseconds here). Zero when not
	// reported (every non-Ollama provider) or when the model was already
	// warm. Lets a caller distinguish a slow cold load from generation time.
	LoadDurationMS int64 `json:"load_duration_ms,omitempty"`
	// PromptEvalDurationMS is how long the provider spent on prefill —
	// processing the prompt tokens before the first generated token (Ollama's
	// native `prompt_eval_duration`, nanoseconds on the wire, converted to
	// milliseconds here). Zero when not reported (every non-Ollama
	// provider). This duration is the KV-cache-hit signal: on current Ollama
	// it collapses on a prefix-cache hit (e.g. 15s->0.1s) while InputTokens
	// stays at the full context size, so duration — not the token count —
	// distinguishes a cache hit from a full reprocess (P35.7, P35.13).
	PromptEvalDurationMS int64 `json:"prompt_eval_duration_ms,omitempty"`
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
	// EventToolUseStart announces a tool call the moment the stream names it,
	// while its arguments are still being generated — often the longest phase
	// of an agentic turn on a local model (P33.3). ToolUse carries Name and,
	// when the provider has assigned one by then, ID; Input is always empty.
	// The fully-assembled EventToolUse for the same call still follows, so a
	// consumer that ignores this event behaves exactly as before.
	EventToolUseStart EventType = "tool_use_start"
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
	ToolUse  *ToolUseBlock  // set for EventToolUseStart / EventToolUse
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

// ContextWindowRaiser is an optional Adapter capability: raise the per-request
// serving context window (Ollama's num_ctx) at runtime. The native Ollama
// adapter implements it so a driven build can escalate the window toward the
// model's max on a context overflow (P47.5b) rather than aborting. Adapters
// whose window is fixed by the remote model (the cloud providers) do not
// implement it. Reach it through RaiseContextWindow, which unwraps the retry /
// failover decorators.
type ContextWindowRaiser interface {
	RaiseContextWindow(n int) bool
}

// RaiseContextWindow raises a's serving context window to n when a — or a base
// adapter it wraps — supports it and n exceeds the current value, returning true
// only when the window actually grew. The retry and failover decorators are
// unwrapped (via Unwrap() Adapter) to reach the base adapter, so a wrapped
// Ollama adapter still escalates. Returns false for any adapter that can't
// raise its window, so the caller falls back to its non-escalating path.
func RaiseContextWindow(a Adapter, n int) bool {
	for a != nil {
		if r, ok := a.(ContextWindowRaiser); ok {
			return r.RaiseContextWindow(n)
		}
		u, ok := a.(interface{ Unwrap() Adapter })
		if !ok {
			return false
		}
		a = u.Unwrap()
	}
	return false
}

// HealthChecker is an optional Adapter capability: a cheap liveness probe
// against the backing model server, independent of a full Stream request. The
// native Ollama adapter implements it (a GET /api/version) so a driven build
// can wait for a crashed/restarting local server to come back before resuming a
// phase from disk (P50.1) instead of aborting the whole run. Adapters whose
// backend is a managed remote service (the cloud providers) need not implement
// it — a transient outage there is already covered by the retry decorator.
// Reach it through CheckBackendHealth, which unwraps the retry / failover
// decorators.
type HealthChecker interface {
	// Healthy reports whether the backend answered a liveness probe within the
	// deadline carried by ctx. It must be side-effect-free and must not load or
	// unload a model — it only answers "is the server reachable right now?".
	Healthy(ctx context.Context) bool
}

// CheckBackendHealth probes a's backend liveness when a — or a base adapter it
// wraps — supports it, unwrapping the retry / failover decorators to reach the
// base adapter exactly as RaiseContextWindow does. The bool result is the
// probe's verdict; supported reports whether any adapter in the chain could
// answer at all. A caller uses supported==false to mean "this backend has no
// liveness probe — don't wait on it" (e.g. a cloud adapter), distinct from
// supported==true, healthy==false ("the local server is down right now").
func CheckBackendHealth(ctx context.Context, a Adapter) (healthy, supported bool) {
	for a != nil {
		if h, ok := a.(HealthChecker); ok {
			return h.Healthy(ctx), true
		}
		u, ok := a.(interface{ Unwrap() Adapter })
		if !ok {
			return false, false
		}
		a = u.Unwrap()
	}
	return false, false
}
