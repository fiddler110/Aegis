// Package anthropic implements a provider.Adapter for the Anthropic Messages
// API. It hand-rolls the HTTP + SSE handling so the harness fully owns the
// normalization between the wire format and the provider package's types.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/sse"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"

	// providerName is the name every error this adapter constructs is stamped
	// with. The decoder builds errors without a reference to the Adapter, so the
	// name lives here rather than being read back off Name().
	providerName = "anthropic"
)

// Adapter talks to the Anthropic Messages API.
type Adapter struct {
	apiKey   string
	baseURL  string
	client   *http.Client
	cache    bool // emit prompt-cache breakpoints
	headers  map[string]string
	thinking *provider.ThinkingConfig // non-nil enables extended thinking

	// headerTimeout remembers an explicitly configured response-header timeout
	// (P61.5) so a WithHTTPClient applied *after* WithResponseHeaderTimeout can
	// re-apply it to the client it installs. 0 means the option was never used;
	// WithResponseHeaderTimeout normalizes its own "<= 0 means default" case
	// before storing, so a stored value is always a real duration.
	headerTimeout time.Duration

	// streamIdleTimeout (P59.2/P61.1) bounds the gap *between* streamed chunks.
	// Same field, semantics and default as the native Ollama adapter's — see
	// sse.DefaultStreamIdleTimeout for why that window is otherwise unwatched
	// once headers arrive. Unlike headerTimeout above it is not client state:
	// sse.Run reads it per stream, so no option can clobber it and it needs no
	// re-application dance. 0 selects sse.DefaultStreamIdleTimeout; negative
	// disables the bound entirely.
	streamIdleTimeout time.Duration
}

// Option configures the adapter.
type Option func(*Adapter)

// WithPromptCaching toggles emission of cache_control breakpoints (default on).
func WithPromptCaching(enabled bool) Option {
	return func(a *Adapter) { a.cache = enabled }
}

