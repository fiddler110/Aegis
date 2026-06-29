// Package openai implements a provider.Adapter for OpenAI-compatible
// chat-completions APIs, demonstrating the harness's provider abstraction. It
// translates the harness's Anthropic-shaped message model to/from the
// chat-completions format and parses the streaming SSE response.
package openai

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

const defaultBaseURL = "https://api.openai.com/v1"

// Adapter talks to an OpenAI-compatible chat-completions endpoint.
type Adapter struct {
	apiKey          string
	baseURL         string
	client          *http.Client
	headers         map[string]string
	think           *bool  // nil = omit; false = disable Ollama extended thinking
	reasoningEffort string // "low"|"medium"|"high" for OpenAI o1/o3; "" = omit
}

// Option configures the adapter.
type Option func(*Adapter)

// WithBaseURL overrides the API base URL (for Azure/Open-compatible servers).
func WithBaseURL(u string) Option {
	return func(a *Adapter) {
		if u != "" {
			a.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithHeaders adds extra HTTP headers to every request (e.g. gateway auth).
func WithHeaders(h map[string]string) Option {
	return func(a *Adapter) { a.headers = h }
}

// WithThink controls the extended-thinking parameter for Ollama-served
// reasoning models (Qwen3, DeepSeek-R1). Pass false to suppress the thinking
// preamble and get plain content-only responses. Nil (the default) omits the
// parameter. Do NOT use this for real OpenAI endpoints — use WithReasoningEffort.
func WithThink(v *bool) Option {
	return func(a *Adapter) { a.think = v }
}

// WithReasoningEffort sets the reasoning_effort field for OpenAI o1/o3 models.
// Valid values: "low", "medium", "high". An empty string omits the field.
func WithReasoningEffort(effort string) Option {
	return func(a *Adapter) { a.reasoningEffort = effort }
}

// New constructs an OpenAI adapter.
func New(apiKey string, opts ...Option) *Adapter {
	a := &Adapter{apiKey: apiKey, baseURL: defaultBaseURL, client: &http.Client{Timeout: 10 * time.Minute}}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name implements provider.Adapter.
func (a *Adapter) Name() string { return "openai" }

// --- wire types ---

type wireMessage struct {
	Role string `json:"role"`
	// Content is a plain string for text-only messages, or a []contentPart array
	// when a user turn carries images (chat-completions multimodal format).
	Content    any            `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// contentPart is one element of a multimodal user message.
type contentPart struct {
	Type     string        `json:"type"` // "text" or "image_url"
	Text     string        `json:"text,omitempty"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

type imageURLPart struct {
	URL string `json:"url"` // data:<media_type>;base64,<data>
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireRequest struct {
	Model           string         `json:"model"`
	Messages        []wireMessage  `json:"messages"`
	Tools           []wireTool     `json:"tools,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	Temperature     *float64       `json:"temperature,omitempty"`
	Stream          bool           `json:"stream"`
	StreamOptions   map[string]any `json:"stream_options,omitempty"`
	Think           *bool          `json:"think,omitempty"`            // Ollama extended-thinking control
	ReasoningEffort string         `json:"reasoning_effort,omitempty"` // OpenAI o1/o3
}

// translate converts harness messages to chat-completions messages.
func translate(system string, msgs []provider.Message) ([]wireMessage, error) {
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
					tc := wireToolCall{ID: v.ID, Type: "function"}
					tc.Function.Name = v.Name
					tc.Function.Arguments = string(v.Input)
					if tc.Function.Arguments == "" {
						tc.Function.Arguments = "{}"
					}
					wm.ToolCalls = append(wm.ToolCalls, tc)
				}
			}
			if text != "" {
				wm.Content = text
			}
			if wm.Content == nil && len(wm.ToolCalls) == 0 {
				continue // skip empty assistant turns (e.g. model returned nothing)
			}
			out = append(out, wm)
		case provider.RoleUser:
			// Tool results become separate tool-role messages; text + images
			// become a single user message (multimodal when images are present).
			var text string
			var images []provider.ImageBlock
			for _, b := range m.Content {
				switch v := b.(type) {
				case provider.TextBlock:
					text += v.Text
				case provider.ImageBlock:
					images = append(images, v)
				case provider.ToolResultBlock:
					out = append(out, wireMessage{Role: "tool", ToolCallID: v.ToolUseID, Content: v.Content})
				}
			}
			switch {
			case len(images) > 0:
				parts := make([]contentPart, 0, len(images)+1)
				if text != "" {
					parts = append(parts, contentPart{Type: "text", Text: text})
				}
				for _, img := range images {
					parts = append(parts, contentPart{
						Type:     "image_url",
						ImageURL: &imageURLPart{URL: fmt.Sprintf("data:%s;base64,%s", img.MediaType, img.Data)},
					})
				}
				out = append(out, wireMessage{Role: "user", Content: parts})
			case text != "":
				out = append(out, wireMessage{Role: "user", Content: text})
			}
		}
	}
	return out, nil
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
	if a.apiKey == "" && a.baseURL == defaultBaseURL {
		return nil, fmt.Errorf("openai: missing API key (set OPENAI_API_KEY)")
	}
	msgs, err := translate(req.System, req.Messages)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(wireRequest{
		Model:           req.Model,
		Messages:        msgs,
		Tools:           translateTools(req.Tools),
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		Stream:          true,
		StreamOptions:   map[string]any{"include_usage": true},
		Think:           a.think,
		ReasoningEffort: a.reasoningEffort,
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")
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
	go consume(ctx, resp.Body, out)
	return out, nil
}

// toolAccum accumulates a streamed tool call by index.
type toolAccum struct {
	id   string
	name string
	args strings.Builder
}

func consume(ctx context.Context, body io.ReadCloser, out chan<- provider.Event) {
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

	tools := map[int]*toolAccum{}
	usage := &provider.Usage{}
	stop := provider.StopEndTurn

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					Thinking         string `json:"thinking"`          // Ollama (Gemma4, Qwen3)
					Reasoning        string `json:"reasoning"`         // Ollama (some versions)
					ReasoningContent string `json:"reasoning_content"` // DeepSeek-R1
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}
		for _, ch := range chunk.Choices {
			if td := ch.Delta.Thinking + ch.Delta.Reasoning + ch.Delta.ReasoningContent; td != "" {
				if !emit(provider.Event{Type: provider.EventThinkingDelta, Text: td}) {
					return
				}
			}
			if ch.Delta.Content != "" {
				if !emit(provider.Event{Type: provider.EventTextDelta, Text: ch.Delta.Content}) {
					return
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				acc := tools[tc.Index]
				if acc == nil {
					acc = &toolAccum{}
					tools[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args.WriteString(tc.Function.Arguments)
			}
			if ch.FinishReason == "tool_calls" {
				stop = provider.StopToolUse
			}
		}
	}
	if err := scanner.Err(); err != nil {
		emit(provider.Event{Type: provider.EventError, Err: fmt.Errorf("openai: read stream: %w", err)})
		return
	}

	// Emit accumulated tool calls in index order.
	for i := 0; i < len(tools); i++ {
		acc := tools[i]
		if acc == nil {
			continue
		}
		args := strings.TrimSpace(acc.args.String())
		if args == "" {
			args = "{}"
		}
		if !emit(provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: acc.id, Name: acc.name, Input: json.RawMessage(args),
		}}) {
			return
		}
	}
	emit(provider.Event{Type: provider.EventDone, Stop: stop, Usage: usage})
}
