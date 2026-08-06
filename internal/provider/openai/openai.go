// Package openai implements a provider.Adapter for OpenAI-compatible
// chat-completions APIs, demonstrating the harness's provider abstraction. It
// translates the harness's Anthropic-shaped message model to/from the
// chat-completions format and parses the streaming SSE response.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/sse"
	"github.com/fiddler110/aegis/internal/tokenest"
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

	// streamIdleTimeout (P59.2/P61.1) bounds the gap *between* streamed chunks.
	// Same field, semantics and default as the native Ollama adapter's — the
	// reasoning for why nothing else covers that window is documented there and
	// on sse.DefaultStreamIdleTimeout. It matters at least as much here: this
	// adapter is what talks to Ollama's OpenAI-compat /v1 endpoint, so the
	// wedged-runner shape P59.2 was filed for reaches it too. 0 selects
	// sse.DefaultStreamIdleTimeout; negative disables the bound entirely.
	streamIdleTimeout time.Duration

	// sharedContextWindow (P61.4) records that this adapter's backend spends
	// one budget on prompt *and* completion — Ollama's num_ctx — so
	// Request.NumCtx bounds the completion too and max_tokens has to be
	// reconciled against it. It is off by default and only ever set by a
	// caller that has positively identified the backend: see
	// clampMaxTokens for why nothing weaker is allowed to turn it on.
	sharedContextWindow bool
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

// WithResponseHeaderTimeout overrides how long the streamed request waits for
// response headers (provider.response_header_timeout, P35.5). <= 0 falls back
// to sse.DefaultResponseHeaderTimeout.
//
// It composes onto whatever client the adapter already has rather than
// replacing it (P61.5), so it commutes with any other option that touches the
// client and option order carries no meaning.
func WithResponseHeaderTimeout(d time.Duration) Option {
	return func(a *Adapter) { a.client = sse.ApplyResponseHeaderTimeout(a.client, d) }
}

// WithStreamIdleTimeout overrides how long the adapter waits between two
// streamed chunks before giving up on the response
// (provider.stream_idle_timeout, P59.2; wired to this adapter by P61.1). 0
// selects sse.DefaultStreamIdleTimeout; pass a negative duration to disable the
// bound. Deliberately identical to ollama.WithStreamIdleTimeout: the config key
// is one key, so it must mean one thing on every backend.
//
// It touches no client state, so unlike WithResponseHeaderTimeout it carries no
// ordering hazard against another client-touching option (P61.5).
func WithStreamIdleTimeout(d time.Duration) Option {
	return func(a *Adapter) { a.streamIdleTimeout = d }
}