// WithBaseURL overrides the API base URL.
func WithBaseURL(u string) Option {
	return func(a *Adapter) {
		if u != "" {
			a.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithHTTPClient overrides the HTTP client. A response-header timeout already
// set by WithResponseHeaderTimeout is re-applied onto the supplied client, so
// the two options compose in either order (P61.5).
func WithHTTPClient(c *http.Client) Option {
	return func(a *Adapter) {
		a.client = c
		if a.headerTimeout > 0 {
			a.client = sse.ApplyResponseHeaderTimeout(a.client, a.headerTimeout)
		}
	}
}

// WithResponseHeaderTimeout overrides how long the streamed request waits for
// response headers (provider.response_header_timeout, P35.5). <= 0 falls back
// to sse.DefaultResponseHeaderTimeout.
//
// It composes onto whatever client the adapter already has rather than
// replacing it (P61.5). This one used to be order-dependent in practice, not
// just in theory: it and WithHTTPClient both assigned a.client, so whichever
// came second won and the other was silently discarded. The remembered
// headerTimeout is what lets a later WithHTTPClient re-apply it.
func WithResponseHeaderTimeout(d time.Duration) Option {
	return func(a *Adapter) {
		if d <= 0 {
			d = sse.DefaultResponseHeaderTimeout
		}
		a.headerTimeout = d
		a.client = sse.ApplyResponseHeaderTimeout(a.client, d)
	}
}

// WithStreamIdleTimeout overrides how long the adapter waits between two
// streamed chunks before giving up on the response
// (provider.stream_idle_timeout, P59.2; wired to this adapter by P61.1). 0
// selects sse.DefaultStreamIdleTimeout; pass a negative duration to disable the
// bound. Deliberately identical to ollama.WithStreamIdleTimeout: the config key
// is one key, so it must mean one thing on every backend.
//
// It stores a plain field rather than touching a.client, so it does not join
// the WithHTTPClient/WithResponseHeaderTimeout ordering problem P61.5 fixed and
// needs no remembered-value counterpart of its own.
func WithStreamIdleTimeout(d time.Duration) Option {
	return func(a *Adapter) { a.streamIdleTimeout = d }
}

// resolveStreamIdleTimeout maps the configured value onto the bound sse.Run
// applies: 0 (unset) takes the default, negative disables.
func (a *Adapter) resolveStreamIdleTimeout() time.Duration {
	if a.streamIdleTimeout == 0 {
		return sse.DefaultStreamIdleTimeout
	}
	if a.streamIdleTimeout < 0 {
		return 0
	}
	return a.streamIdleTimeout
}

// WithHeaders adds extra HTTP headers to every request (e.g. gateway auth).
func WithHeaders(h map[string]string) Option {
	return func(a *Adapter) { a.headers = h }
}

// WithThinking enables extended thinking with the given token budget. A budget
// <1024 disables it (the API minimum).
func WithThinking(budgetTokens int) Option {
	return func(a *Adapter) {
		if budgetTokens >= 1024 {
			a.thinking = &provider.ThinkingConfig{BudgetTokens: budgetTokens}
		}
	}
}

// New constructs an Anthropic adapter.
func New(apiKey string, opts ...Option) *Adapter {
	a := &Adapter{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  sse.NewStreamingClient(0),
		cache:   true,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name implements provider.Adapter.
func (a *Adapter) Name() string { return "anthropic" }

// Compile-time proof that the adapter satisfies the optional capability the
// drive's recovery is gated on (P50.1/P61.6, mirrored here by P78.7). Without
// it, wait-and-resume is inert on this adapter: a transient outage (rate
// limit, 529 overloaded, a transient 5xx) is correctly classified
// (provider.IsBackendUnavailableError) but drive.recoverBackendDown still
// returns backendNotDown because nothing can answer "is it back yet?", so the
// drive hard-aborts instead of waiting and resuming from disk — the same
// failure openai.go's Healthy() fixed for the OpenAI-compat path, applying to
// a cloud backend's transient failure modes instead of a locally-restartable
// server's.
var _ provider.HealthChecker = (*Adapter)(nil)

// Healthy implements provider.HealthChecker: a cheap GET <base>/v1/models
// against the Anthropic API, used by the phased drive (P50.1) to wait for a
// transient outage to clear before resuming a phase from disk.
//
// /v1/models is side-effect-free (it lists what is available, starts no
// generation) and, like openai.go's /models probe, requires no request body —
// unlike Stream, which needs a real prompt to send. It still needs the
// x-api-key/anthropic-version headers Stream sends, since an unauthenticated
// request to a real endpoint answers a different question than the one being
// asked here.
//
// What counts as healthy mirrors openai.go's reasoning exactly: this is a
// liveness question, not a usability one — recoverBackendDown already knows
// the request failed, all it needs is whether there is a server on the other
// end again. A 401 (bad/missing key) or 404 still proves an HTTP server
// answered, which is the honest signal for "reachable"; treating those as
// unhealthy would make the drive wait out its full recovery budget against a
// server that was up the whole time. The carve-out is the gateway 5xx trio
// (502/504 bad gateway/timeout, 503 unavailable — the last also being
// Anthropic's own "overloaded" status), where the responder is explicitly
// saying the service is not there.
//
// It runs on the adapter's own transport (so proxy/TLS configuration
// applies) under a short timeout of its own (provider.HealthClient), so a
// probe can't block on the long prefill budget the streaming client allows.
func (a *Adapter) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
	resp, err := provider.HealthClient(a.client).Do(req)
	if err != nil {
		return false
	}
	provider.DrainAndClose(resp)
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return false
	}
	return true
}

// --- wire types ---

type wireRequest struct {
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens"`
	System      []wireSystemBlock `json:"system,omitempty"`
	Messages    []wireMessage     `json:"messages"`
	Tools       []wireTool        `json:"tools,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	Thinking    *wireThinking     `json:"thinking,omitempty"`
	Stream      bool              `json:"stream"`
}

type wireThinking struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens"`
}

// cacheControl marks a content block as a prompt-cache breakpoint. Anthropic
// caches the longest matching prefix ending at a breakpoint.
type cacheControl struct {
	Type string `json:"type"` // always "ephemeral"
}

var ephemeral = &cacheControl{Type: "ephemeral"}

type wireSystemBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type wireTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type wireMessage struct {
	Role    string      `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// image
	Source *wireImageSource `json:"source,omitempty"`
	// caching
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// wireImageSource is the Anthropic base64 image source descriptor.
type wireImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // image/png, image/jpeg, …
	Data      string `json:"data"`       // base64-encoded bytes
}

func toWireMessages(msgs []provider.Message) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		wm := wireMessage{Role: string(m.Role)}
		for _, b := range m.Content {
			switch v := b.(type) {
			case provider.ThinkingBlock:
				// Replayed verbatim with its signature so the API can validate
				// tool use that followed the model's reasoning.
				wm.Content = append(wm.Content, wireBlock{Type: "thinking", Thinking: v.Text, Signature: v.Signature})
			case provider.TextBlock:
				wm.Content = append(wm.Content, wireBlock{Type: "text", Text: v.Text})
			case provider.ToolUseBlock:
				input := v.Input
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				wm.Content = append(wm.Content, wireBlock{Type: "tool_use", ID: v.ID, Name: v.Name, Input: input})
			case provider.ToolResultBlock:
				wm.Content = append(wm.Content, wireBlock{Type: "tool_result", ToolUseID: v.ToolUseID, Content: v.Content, IsError: v.IsError})
			case provider.ImageBlock:
				wm.Content = append(wm.Content, wireBlock{Type: "image", Source: &wireImageSource{
					Type: "base64", MediaType: v.MediaType, Data: v.Data,
				}})
			default:
				return nil, fmt.Errorf("anthropic: unsupported block type %T", b)
			}
		}
		out = append(out, wm)
	}
	return out, nil
}

// buildSystem wraps the system prompt as a content-block array so a cache
// breakpoint can be attached to it.
func buildSystem(system string, cache bool) []wireSystemBlock {
	if system == "" {
		return nil
	}
	b := wireSystemBlock{Type: "text", Text: system}
	if cache {
		b.CacheControl = ephemeral
	}
	return []wireSystemBlock{b}
}

// buildTools converts tool schemas, placing a cache breakpoint on the final
// tool so the whole (stable) tool list is cached as one prefix segment.
func buildTools(tools []provider.ToolSchema, cache bool) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, len(tools))
	for i, t := range tools {
		out[i] = wireTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
	}
	if cache {
		out[len(out)-1].CacheControl = ephemeral
	}
	return out
}

// cacheLastMessage marks the final block of the conversation as a breakpoint so
// the growing message prefix is cached and reused on the next turn.
func cacheLastMessage(msgs []wireMessage) {
	if len(msgs) == 0 {
		return
	}
	last := &msgs[len(msgs)-1]
	if len(last.Content) == 0 {
		return
	}
	last.Content[len(last.Content)-1].CacheControl = ephemeral
}

// Stream implements provider.Adapter.
func (a *Adapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	if a.apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY)")
	}

	wmsgs, err := toWireMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	cache := a.cache && !req.SuppressCache
	if cache {
		cacheLastMessage(wmsgs)
	}

	// Extended thinking: budget_tokens must be < max_tokens, and temperature
	// must be omitted (the API only permits the default while thinking).
	var thinking *wireThinking
	temperature := req.Temperature
	if a.thinking != nil {
		budget := a.thinking.BudgetTokens
		if budget >= req.MaxTokens {
			budget = req.MaxTokens - 1
		}
		if budget >= 1024 {
			thinking = &wireThinking{Type: "enabled", BudgetTokens: budget}
			temperature = nil
		}
	}

	body, err := json.Marshal(wireRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		System:      buildSystem(req.System, cache),
		Messages:    wmsgs,
		Tools:       buildTools(req.Tools, cache),
		Temperature: temperature,
		Thinking:    thinking,
		Stream:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("accept", "text/event-stream")
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
	go sse.Run(ctx, resp.Body, out, sse.Options{
		Provider:        a.Name(),
		IdleTimeout:     a.resolveStreamIdleTimeout(),
		MissingTerminal: missingMessageStop,
	}, newEventDecoder())
	return out, nil
}

// blockState accumulates a streaming content block.
type blockState struct {
	typ       string
	toolID    string
	toolName  string
	json      strings.Builder
	thinking  strings.Builder // accumulated thinking text
	signature strings.Builder // accumulated thinking signature
}

// missingMessageStop is reported when the SSE stream ends cleanly without any
// terminal event (P61.2) — see eventDecoder.handleData for the two that count,
// and sse.Run, which enforces the requirement.
const missingMessageStop = "read stream: the response ended without a message_stop — " +
	"the generation was cut off mid-stream (the server closed the connection)"

// eventDecoder translates the Messages API SSE stream into provider events. It
// owns only the per-event decode and the content-block accumulation that
// decode implies (P61.6); the read loop, the idle watchdog, the terminal-event
// requirement, scanner-error classification and the final EventDone all live in
// sse.Run, shared with the other adapters.
//
// Framing is why it also implements sse.Flusher: an event is dispatched on the
// blank line that ends it, so the last event of a stream that does not end with
// one exists only in dataBuf until Flush runs.
type eventDecoder struct {
	blocks  map[int]*blockState
	usage   *provider.Usage
	stop    provider.StopReason
	dataBuf strings.Builder
}

func newEventDecoder() *eventDecoder {
	return &eventDecoder{
		blocks: map[int]*blockState{},
		usage:  &provider.Usage{},
		stop:   provider.StopOther,
	}
}

func (d *eventDecoder) Line(line string, emit func(provider.Event) bool) sse.Status {
	if line == "" {
		return d.dispatch(emit)
	}
	if rest, ok := strings.CutPrefix(line, "data:"); ok {
		d.dataBuf.WriteString(strings.TrimSpace(rest))
	}
	// "event:" lines are ignored; the JSON payload carries its own "type".
	return sse.StatusContinue
}

// Flush decodes a final event left unterminated by the end of the stream. It
// runs before sse.Run classifies a read error, matching the order this adapter
// used before the lifecycle moved: a buffered final message_delta is what makes
// an otherwise-complete stream count as terminal.
func (d *eventDecoder) Flush(emit func(provider.Event) bool) sse.Status {
	return d.dispatch(emit)
}

func (d *eventDecoder) Finish(func(provider.Event) bool) (provider.StopReason, *provider.Usage, bool) {
	return d.stop, d.usage, true
}

func (d *eventDecoder) dispatch(emit func(provider.Event) bool) sse.Status {
	if d.dataBuf.Len() == 0 {
		return sse.StatusContinue
	}
	data := d.dataBuf.String()
	d.dataBuf.Reset()
	return d.handleData(data, emit)
}

// sseEvent is the decoded JSON of a single SSE data payload.
type sseEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Message struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (d *eventDecoder) handleData(data string, emit func(provider.Event) bool) sse.Status {
	blocks, usage := d.blocks, d.usage
	var ev sseEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		// Ignore malformed keepalive lines rather than aborting the stream.
		return sse.StatusContinue
	}

	terminal := false
	switch ev.Type {
	case "message_start":
		usage.InputTokens = ev.Message.Usage.InputTokens
		usage.CacheCreationTokens = ev.Message.Usage.CacheCreationInputTokens
		usage.CacheReadTokens = ev.Message.Usage.CacheReadInputTokens
	case "content_block_start":
		blocks[ev.Index] = &blockState{
			typ:      ev.ContentBlock.Type,
			toolID:   ev.ContentBlock.ID,
			toolName: ev.ContentBlock.Name,
		}
		if ev.ContentBlock.Type == "tool_use" && ev.ContentBlock.Name != "" {
			if !emit(provider.Event{Type: provider.EventToolUseStart, ToolUse: &provider.ToolUseBlock{
				ID:   ev.ContentBlock.ID,
				Name: ev.ContentBlock.Name,
			}}) {
				return sse.StatusAbort
			}
		}
	case "content_block_delta":
		bs := blocks[ev.Index]
		switch ev.Delta.Type {
		case "text_delta":
			if ev.Delta.Text != "" {
				if !emit(provider.Event{Type: provider.EventTextDelta, Text: ev.Delta.Text}) {
					return sse.StatusAbort
				}
			}
		case "input_json_delta":
			if bs != nil {
				bs.json.WriteString(ev.Delta.PartialJSON)
			}
		case "thinking_delta":
			if bs != nil {
				bs.thinking.WriteString(ev.Delta.Thinking)
			}
			if ev.Delta.Thinking != "" {
				if !emit(provider.Event{Type: provider.EventThinkingDelta, Text: ev.Delta.Thinking}) {
					return sse.StatusAbort
				}
			}
		case "signature_delta":
			if bs != nil {
				bs.signature.WriteString(ev.Delta.Signature)
			}
		}
	case "content_block_stop":
		bs := blocks[ev.Index]
		switch {
		case bs == nil:
		case bs.typ == "tool_use":
			input := strings.TrimSpace(bs.json.String())
			if input == "" {
				input = "{}"
			}
			if !json.Valid([]byte(input)) {
				// LLM-08: the accumulated input_json_delta fragments do not parse.
				// The OpenAI adapter has reported this as a classified error since
				// P35.2; this one used to hand the broken bytes downstream as a
				// json.RawMessage, where it surfaced as an opaque unmarshal failure
				// at tool dispatch with nothing naming the stream as the cause.
				// The tool name goes in Detail, never in Message — it is
				// model-authored text and Message is what the retry and
				// backend-liveness classifiers substring-match (P61.7).
				emit(provider.Event{Type: provider.EventError,
					Err: provider.NewMalformedToolCallError(providerName, bs.toolName)})
				return sse.StatusAbort
			}
			if !emit(provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID:    bs.toolID,
				Name:  bs.toolName,
				Input: json.RawMessage(input),
			}}) {
				return sse.StatusAbort
			}
		case bs.typ == "thinking":
			if !emit(provider.Event{Type: provider.EventThinking, Thinking: &provider.ThinkingBlock{
				Text:      bs.thinking.String(),
				Signature: bs.signature.String(),
			}}) {
				return sse.StatusAbort
			}
		}
		delete(blocks, ev.Index)
	case "message_delta":
		if ev.Delta.StopReason != "" {
			// P61.2: the message stated why it ended and carried its final
			// usage — everything message_stop adds is envelope. Counting it as
			// terminal keeps a stream whose last event was dropped from being
			// reported as a transport failure.
			terminal = true
			d.stop = mapStopReason(ev.Delta.StopReason)
		}
		if ev.Usage.OutputTokens > 0 {
			usage.OutputTokens = ev.Usage.OutputTokens
		}
	case "message_stop":
		terminal = true // P61.2: the protocol's own end-of-stream event
	case "error":
		// LLM-08: a mid-stream {"error":...} envelope used to be a bare
		// fmt.Errorf, which no classifier can read — errors.As finds no
		// *APIError, so IsContextOverflowError and IsBackendUnavailableError both
		// answer false and the phased drive aborts on an unclassifiable error
		// instead of resetting the context or waiting for the backend. Both other
		// adapters have gone through NewStreamError since P33.16.
		//
		// Type and message are both provider-authored (this envelope comes from
		// the API's infrastructure, not from a generation), so both are safe to
		// classify on, and both are needed: the type carries `invalid_request_error`
		// while the message carries `prompt is too long`, and it is the latter that
		// makes an over-long prompt classify as a recoverable context overflow
		// rather than a generic terminal failure. The rendered text is unchanged.
		emit(provider.Event{Type: provider.EventError,
			Err: provider.NewStreamError(providerName, ev.Error.Type+": "+ev.Error.Message)})
		return sse.StatusAbort
	}
	if terminal {
		return sse.StatusTerminal
	}
	return sse.StatusContinue
}

func mapStopReason(s string) provider.StopReason {
	switch s {
	case "end_turn", "stop_sequence":
		return provider.StopEndTurn
	case "tool_use":
		return provider.StopToolUse
	case "max_tokens":
		return provider.StopMaxTokens
	default:
		return provider.StopOther
	}
}
