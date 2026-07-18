// Package ollama implements a provider.Adapter for Ollama's native /api/chat
// endpoint (P33.9), as distinct from internal/provider/openai talking to
// Ollama's OpenAI-compatible /v1/chat/completions endpoint. The native API
// unlocks four things the compat endpoint structurally blocks: per-request
// num_ctx, keep_alive control, real token usage (prompt_eval_count/
// eval_count) instead of a byte-count estimate, and load/prompt-eval
// telemetry (load_duration) that lets a caller name a cold model load
// instead of showing it as generic generation time.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/sse"
)

const defaultBaseURL = "http://localhost:11434"

// Adapter talks to an Ollama server's native /api/chat endpoint.
type Adapter struct {
	baseURL   string
	client    *http.Client
	headers   map[string]string
	think     *bool  // nil = omit; false = disable extended thinking
	numCtx    int    // 0 = omit, let Ollama use its own default
	keepAlive string // "" = omit, let Ollama use its own default (5m)
}

// Option configures the adapter.
type Option func(*Adapter)

// WithBaseURL overrides the native API base URL (default
// "http://localhost:11434"). A lingering OpenAI-compat "/v1" suffix from an
// older config is stripped so requests land on /api/chat, not /v1/api/chat.
func WithBaseURL(u string) Option {
	return func(a *Adapter) {
		if u == "" {
			return
		}
		b := strings.TrimRight(u, "/")
		b = strings.TrimSuffix(b, "/v1")
		a.baseURL = strings.TrimRight(b, "/")
	}
}

// WithHeaders adds extra HTTP headers to every request (e.g. a reverse-proxy
// auth header — Ollama itself needs no credential).
func WithHeaders(h map[string]string) Option {
	return func(a *Adapter) { a.headers = h }
}

// WithThink controls the extended-thinking parameter for reasoning models
// (Qwen3, DeepSeek-R1). Pass false to suppress the thinking preamble and get
// plain content-only responses. Nil (the default) omits the parameter.
func WithThink(v *bool) Option {
	return func(a *Adapter) { a.think = v }
}

// WithNumCtx sets the per-request serving context window (options.num_ctx).
// Zero (the default) omits the field, leaving Ollama's own default
// (OLLAMA_CONTEXT_LENGTH or a modelfile-pinned value) in effect.
func WithNumCtx(n int) Option {
	return func(a *Adapter) {
		if n > 0 {
			a.numCtx = n
		}
	}
}

// WithKeepAlive sets how long Ollama keeps the model loaded after this
// request (e.g. "10m", "-1" to pin forever, "0" to unload immediately).
// Empty (the default) omits the field, leaving Ollama's own default (5m) in
// effect. Not yet driven by config — see roadmap P33.10.
func WithKeepAlive(v string) Option {
	return func(a *Adapter) { a.keepAlive = v }
}