// WithSharedContextWindow declares that the configured backend serves prompt
// and completion out of one budget, which is what makes Request.NumCtx a bound
// on the completion length and lets clampMaxTokens reconcile the pair (P61.4).
//
// It is the caller's positive identification of the backend, not a guess: only
// wire it where the server behind base_url is known to be Ollama. Left unset,
// the adapter sends max_tokens through untouched exactly as it always has,
// which is the correct behavior for every OpenAI-compatible backend that bills
// max_tokens against a separate output allowance.
func WithSharedContextWindow(v bool) Option {
	return func(a *Adapter) { a.sharedContextWindow = v }
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

// New constructs an OpenAI adapter.
func New(apiKey string, opts ...Option) *Adapter {
	a := &Adapter{apiKey: apiKey, baseURL: defaultBaseURL, client: sse.NewStreamingClient(0)}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name implements provider.Adapter.
func (a *Adapter) Name() string { return "openai" }

// Compile-time proof that the adapter satisfies the optional capability the
// drive's recovery is gated on. Without it, P50.1's wait-and-resume is inert on
// this adapter — which is exactly the state P61.6 left the OpenAI-compat path
// in: a mid-stream backend death was correctly *classified*
// (provider.IsBackendUnavailableError) but drive.recoverBackendDown still
// returned backendNotDown because nothing could answer "is it back yet?", so
// the drive aborted instead of resuming from disk.
var _ provider.HealthChecker = (*Adapter)(nil)

// Healthy implements provider.HealthChecker: a cheap GET <base>/models against
// the configured OpenAI-compatible server, used by the phased drive (P50.1) to
// wait for a crashed/restarting backend to come back before resuming a phase
// from disk. This adapter is what talks to Ollama's OpenAI-compat /v1 endpoint,
// so the `ollama serve` killed mid-response shape reaches it too.
//
// The endpoint is deliberately /models and NOT Ollama's /api/version: the native
// probe's path is not part of the OpenAI API, and this adapter also serves real
// OpenAI, LM Studio, liteLLM and assorted gateways. /models is the one liveness
// endpoint every OpenAI-compatible server implements, and it is side-effect-free
// — it lists what is configured, loads or unloads nothing, and is not a metered
// completion, so probing on a paid backend costs nothing.
//
// What counts as healthy is narrower than the native probe's "200 or nothing",
// and the difference is deliberate. This question is *liveness*, not usability:
// recoverBackendDown already knows the request failed; all it needs to learn is
// whether there is a server on the other end again. A 401 (no key configured for
// a gateway that wants one), 403 or 404 (a backend that routes /chat/completions
// but not /models) all prove an HTTP server answered — which is the honest
// signal for "reachable". Treating those as unhealthy would be worse than
// having no probe at all: the drive would wait out the full recovery budget
// against a server that was up the whole time. The carve-out is the gateway 5xx
// trio (502/504 bad gateway/timeout, 503 unavailable), where the responder is a
// proxy explicitly saying the upstream model server — the thing we are waiting
// for — is not there.
//
// It runs on the adapter's own transport (so proxy/TLS configuration applies)
// under a short timeout of its own, so a probe can't block on the long prefill
// budget the streaming client allows — see healthClient. Configured headers are
// sent, mirroring Stream and the native probe, since a gateway may need them to
// route the request at all; the Authorization header is sent only when a key was
// configured, so an unauthenticated local server isn't handed an empty bearer.
func (a *Adapter) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/models", nil)
	if err != nil {
		return false
	}
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
	resp, err := a.healthClient().Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<12))
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return false
	}
	return true
}

// healthProbeTimeout is the whole-request bound on the liveness probe. A probe
// must fail fast when the server is down, not hang — which is why it cannot
// simply reuse the streaming client, whose Client.Timeout is deliberately zero
// so a long prefill isn't cut off (P59.2). Same value as the native adapter's,
// because it answers the same question about the same class of server.
const healthProbeTimeout = 3 * time.Second

// healthClient builds the probe's HTTP client: the *transport* is the streaming
// client's, so any proxy, TLS or dialer configuration the adapter was given
// applies to the probe too and the two share a connection pool — but the timeout
// is the probe's own, since inheriting the streaming client's unbounded one
// would defeat the point of a liveness check (P61.5). A package-level client
// with its own default transport would quietly mean the probe gating P50.1
// recovery ignored the user's transport configuration entirely.
//
// Built per call rather than cached: an http.Client is a small value, the probe
// is not a hot path, and the transport — the part that holds state worth
// reusing — is shared, not rebuilt.
func (a *Adapter) healthClient() *http.Client {
	if a.client == nil {
		return &http.Client{Timeout: healthProbeTimeout}
	}
	return &http.Client{
		Transport:     a.client.Transport,
		CheckRedirect: a.client.CheckRedirect,
		Jar:           a.client.Jar,
		Timeout:       healthProbeTimeout,
	}
}

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
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []wireTool    `json:"tools,omitempty"`
	// MaxTokens and MaxCompletionTokens are mutually exclusive on the wire:
	// real OpenAI o1/o3-class reasoning models reject max_tokens outright
	// (a 400) and require max_completion_tokens instead, while every other
	// model (and Ollama's OpenAI-compatible endpoint) expects max_tokens.
	// Stream picks exactly one of these per request via isReasoningModel.
	MaxTokens           int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	Stream              bool           `json:"stream"`
	StreamOptions       map[string]any `json:"stream_options,omitempty"`
	Think               *bool          `json:"think,omitempty"`            // Ollama extended-thinking control
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"` // OpenAI o1/o3
}

