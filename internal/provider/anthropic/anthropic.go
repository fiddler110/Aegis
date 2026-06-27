// Package anthropic implements a provider.Adapter for the Anthropic Messages
// API. It hand-rolls the HTTP + SSE handling so the harness fully owns the
// normalization between the wire format and the provider package's types.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/provider"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
)

// Adapter talks to the Anthropic Messages API.
type Adapter struct {
	apiKey   string
	baseURL  string
	client   *http.Client
	cache    bool // emit prompt-cache breakpoints
	headers  map[string]string
	thinking *provider.ThinkingConfig // non-nil enables extended thinking
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

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) Option {
	return func(a *Adapter) { a.client = c }
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
		client:  &http.Client{Timeout: 10 * time.Minute},
		cache:   true,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name implements provider.Adapter.
func (a *Adapter) Name() string { return "anthropic" }

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
	if a.cache {
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
		System:      buildSystem(req.System, a.cache),
		Messages:    wmsgs,
		Tools:       buildTools(req.Tools, a.cache),
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
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, provider.NewHTTPError(a.Name(), resp.StatusCode,
			resp.Header.Get("Retry-After"), strings.TrimSpace(string(msg)))
	}

	out := make(chan provider.Event)
	go a.consume(ctx, resp.Body, out)
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

func (a *Adapter) consume(ctx context.Context, body io.ReadCloser, out chan<- provider.Event) {
	defer close(out)
	defer body.Close()

	emit := func(ev provider.Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	blocks := map[int]*blockState{}
	usage := &provider.Usage{}
	stop := provider.StopOther

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var dataBuf strings.Builder
	dispatch := func() bool {
		if dataBuf.Len() == 0 {
			return true
		}
		data := dataBuf.String()
		dataBuf.Reset()
		return a.handleData(data, blocks, usage, &stop, emit)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !dispatch() {
				return
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			dataBuf.WriteString(strings.TrimSpace(rest))
		}
		// "event:" lines are ignored; the JSON payload carries its own "type".
	}
	if !dispatch() {
		return
	}
	if err := scanner.Err(); err != nil {
		emit(provider.Event{Type: provider.EventError, Err: fmt.Errorf("anthropic: read stream: %w", err)})
		return
	}

	emit(provider.Event{Type: provider.EventDone, Stop: stop, Usage: usage})
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

func (a *Adapter) handleData(data string, blocks map[int]*blockState, usage *provider.Usage, stop *provider.StopReason, emit func(provider.Event) bool) bool {
	var ev sseEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		// Ignore malformed keepalive lines rather than aborting the stream.
		return true
	}

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
	case "content_block_delta":
		bs := blocks[ev.Index]
		switch ev.Delta.Type {
		case "text_delta":
			if ev.Delta.Text != "" {
				if !emit(provider.Event{Type: provider.EventTextDelta, Text: ev.Delta.Text}) {
					return false
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
					return false
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
			if !emit(provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID:    bs.toolID,
				Name:  bs.toolName,
				Input: json.RawMessage(input),
			}}) {
				return false
			}
		case bs.typ == "thinking":
			if !emit(provider.Event{Type: provider.EventThinking, Thinking: &provider.ThinkingBlock{
				Text:      bs.thinking.String(),
				Signature: bs.signature.String(),
			}}) {
				return false
			}
		}
		delete(blocks, ev.Index)
	case "message_delta":
		if ev.Delta.StopReason != "" {
			*stop = mapStopReason(ev.Delta.StopReason)
		}
		if ev.Usage.OutputTokens > 0 {
			usage.OutputTokens = ev.Usage.OutputTokens
		}
	case "error":
		emit(provider.Event{Type: provider.EventError, Err: fmt.Errorf("anthropic: %s: %s", ev.Error.Type, ev.Error.Message)})
		return false
	}
	return true
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