// New constructs a native Ollama adapter.
func New(opts ...Option) *Adapter {
	a := &Adapter{baseURL: defaultBaseURL, client: sse.NewStreamingClient()}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name implements provider.Adapter.
func (a *Adapter) Name() string { return "ollama" }

// --- wire types ---

type wireMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	Images    []string       `json:"images,omitempty"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
	// ToolName identifies which tool a role:"tool" message is a result for.
	// Ollama's native API correlates tool results by name, not by an ID —
	// unlike chat-completions' tool_call_id, native tool calls carry no ID at
	// all (see wireToolCall).
	ToolName string `json:"tool_name,omitempty"`
}

type wireToolCall struct {
	Function wireToolCallFunc `json:"function"`
}

type wireToolCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // a JSON object, not a string
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireOptions struct {
	NumCtx      int      `json:"num_ctx,omitempty"`
	NumPredict  int      `json:"num_predict,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

type wireRequest struct {
	Model     string        `json:"model"`
	Messages  []wireMessage `json:"messages"`
	Tools     []wireTool    `json:"tools,omitempty"`
	Stream    bool          `json:"stream"`
	Think     *bool         `json:"think,omitempty"`
	KeepAlive string        `json:"keep_alive,omitempty"`
	Options   *wireOptions  `json:"options,omitempty"`
}

// toolNames maps every ToolUseBlock ID appearing in msgs to its tool name, so
// a later ToolResultBlock (which carries only the ID) can be translated into
// the tool_name field the native API expects instead.
func toolNames(msgs []provider.Message) map[string]string {
	names := make(map[string]string)
	for _, m := range msgs {
		for _, b := range m.Content {
			if tu, ok := b.(provider.ToolUseBlock); ok {
				names[tu.ID] = tu.Name
			}
		}
	}
	return names
}

// translate converts harness messages to native chat-message wire format.
func translate(system string, msgs []provider.Message) []wireMessage {
	names := toolNames(msgs)
	out := make([]wireMessage, 0, len(msgs)+1)
	if system != "" {
		out = append(out, wireMessage{Role: "system", Content: system})
	}
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleAssistant:
			wm := wireMessage{Role: "assistant"}
			var text string
			for _, b := range m.Content {
				switch v := b.(type) {
				case provider.TextBlock:
					text += v.Text
				case provider.ToolUseBlock:
					args := v.Input
					if len(bytes.TrimSpace(args)) == 0 {
						args = json.RawMessage("{}")
					}
					wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
						Function: wireToolCallFunc{Name: v.Name, Arguments: args},
					})
				}
			}
			wm.Content = text
			if wm.Content == "" && len(wm.ToolCalls) == 0 {
				continue // skip empty assistant turns (e.g. model returned nothing)
			}
			out = append(out, wm)
		case provider.RoleUser:
			var text string
			var images []string
			for _, b := range m.Content {
				switch v := b.(type) {
				case provider.TextBlock:
					text += v.Text
				case provider.ImageBlock:
					images = append(images, v.Data)
				case provider.ToolResultBlock:
					out = append(out, wireMessage{Role: "tool", Content: v.Content, ToolName: names[v.ToolUseID]})
				}
			}
			if text != "" || len(images) > 0 {
				out = append(out, wireMessage{Role: "user", Content: text, Images: images})
			}
		}
	}
	return out
}

func translateTools(tools []provider.ToolSchema) []wireTool {
	out := make([]wireTool, 0, len(tools))
	for _, t := range tools {
		var wt wireTool
		wt.Type = "function"
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		wt.Function.Parameters = t.InputSchema
		out = append(out, wt)
	}
	return out
}

// Stream implements provider.Adapter.
func (a *Adapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	wr := wireRequest{
		Model:     req.Model,
		Messages:  translate(req.System, req.Messages),
		Tools:     translateTools(req.Tools),
		Stream:    true,
		Think:     a.think,
		KeepAlive: a.keepAlive,
	}
	var opts wireOptions
	var hasOpts bool
	if a.numCtx > 0 {
		opts.NumCtx = a.numCtx
		hasOpts = true
	}
	if req.MaxTokens > 0 {
		opts.NumPredict = req.MaxTokens
		hasOpts = true
	}
	if req.Temperature != nil {
		opts.Temperature = req.Temperature
		hasOpts = true
	}
	if hasOpts {
		wr.Options = &opts
	}

	body, err := json.Marshal(wr)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range a.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, provider.NewTransportError(a.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, sse.HandleErrorResponse(a.Name(), resp)
	}

	out := make(chan provider.Event)
	go consume(ctx, resp.Body, out)
	return out, nil
}

// errorMessage extracts a human-readable message from a chunk's "error"
// field. Ollama's native API spells it as a bare string
// ({"error":"model runner has unexpectedly stopped"}); the object spelling
// ({"error":{"message":"..."}}) is tolerated too in case a future version or
// a proxy in front of Ollama changes shape. Anything else — absent, null, or
// neither — returns "" so the caller treats the chunk as ordinary.
func errorMessage(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	if msg := strings.TrimSpace(obj.Message); msg != "" {
		return msg
	}
	return trimmed
}