// isReasoningModel reports whether model is an OpenAI o1/o3-class reasoning
// model. Matching mirrors the cost package's model-pricing lookup: longest
// registered prefix, retrying after a vendor prefix like "openai/o1-mini"
// (OpenRouter-style) if the bare id doesn't match.
func isReasoningModel(model string) bool {
	if hasReasoningPrefix(model) {
		return true
	}
	if i := strings.LastIndex(model, "/"); i >= 0 && i+1 < len(model) {
		return hasReasoningPrefix(model[i+1:])
	}
	return false
}

func hasReasoningPrefix(model string) bool {
	return strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3")
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

// minCompletionTokens is the floor clampMaxTokens never goes below, and is
// deliberately the same 512 as the native adapter's minNumPredict: a prompt
// that has already filled the window still has to be allowed to say something,
// and the two paths reach the same server, so they must not disagree about how
// much "something" is.
const minCompletionTokens = 512

// clampMaxTokens bounds the requested completion length by the room actually
// left in the served window (P61.4), and is the OpenAI-compat counterpart to
// ollama.clampNumPredict (P59.1) — read that function's comment for why the
// arithmetic is what it is; this one mirrors it deliberately, reserve, floor
// and one-directionality included, because the two adapters can be pointed at
// the same Ollama server and a user must not get a different completion budget
// depending on which one the config happens to select.
//
// What differs is the gate. On Ollama num_ctx is one budget covering prompt and
// completion, so provider.max_tokens (default 32768) against a stock 4096
// window asks for 8x the whole window in output; the ceiling is then hit
// mid-generation, which arrives here as finish_reason "length" → StopMaxTokens
// → the engine's continuation retry → context growth until the run burns to its
// iteration cap. But this adapter also talks to real OpenAI and to every other
// OpenAI-compatible backend, where max_tokens is a *separate* output allowance
// and clamping it would truncate a legitimate long generation — a worse bug
// than the one being fixed. So three conditions must all hold, and every one of
// them fails closed:
//
//   - sharedContextWindow: the caller positively identified the backend as one
//     with a shared budget (providerfactory sets it only for an Ollama-port
//     base_url). Unset — which is the zero value, so it is what any embedder,
//     test or cloud config gets — nothing is clamped.
//   - baseURL is not api.openai.com: a second, structural refusal that holds
//     even if a caller sets the option wrongly. api.openai.com is never a
//     shared-budget backend, whatever it is asked to be.
//   - numCtx > 0: a window is genuinely known, having been resolved per model
//     against the live server (internal/server/contextwindow.go →
//     provider.WithNumCtx). No window is left untouched rather than clamped
//     against an invented default — the compat endpoint cannot report the
//     served window, so there is no honest number to guess, and `aegis doctor`'s
//     generation-budget row is what tells the user in that case.
func (a *Adapter) clampMaxTokens(maxTokens, numCtx int, system string, msgs []provider.Message) int {
	if !a.sharedContextWindow || a.baseURL == defaultBaseURL {
		return maxTokens
	}
	if maxTokens <= 0 || numCtx <= 0 {
		return maxTokens
	}
	headroom := numCtx - tokenest.Messages(system, msgs) - numCtx/20
	if headroom >= maxTokens {
		return maxTokens
	}
	if headroom < minCompletionTokens {
		headroom = minCompletionTokens
	}
	if headroom > numCtx {
		headroom = numCtx
	}
	// Debug, not Warn, for the same reason the native adapter logs it that way:
	// this fires on every request of a misconfigured pair, and the place to
	// tell a user about the misconfiguration once is `aegis doctor`.
	slog.Default().Debug("openai: clamped max_tokens to the context window's remaining headroom",
		"requested", maxTokens, "sent", headroom, "num_ctx", numCtx)
	return headroom
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
	wr := wireRequest{
		Model:           req.Model,
		Messages:        msgs,
		Tools:           translateTools(req.Tools),
		Temperature:     req.Temperature,
		Stream:          true,
		StreamOptions:   map[string]any{"include_usage": true},
		Think:           a.think,
		ReasoningEffort: a.reasoningEffort,
	}
	maxTokens := a.clampMaxTokens(req.MaxTokens, req.NumCtx, req.System, req.Messages)
	if isReasoningModel(req.Model) {
		wr.MaxCompletionTokens = maxTokens
	} else {
		wr.MaxTokens = maxTokens
	}
	body, err := json.Marshal(wr)
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
		// P35.6: on an OpenAI-compat local backend (e.g. Ollama's compat
		// endpoint) the response header is withheld until prefill finishes,
		// so a header-timeout here means "prefill is slower than the
		// configured budget," not "the server is unreachable." Rewrap it into
		// an actionable, non-retryable error naming the levers instead of the
		// bare Go transport string.
		if provider.IsResponseHeaderTimeoutError(err) {
			return nil, provider.NewResponseHeaderTimeoutError(a.Name(), err)
		}
		return nil, provider.NewTransportError(a.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, sse.HandleErrorResponse(a.Name(), resp)
	}

	out := make(chan provider.Event)
	go sse.Run(ctx, resp.Body, out, sse.Options{
		Provider:        "openai",
		IdleTimeout:     a.resolveStreamIdleTimeout(),
		MissingTerminal: missingTerminator,
	}, newChunkDecoder())
	return out, nil
}

// toolAccum accumulates a streamed tool call by index. announced records that
// the call's EventToolUseStart has already been emitted, so a name repeated
// across deltas can't announce the same call twice.
type toolAccum struct {
	id        string
	name      string
	args      strings.Builder
	announced bool
}

// errorMessage extracts the human-readable message from the "error" field of
// a streamed chunk, which servers spell two ways: Ollama emits a bare string
// ({"error":"model runner has unexpectedly stopped"}) mid-stream when a
// worker OOMs or overflows its context, while OpenAI and vLLM emit an object
// ({"error":{"message":"..."}}). An object carrying no message degrades to
// its raw JSON rather than being lost. Anything else — an absent or null
// field, a chunk whose "error" is neither string nor object — returns "" so
// the caller drops it as benign noise instead of failing the turn.
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

// streamErrorMessage is errorMessage for a chunk that failed to decode as a
// completion chunk at all, pulling the error envelope out on its own.
func streamErrorMessage(data string) string {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(data), &env) != nil {
		return ""
	}
	return errorMessage(env.Error)
}