// wireChunk is one line of the newline-delimited JSON stream /api/chat
// returns — not SSE, so there is no "data:" framing to strip.
type wireChunk struct {
	Message struct {
		Content   string `json:"content"`
		Thinking  string `json:"thinking"`
		ToolCalls []struct {
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Done            bool            `json:"done"`
	DoneReason      string          `json:"done_reason"`
	PromptEvalCount int             `json:"prompt_eval_count"`
	EvalCount       int             `json:"eval_count"`
	LoadDuration    int64           `json:"load_duration"` // nanoseconds
	Error           json.RawMessage `json:"error"`
}

func consume(ctx context.Context, body io.ReadCloser, out chan<- provider.Event) {
	defer close(out)
	defer body.Close()

	emit := sse.NewEmitter(ctx, out).Emit

	usage := &provider.Usage{}
	stop := provider.StopEndTurn
	toolIndex := 0
	sawLength := false // a chunk reported done_reason "length" (context ceiling hit)

	scanner := sse.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk wireChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.DoneReason == "length" {
			sawLength = true
		}
		if msg := errorMessage(chunk.Error); msg != "" {
			// P35.2: a generation cut off at the context ceiling mid-tool-call
			// surfaces here as the server's opaque "invalid tool call arguments
			// ... unexpected end of JSON input" — the native adapter never parses
			// tool arguments itself (it passes them through as RawMessage), so
			// that message is entirely server-side and the only truncation signal
			// available is a done_reason "length" on this/a prior chunk or the
			// truncated-tool-call shape of the message. Detect it before the
			// generic path so the discoverable fix (raise context_window) is
			// named instead of the opaque parse error.
			if sawLength || provider.IsTruncatedToolCallError(msg) {
				emit(provider.Event{Type: provider.EventError, Err: provider.NewContextTruncationError("ollama", msg)})
				return
			}
			// P33.16: classify the {"error":...} envelope so a transient failure
			// (worker crash, model-load failure, OOM) carries a retryable verdict
			// while a deterministic one (context overflow, invalid request) stays
			// terminal — retrying the latter only burns another prompt-eval.
			emit(provider.Event{Type: provider.EventError, Err: provider.NewStreamError("ollama", msg)})
			return
		}

		if chunk.Message.Thinking != "" {
			if !emit(provider.Event{Type: provider.EventThinkingDelta, Text: chunk.Message.Thinking}) {
				return
			}
		}
		if chunk.Message.Content != "" {
			if !emit(provider.Event{Type: provider.EventTextDelta, Text: chunk.Message.Content}) {
				return
			}
		}
		for _, tc := range chunk.Message.ToolCalls {
			args := tc.Function.Arguments
			if len(bytes.TrimSpace(args)) == 0 {
				args = json.RawMessage("{}")
			}
			id := fmt.Sprintf("tu_%d", toolIndex)
			toolIndex++
			// Native tool calls arrive whole (no incremental-argument
			// streaming), so the start and fully-assembled events fire back
			// to back — still worth emitting both so a consumer keyed on
			// EventToolUseStart (P33.3's provisional tool card) behaves
			// identically to the openai-adapter path.
			if !emit(provider.Event{Type: provider.EventToolUseStart, ToolUse: &provider.ToolUseBlock{
				ID: id, Name: tc.Function.Name,
			}}) {
				return
			}
			stop = provider.StopToolUse
			if !emit(provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: id, Name: tc.Function.Name, Input: args,
			}}) {
				return
			}
		}

		if chunk.Done {
			usage.InputTokens = chunk.PromptEvalCount
			usage.OutputTokens = chunk.EvalCount
			usage.LoadDurationMS = chunk.LoadDuration / 1e6
			if stop != provider.StopToolUse && chunk.DoneReason == "length" {
				stop = provider.StopMaxTokens
			}
		}
	}
	if err := scanner.Err(); err != nil {
		emit(provider.Event{Type: provider.EventError, Err: fmt.Errorf("ollama: read stream: %w", err)})
		return
	}

	emit(provider.Event{Type: provider.EventDone, Stop: stop, Usage: usage})
}