// missingTerminator is reported when the SSE stream ends cleanly without
// either accepted terminator (P61.2) — see chunkDecoder.Line for why there are
// two, and sse.Run, which enforces the requirement.
const missingTerminator = "read stream: the response ended without a finish_reason or [DONE] — " +
	"the generation was cut off mid-stream (the server closed the connection or the model runner exited)"

// chunkDecoder translates the chat-completions SSE stream into provider
// events. It owns only the per-chunk decode plus the tool-call accumulation
// that decode implies (P61.6); the read loop, the idle watchdog, the
// terminal-chunk requirement, scanner-error classification and the final
// EventDone all live in sse.Run, shared with the other adapters.
type chunkDecoder struct {
	tools     map[int]*toolAccum
	usage     *provider.Usage
	stop      provider.StopReason
	sawLength bool // a choice reported finish_reason "length" (context ceiling hit)
}

func newChunkDecoder() *chunkDecoder {
	return &chunkDecoder{
		tools: map[int]*toolAccum{},
		usage: &provider.Usage{},
		stop:  provider.StopEndTurn,
	}
}

func (d *chunkDecoder) Line(line string, emit func(provider.Event) bool) sse.Status {
	tools, usage := d.tools, d.usage
	data, ok := strings.CutPrefix(line, "data:")
	if !ok {
		return sse.StatusContinue
	}
	data = strings.TrimSpace(data)
	if data == "[DONE]" {
		// The OpenAI sentinel: terminal *and* the last line of the stream,
		// so sse.Run stops reading here rather than draining to EOF.
		return sse.StatusEnd
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
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		if msg := streamErrorMessage(data); msg != "" {
			emit(provider.Event{Type: provider.EventError, Err: provider.NewStreamError("openai", msg)})
			return sse.StatusAbort
		}
		return sse.StatusContinue
	}
	if msg := errorMessage(chunk.Error); msg != "" {
		// P35.2: a generation truncated at the context ceiling mid-tool-call
		// can surface as an opaque "invalid tool call arguments ... unexpected
		// end of JSON input" — name the discoverable fix (raise
		// context_window) when a truncation signal is present (a prior
		// finish_reason "length" or the truncated-tool-call message shape)
		// instead of falling through to the generic parse failure.
		if d.sawLength || provider.IsTruncatedToolCallError(msg) {
			emit(provider.Event{Type: provider.EventError, Err: provider.NewContextTruncationError("openai", msg)})
			return sse.StatusAbort
		}
		// P33.16: classify the mid-stream {"error":...} envelope so a
		// transient failure (worker crash, model-load failure, OOM) carries a
		// retryable verdict while a deterministic one (context overflow,
		// invalid request) stays terminal — retrying the latter only burns
		// another prompt-eval on a slow local model.
		emit(provider.Event{Type: provider.EventError, Err: provider.NewStreamError("openai", msg)})
		return sse.StatusAbort
	}
	if chunk.Usage != nil {
		usage.InputTokens = chunk.Usage.PromptTokens
		usage.OutputTokens = chunk.Usage.CompletionTokens
	}
	terminal := false
	for _, ch := range chunk.Choices {
		if td := ch.Delta.Thinking + ch.Delta.Reasoning + ch.Delta.ReasoningContent; td != "" {
			if !emit(provider.Event{Type: provider.EventThinkingDelta, Text: td}) {
				return sse.StatusAbort
			}
		}
		if ch.Delta.Content != "" {
			if !emit(provider.Event{Type: provider.EventTextDelta, Text: ch.Delta.Content}) {
				return sse.StatusAbort
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
			if acc.name != "" && !acc.announced {
				acc.announced = true
				if !emit(provider.Event{Type: provider.EventToolUseStart, ToolUse: &provider.ToolUseBlock{
					ID: acc.id, Name: acc.name,
				}}) {
					return sse.StatusAbort
				}
			}
			acc.args.WriteString(tc.Function.Arguments)
		}
		if ch.FinishReason != "" {
			// P61.2: the choice reported why it ended, which is the wire's
			// own statement that this generation completed. Accepted as a
			// terminator alongside "[DONE]", because Ollama's /v1 compat
			// endpoint omits the sentinel and requiring it alone would
			// report those legitimate streams as transport failures — a
			// worse regression than the bug. Unlike "[DONE]" it does not
			// end the read: further chunks (a usage-only trailer) may
			// legitimately follow.
			terminal = true
		}
		switch ch.FinishReason {
		case "tool_calls":
			d.stop = provider.StopToolUse
		case "length":
			// Without this the caller cannot tell a model that chose to
			// stop from one cut off mid-answer: stop defaults to
			// StopEndTurn, so a truncated response reported as a clean end
			// of turn. Guarded on StopToolUse for parity with the native
			// Ollama adapter — a cap reached after the tool call already
			// landed truncated nothing the caller was waiting for.
			d.sawLength = true
			if d.stop != provider.StopToolUse {
				d.stop = provider.StopMaxTokens
			}
		}
	}
	if terminal {
		return sse.StatusTerminal
	}
	return sse.StatusContinue
}

// Finish emits the tool calls accumulated across the stream. It runs only
// after sse.Run has confirmed the stream reached a terminator (P61.2), which
// is the point of doing it here rather than inline: a stream cut off
// mid-tool-call must be reported as truncated, not flushed as if the
// half-received arguments were whole.
func (d *chunkDecoder) Finish(emit func(provider.Event) bool) (provider.StopReason, *provider.Usage, bool) {
	tools := d.tools
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
		if !json.Valid([]byte(args)) {
			// P35.2: the accumulated arguments are not valid JSON. When the
			// stream also reported finish_reason "length" the call was cut off
			// at the context ceiling, not malformed by the model — surface the
			// actionable, discoverable fix instead of passing broken JSON
			// downstream where it fails as an opaque parse error. A genuine
			// malformed call (no length signal) still yields a plain parse error.
			detail := fmt.Sprintf("invalid tool call arguments for %q: unexpected end of JSON input", acc.name)
			if d.sawLength {
				emit(provider.Event{Type: provider.EventError, Err: provider.NewContextTruncationError("openai", detail)})
				return d.stop, d.usage, false
			}
			emit(provider.Event{Type: provider.EventError, Err: provider.NewStreamError("openai", detail)})
			return d.stop, d.usage, false
		}
		if !emit(provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: acc.id, Name: acc.name, Input: json.RawMessage(args),
		}}) {
			return d.stop, d.usage, false
		}
	}
	return d.stop, d.usage, true
}
