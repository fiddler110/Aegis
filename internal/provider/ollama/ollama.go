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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/sse"
	"github.com/fiddler110/aegis/internal/tokenest"
)

const defaultBaseURL = "http://localhost:11434"

// Adapter talks to an Ollama server's native /api/chat endpoint.
type Adapter struct {
	baseURL   string
	client    *http.Client
	headers   map[string]string
	think     *bool  // nil = omit; false = disable extended thinking
	keepAlive string // "" = omit, let Ollama use its own default (5m)
	logger    *slog.Logger

	// streamIdleTimeout (P59.2) bounds the gap *between* streamed chunks, which
	// is the one window nothing else covers: the streaming client deliberately
	// leaves Client.Timeout at zero (it would cap the whole turn) and
	// ResponseHeaderTimeout stops applying the moment headers arrive. 0 selects
	// DefaultStreamIdleTimeout; negative disables the bound entirely.
	streamIdleTimeout time.Duration

	// numCtx (0 = omit, let Ollama use its own default) is the adapter-wide
	// *fallback* serving context window, used only for requests that carry no
	// NumCtx of their own (P52.4 moved the authoritative value onto
	// provider.Request, since the model is per-request and the adapter is
	// shared). numCtxRaised is the highest window RaiseContextWindow has
	// escalated to, 0 until one happens — see resolveNumCtx for why an
	// escalation has to outrank a per-request value rather than fall back to it.
	// Both are mutable at runtime, so every read and write goes through
	// numCtxMu (P52.6): a single daemon adapter is shared across concurrent
	// sessions, which makes an unguarded escalation a real data race, not a
	// theoretical one.
	numCtxMu     sync.RWMutex
	numCtx       int
	numCtxRaised int

	// thinkRejected latches, per model, the P52.5 verdict "this model 400s the
	// instant `think` is sent". Keyed by model rather than held per-adapter
	// because one daemon adapter serves every model in a session mix: latching
	// adapter-wide on one model's rejection would silently strip `think` from
	// a sibling model that supports it. Values are always true; presence is
	// the signal.
	thinkRejected sync.Map // model name -> bool

	// caps, when set, backs thinkRejected with an on-disk record so the latch
	// survives process exit (P53.5). It stays a *cache in front of* the
	// in-memory map, not a replacement: the map is the hot path and the only
	// thing Stream consults per request, and a store miss falls through to the
	// live discovery path exactly as before. It also carries the user's
	// declared overrides, which is why a store hit can say "not rejected" and
	// must be honored — that is how a stale latch is retracted without
	// deleting the file.
	caps CapabilityStore
	// capsLoaded remembers which models have had their persisted verdict
	// consulted, so a model whose record says nothing doesn't re-read on every
	// request.
	capsLoaded sync.Map // model name -> bool

	// dropsToolCalls caches, per model, whether the server's chat template
	// discards an assistant turn's tool calls when that turn also carries prose
	// (see ollamainfo.TemplateDropsToolCalls). Keyed by model for the same
	// reason thinkRejected is: one daemon adapter serves a session's whole model
	// mix, and Qwen3 and Qwen3.5 disagree about this. Values are meaningful in
	// both directions, so unlike thinkRejected presence alone is not the signal.
	dropsToolCalls sync.Map // model name -> bool
	// detectTemplate is the template probe. Nil disables the whole mitigation:
	// the adapter never reads a template and never withholds prose, which is
	// what a bare New() does. The daemon wires ollamainfo.TemplateDropsToolCalls
	// in via WithTemplateProbe — keeping the /api/show call an injected
	// dependency rather than an implicit one means an adapter pointed at a
	// non-Ollama endpoint (or a test stub) issues no surprise request.
	detectTemplate func(ctx context.Context, base, model string) (bool, bool)
	// warnedTemplate keeps the "template drops tool calls" warning to once per
	// model rather than once per turn.
	warnedTemplate sync.Map // model name -> bool
}

// CapabilityStore is the slice of internal/modelcaps this adapter needs.
// Declared as an interface here so the provider package doesn't depend on the
// store (and so tests can substitute a trivial fake); *modelcaps.Store
// satisfies it, including as a nil pointer, which is why no nil check is
// needed beyond the interface itself.
type CapabilityStore interface {
	// ThinkRejected reports the persisted-or-declared verdict for model, and
	// whether one exists at all.
	ThinkRejected(model string) (rejected bool, known bool)
	// SetThinkRejected persists a newly-discovered rejection.
	SetThinkRejected(model string, rejected bool)
	// TemplateDropsToolCalls reports the persisted template verdict for model,
	// and whether one exists at all.
	TemplateDropsToolCalls(model string) (drops bool, known bool)
	// SetTemplateDropsToolCalls persists a freshly-read template verdict.
	SetTemplateDropsToolCalls(model string, drops bool)
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

// WithCapabilityStore backs the per-model `think`-rejection latch with a
// persistent store (P53.5), so a restart doesn't re-send the request a model
// has already been proven to reject. Omitting it leaves the latch
// process-lifetime only, exactly as before.
func WithCapabilityStore(s CapabilityStore) Option {
	return func(a *Adapter) { a.caps = s }
}

// WithTemplateProbe enables the chat-template mitigation: fn is asked, once
// per model, whether that model's template drops an assistant turn's tool
// calls when the turn also carries prose, and a "yes" makes translate withhold
// the prose so the call survives. Pass ollamainfo.TemplateDropsToolCalls.
// Omitting it (the default) leaves the adapter's wire encoding exactly as it
// was and issues no /api/show request.
func WithTemplateProbe(fn func(ctx context.Context, base, model string) (bool, bool)) Option {
	return func(a *Adapter) { a.detectTemplate = fn }
}

// WithNumCtx sets the adapter's default serving context window
// (options.num_ctx), used for every request that doesn't carry its own
// provider.Request.NumCtx (P52.4). Zero (the default) omits the field when the
// request is silent too, leaving Ollama's own default (OLLAMA_CONTEXT_LENGTH or
// a modelfile-pinned value) in effect.
func WithNumCtx(n int) Option {
	return func(a *Adapter) {
		if n > 0 {
			a.numCtx = n
		}
	}
}

// RaiseContextWindow raises the adapter's num_ctx to n when n exceeds the
// current value, returning true when it actually grew. It implements
// provider.ContextWindowRaiser so a driven build can escalate the serving window
// toward the model's max on a context overflow (P47.5b) instead of aborting.
//
// Safe for concurrent use with Stream (P52.6): the escalation write and the
// doChat read both take numCtxMu. It previously relied on the caller being a
// single-session CLI process, an invariant that dies as soon as the phased
// drive runs inside the daemon, where one adapter is shared across every
// concurrent session. The escalation stays monotonic, so a concurrent raise
// can only ever be observed as a larger window, never a shrink.
func (a *Adapter) RaiseContextWindow(n int) bool {
	a.numCtxMu.Lock()
	defer a.numCtxMu.Unlock()
	if n > a.numCtx {
		a.numCtx = n
		a.numCtxRaised = n
		return true
	}
	return false
}

// RaisedContextWindow implements provider.ContextWindowFloorReporter: the
// window an escalation has raised this adapter to, or 0 when none has happened.
//
// It reports numCtxRaised rather than numCtx deliberately (P59.7). numCtx is the
// adapter-wide *fallback* for requests that carry none of their own, so a
// consumer reading it would mistake a configured default for an escalation and
// override a correctly-detected per-model window with a server-wide one.
// numCtxRaised is only ever non-zero because RaiseContextWindow made it so, and
// resolveNumCtx already treats it as a floor outranking a per-request value —
// so this is exactly the number the engine and the compactor have to agree with
// to stop compacting against a window the adapter no longer serves.
func (a *Adapter) RaisedContextWindow() int {
	a.numCtxMu.RLock()
	defer a.numCtxMu.RUnlock()
	return a.numCtxRaised
}

// contextWindow reads the current adapter-wide num_ctx under the lock that
// RaiseContextWindow writes it under (P52.6).
func (a *Adapter) contextWindow() int {
	a.numCtxMu.RLock()
	defer a.numCtxMu.RUnlock()
	return a.numCtx
}

// resolveNumCtx picks the num_ctx one request is sent with (P52.4): the
// request's own value when it has one, otherwise the adapter's configured
// default — which preserves today's behavior for every caller that doesn't
// populate provider.Request.NumCtx (the CLI phased drive, any non-daemon
// embedder).
//
// A RaiseContextWindow escalation outranks both. That asymmetry is deliberate:
// an escalation is a runtime *response to an overflow that already happened*
// (P47.5b), while a request's NumCtx was computed by the server before the run
// from the window it believed the model would be served with. Letting the
// stale, pre-overflow number win would silently undo the escalation and send
// the drive straight back into the same overflow — so the raised value acts as
// a floor rather than a fallback, staying monotonic exactly as
// RaiseContextWindow promises. Nothing escalates a shared daemon adapter today
// (the sole caller is the single-model CLI phased drive, whose requests carry
// no NumCtx at all), so the floor is inert until a caller deliberately raises
// the window and then genuinely wants it applied.
func (a *Adapter) resolveNumCtx(reqNumCtx int) int {
	a.numCtxMu.RLock()
	defer a.numCtxMu.RUnlock()
	n := reqNumCtx
	if n <= 0 {
		n = a.numCtx
	}
	if a.numCtxRaised > n {
		n = a.numCtxRaised
	}
	return n
}

// Healthy implements provider.HealthChecker: a cheap GET /api/version against
// the Ollama server, used by the phased drive (P50.1) to wait for a
// crashed/restarting local server to come back before resuming a phase from
// disk. It is side-effect-free — /api/version neither loads nor unloads a model
// — so it answers only "is the server reachable right now?". Any transport
// error or non-2xx status is treated as "not healthy". It runs on the adapter's
// own transport (so proxy/TLS configuration applies) under a short timeout of
// its own, so a probe can't block on the long prefill budget the streaming
// client allows — see healthClient.
func (a *Adapter) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/api/version", nil)
	if err != nil {
		return false
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
	resp, err := provider.HealthClient(a.client).Do(req)
	if err != nil {
		return false
	}
	provider.DrainAndClose(resp)
	return resp.StatusCode == http.StatusOK
}

// WithKeepAlive sets how long Ollama keeps the model loaded after this
// request (e.g. "10m", "-1" to pin forever, "0" to unload immediately).
// Empty (this adapter's own default) omits the field, leaving Ollama's own
// default (5m) in effect. In practice callers never get that: config
// (provider.keep_alive) is threaded through providerfactory.buildOne, which
// substitutes a bounded resident default of 30m
// (providerfactory.defaultOllamaKeepAlive) when the key is unset, and passes
// any explicit value — including "-1" or "0" — through unchanged. The policy
// deliberately lives in providerfactory, not here, so the adapter stays a
// faithful transport for whatever it is handed.
func WithKeepAlive(v string) Option {
	return func(a *Adapter) { a.keepAlive = v }
}

// WithResponseHeaderTimeout overrides how long the streamed request waits for
// response headers (provider.response_header_timeout, P35.5). Ollama
// withholds the response header until prompt-eval (prefill) finishes, so a
// large local context can legitimately need longer than the default. <= 0
// falls back to sse.DefaultResponseHeaderTimeout.
//
// It composes onto whatever client the adapter already has rather than
// replacing it (P61.5), so it commutes with any other option that touches the
// client and option order carries no meaning.
func WithResponseHeaderTimeout(d time.Duration) Option {
	return func(a *Adapter) { a.client = sse.ApplyResponseHeaderTimeout(a.client, d) }
}

// WithStreamIdleTimeout overrides how long the adapter waits between two
// streamed chunks before giving up on the response
// (provider.stream_idle_timeout, P59.2). 0 selects
// sse.DefaultStreamIdleTimeout; pass a negative duration to disable the bound.
// The anthropic and openai adapters carry an identical option (P61.1).
func WithStreamIdleTimeout(d time.Duration) Option {
	return func(a *Adapter) { a.streamIdleTimeout = d }
}

// resolveStreamIdleTimeout maps the configured value onto the bound consume
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

// WithLogger sets the logger used to warn about degraded behavior (e.g. a
// think-rejected request retried without it — P38.5). Defaults to
// slog.Default() when unset.
func WithLogger(l *slog.Logger) Option {
	return func(a *Adapter) { a.logger = l }
}

// New constructs a native Ollama adapter.
func New(opts ...Option) *Adapter {
	a := &Adapter{baseURL: defaultBaseURL, client: sse.NewStreamingClient(0)}
	for _, o := range opts {
		o(a)
	}
	if a.logger == nil {
		a.logger = slog.Default()
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
	// Seed is a pointer and NOT omitempty-elided by value: seed 0 is a valid
	// pin, and the zero value is exactly what a caller asking for determinism
	// is most likely to write.
	Seed *int `json:"seed,omitempty"`
}

type wireRequest struct {
	Model     string        `json:"model"`
	Messages  []wireMessage `json:"messages"`
	Tools     []wireTool    `json:"tools,omitempty"`
	Stream    bool          `json:"stream"`
	Think     *bool         `json:"think,omitempty"`
	KeepAlive string        `json:"keep_alive,omitempty"`
	Options   *wireOptions  `json:"options,omitempty"`
	// Format carries a JSON Schema for Ollama's structured outputs (P59.8).
	// Ollama compiles it to a llama.cpp grammar and constrains sampling to it,
	// which is a decode-time guarantee rather than an instruction the model may
	// ignore. Omitted when empty, which is every request but the one the schema
	// output guard constrains.
	Format json.RawMessage `json:"format,omitempty"`
}

// translate converts harness messages to native chat-message wire format.
//
// The native adapter mints tool-use IDs from a per-request counter (see
// consume), so the same ID (e.g. "tu_0") recurs across turns naming whatever
// tool happened to be called first each time. Resolving a ToolResultBlock's
// name from a map built over the *entire* history (last write wins) would
// therefore mislabel every earlier turn's result once a later turn reuses its
// ID with a different tool. Walking messages in order and updating the ID->
// name map as ToolUseBlocks appear resolves each result against the nearest
// *preceding* use instead, which is correct regardless of ID reuse and is
// stable turn-over-turn — required for Ollama's prefix cache to survive
// mixed-tool runs (P35.9).
//
// dropProse withholds an assistant turn's narration when that same turn also
// carries tool calls, for the models whose chat template would otherwise drop
// the *calls* instead — see ollamainfo.TemplateDropsToolCalls for the defect
// and the measurement. Withholding is the lesser loss by a wide margin: the
// narration is commentary, the call carries the arguments the rest of the
// history refers back to. It is off by default and applies only to turns that
// have both, so a model with a correct template pays nothing — no extra
// tokens and no prefix-cache churn from a changed encoding.
//
// Splitting the turn in two (prose message, then call message) was measured as
// an alternative and does not work: Ollama coalesces adjacent same-role
// messages before templating, so the pair arrives at the template as the same
// content-plus-calls message and is dropped exactly as before (0/3 on
// qwen3:14b-32k, unchanged from the unsplit case).
func translate(system string, msgs []provider.Message, dropProse bool) []wireMessage {
	names := make(map[string]string)
	args := make(map[string]json.RawMessage)
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
					names[v.ID] = v.Name
					callArgs := v.Input
					if len(bytes.TrimSpace(callArgs)) == 0 {
						callArgs = json.RawMessage("{}")
					}
					args[v.ID] = callArgs
					wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
						Function: wireToolCallFunc{Name: v.Name, Arguments: callArgs},
					})
				}
			}
			if dropProse && len(wm.ToolCalls) > 0 {
				text = ""
			}
			wm.Content = text
			if wm.Content == "" && len(wm.ToolCalls) == 0 {
				continue // skip empty assistant turns (e.g. model returned nothing)
			}
			out = append(out, wm)
		case provider.RoleUser:
			var text string
			var images []string
			echo := ambiguousRound(m, names)
			for _, b := range m.Content {
				switch v := b.(type) {
				case provider.TextBlock:
					text += v.Text
				case provider.ImageBlock:
					images = append(images, v.Data)
				case provider.ToolResultBlock:
					content := v.Content
					if echo {
						content = toolResultEcho(names[v.ToolUseID], args[v.ToolUseID]) + "\n" + content
					}
					out = append(out, wireMessage{Role: "tool", Content: content, ToolName: names[v.ToolUseID]})
				}
			}
			if text != "" || len(images) > 0 {
				out = append(out, wireMessage{Role: "user", Content: text, Images: images})
			}
		}
	}
	return out
}

// templateDropsToolCalls reports whether model's chat template discards an
// assistant turn's tool calls when that turn also carries prose, resolving in
// the same order as thinkIsRejected: in-memory cache, then the persisted
// record, then a live read of the template. A server that cannot answer is
// cached as "fine" for the process lifetime but never persisted, so the
// question is re-asked after a restart rather than being answered wrongly
// forever from one unreachable moment.
func (a *Adapter) templateDropsToolCalls(ctx context.Context, model string) bool {
	if model == "" {
		return false
	}
	if v, ok := a.dropsToolCalls.Load(model); ok {
		return v.(bool)
	}
	if a.caps != nil {
		if drops, known := a.caps.TemplateDropsToolCalls(model); known {
			a.dropsToolCalls.Store(model, drops)
			if drops {
				a.warnTemplateDropsToolCalls(model)
			}
			return drops
		}
	}
	if a.detectTemplate == nil {
		return false
	}
	drops, ok := a.detectTemplate(ctx, a.baseURL, model)
	if !ok {
		a.dropsToolCalls.Store(model, false)
		return false
	}
	a.dropsToolCalls.Store(model, drops)
	if a.caps != nil {
		a.caps.SetTemplateDropsToolCalls(model, drops)
	}
	if drops {
		a.warnTemplateDropsToolCalls(model)
	}
	return drops
}

// warnTemplateDropsToolCalls logs the mitigation once per model. It is a
// warning rather than a silent fix because the real repair is on the model
// side — rebuilding it with a template whose assistant branch renders content
// and tool calls in sequence rather than as `if`/`else if` — and that repair
// keeps the narration this mitigation has to discard.
func (a *Adapter) warnTemplateDropsToolCalls(model string) {
	if _, already := a.warnedTemplate.LoadOrStore(model, true); already {
		return
	}
	if a.logger == nil {
		return
	}
	a.logger.Warn("ollama: model's chat template drops tool calls from an assistant turn that also has text; withholding that text so the call survives in history",
		"model", model,
		"fix", "rebuild the model with an assistant branch that renders .Content and .ToolCalls in sequence, not as if/else-if")
}

// ambiguousRound reports whether a tool-results message carries two or more
// results for the *same* tool — the one case where Ollama's native correlation
// cannot tell them apart (P52.16).
//
// Native tool calls carry no ID (see wireToolCall), so translate emits every
// result as role:"tool" keyed only on tool_name. Three parallel read_file calls
// — which the engine explicitly permits, since read-capability tools run
// concurrently in runTools — therefore produce three wire messages identical in
// their correlation metadata, leaving position as the only signal. Rounds that
// call each tool once are unambiguous and are left byte-for-byte as they were,
// so the common case pays nothing: no extra tokens, and no prefix-cache churn
// from a changed encoding.
func ambiguousRound(m provider.Message, names map[string]string) bool {
	var seen map[string]bool
	for _, b := range m.Content {
		v, ok := b.(provider.ToolResultBlock)
		if !ok {
			continue
		}
		name := names[v.ToolUseID]
		if seen[name] {
			return true
		}
		if seen == nil {
			seen = make(map[string]bool, len(m.Content))
		}
		seen[name] = true
	}
	return false
}

// toolResultEcho renders the compact echo of an originating call that P52.16
// prefixes onto an ambiguous round's results — "[read_file path=internal/x.go]"
// — carrying the association in content where the protocol cannot carry it in
// metadata.
//
// Measured on Ollama 0.30.10 with a 3-parallel-read_file attribution task, paired
// trials, results graded on naming the file a fact came from:
//
//	qwen2.5-coder:1.5b   32/40 bare -> 38/40 echoed   (+15pp)
//	qwen3:14b             9/10 bare -> 10/10 echoed
//	gemma4:12b           20/20 bare -> 20/20 echoed
//
// So the conflation is real on a small model and the echo never hurt a capable
// one, which was the stated risk of shipping it.
//
// Keys are sorted and values truncated so the rendering is deterministic and
// bounded: an echo that varied run-to-run would break the very prefix cache
// translate's ordering rationale above exists to protect.
func toolResultEcho(name string, rawArgs json.RawMessage) string {
	var b strings.Builder
	b.WriteByte('[')
	if name == "" {
		b.WriteString("tool")
	} else {
		b.WriteString(name)
	}
	var fields map[string]any
	if err := json.Unmarshal(rawArgs, &fields); err == nil && len(fields) > 0 {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v, ok := scalarArg(fields[k])
			if !ok {
				continue // objects/arrays add bulk without disambiguating
			}
			b.WriteByte(' ')
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
		}
	}
	b.WriteByte(']')
	return b.String()
}

// maxEchoValueLen bounds one echoed argument value. A path or a query is what
// disambiguates a call; a 4KB inline file body is not, and echoing it would
// duplicate the payload the result already carries.
const maxEchoValueLen = 96

func scalarArg(v any) (string, bool) {
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case bool:
		s = strconv.FormatBool(t)
	case float64:
		s = strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return "", false
	}
	if len(s) > maxEchoValueLen {
		s = s[:maxEchoValueLen] + "..."
	}
	return s, true
}

func translateTools(tools []provider.ToolSchema) []wireTool {
	return provider.TranslateTools(tools, func(name, description string, parameters json.RawMessage) wireTool {
		var wt wireTool
		wt.Type = "function"
		wt.Function.Name = name
		wt.Function.Description = description
		wt.Function.Parameters = parameters
		return wt
	})
}

// Stream implements provider.Adapter.
func (a *Adapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	// P52.5: once a model has proven it rejects `think`, skip the doomed first
	// attempt for every later request naming that model. Without the latch the
	// adapter re-sent `think`, took another 400, and warned again on every turn
	// of the session.
	think := a.think
	if think != nil {
		if a.thinkIsRejected(req.Model) {
			think = nil
		}
	}

	resp, err := a.doChat(ctx, req, think)
	if err != nil {
		// P38.5: some models (e.g. mythos-sec:24b) 400 the instant `think` is
		// sent at all — "does not support thinking" — rather than accepting
		// and ignoring it. That aborts the run with a raw provider error and
		// zero tool calls, and nothing tells the user why. Retry once with
		// `think` omitted entirely and warn, rather than surfacing the raw
		// 400 — only when we actually sent a non-nil think value, so this
		// never masks an unrelated 400 or loops on a second failure.
		if think != nil && isThinkRejected(err) {
			rejection := err
			var retried *http.Response
			retried, err = a.doChat(ctx, req, nil)
			if err == nil {
				resp = retried
				// Latch only once the retry has *proven* think-omitted works
				// for this model, and warn exactly once per model — the log
				// line is a one-time capability finding, not a per-turn event.
				if _, already := a.thinkRejected.LoadOrStore(req.Model, true); !already {
					a.logger.Warn("ollama: model rejected the think parameter; retried without it and will omit it for this model from now on",
						"model", req.Model, "error", rejection)
					// P53.5: persist it too, so "from now on" means past this
					// process rather than only until the next restart.
					if a.caps != nil {
						a.caps.SetThinkRejected(req.Model, true)
					}
				}
			} else {
				// P61.5: the retry's failure used to overwrite `err` outright,
				// so a caller facing two failures saw only the second and lost
				// the "does not support thinking" signal that explains why a
				// second request was sent at all. Join them so both survive.
				//
				// The order is load-bearing: errors.As/Is walk a joined error
				// in order and stop at the first match, so the retry's error —
				// the operative failure — must come first. Putting the rejection
				// first would hand every downstream classifier (retryable,
				// IsBackendUnavailableError, IsContextOverflowError) the stale
				// terminal 400 instead of the failure that actually ended the
				// request, turning e.g. a retryable 503 or a dead backend into an
				// unretryable, unrecoverable one.
				err = errors.Join(err, rejection)
			}
		}
		if err != nil {
			return nil, err
		}
	}

	out := make(chan provider.Event)
	go sse.Run(ctx, resp.Body, out, sse.Options{
		Provider:        "ollama",
		IdleTimeout:     a.resolveStreamIdleTimeout(),
		MissingTerminal: missingCompletionChunk,
	}, newChunkDecoder())
	return out, nil
}

// thinkIsRejected reports whether `think` must be omitted for model: the
// in-memory latch first, then — once per model — the persisted/declared record
// (P53.5).
//
// The persisted answer is folded into the in-memory map only when it says
// "rejected". A store that says "not rejected" (a user declaring `think: true`)
// is honored by simply not latching, which leaves the live discovery path
// intact: if the model really does reject it, the retry-and-latch below fires
// as it always did. That asymmetry is what keeps a wrong persisted value from
// wedging anything — the worst it can cost is one re-discovered 400.
func (a *Adapter) thinkIsRejected(model string) bool {
	if _, latched := a.thinkRejected.Load(model); latched {
		return true
	}
	if a.caps == nil || model == "" {
		return false
	}
	if _, seen := a.capsLoaded.LoadOrStore(model, true); seen {
		return false
	}
	rejected, known := a.caps.ThinkRejected(model)
	if known && rejected {
		a.thinkRejected.Store(model, true)
		return true
	}
	return false
}

// isThinkRejected reports whether err is the HTTP 400 Ollama returns when a
// model doesn't support the `think` parameter at all (as opposed to any
// other 400, e.g. a malformed request, which must not trigger the retry).
func isThinkRejected(err error) bool {
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusBadRequest &&
		strings.Contains(apiErr.Message, "does not support thinking")
}

// clampNumPredict bounds the requested completion length by the room actually
// left in the served window (P59.1). The shared arithmetic — the same
// script-aware estimate the engine compacts against, the 5% margin, the
// tokenest.MinCompletionTokens floor, one-directionality — lives in
// tokenest.ClampCompletionTokens next to Messages, since it is the same
// arithmetic openai.clampMaxTokens applies once its own gate (a positively
// identified Ollama backend) is open. This adapter's gate is unconditional:
// on Ollama num_ctx is *always* one budget covering prompt and completion, so
// unlike the OpenAI-compat adapter there is no backend-identity check here —
// see tokenest.ClampCompletionTokens for what happens without a clamp at all.
func (a *Adapter) clampNumPredict(maxTokens, numCtx int, system string, msgs []provider.Message) int {
	sent := tokenest.ClampCompletionTokens(maxTokens, numCtx, system, msgs)
	if sent == maxTokens {
		return sent
	}
	// Debug, not Warn: this fires per request on a misconfigured pair, and the
	// place to tell a user about the misconfiguration once is `aegis doctor`.
	a.logger.Debug("ollama: clamped num_predict to the context window's remaining headroom",
		"requested", maxTokens, "sent", sent, "num_ctx", numCtx)
	return sent
}

// doChat sends one /api/chat request with the given think override (nil
// omits the field) and returns the raw response on success. The caller owns
// closing resp.Body.
func (a *Adapter) doChat(ctx context.Context, req provider.Request, think *bool) (*http.Response, error) {
	wr := wireRequest{
		Model:     req.Model,
		Messages:  translate(req.System, req.Messages, a.templateDropsToolCalls(ctx, req.Model)),
		Tools:     translateTools(req.Tools),
		Stream:    true,
		Think:     think,
		KeepAlive: a.keepAlive,
		Format:    req.Format,
	}
	var opts wireOptions
	var hasOpts bool
	numCtx := a.resolveNumCtx(req.NumCtx)
	if numCtx > 0 {
		opts.NumCtx = numCtx
		hasOpts = true
	}
	if req.MaxTokens > 0 {
		opts.NumPredict = a.clampNumPredict(req.MaxTokens, numCtx, req.System, req.Messages)
		hasOpts = true
	}
	if req.Temperature != nil {
		opts.Temperature = req.Temperature
		hasOpts = true
	}
	if req.Seed != nil {
		opts.Seed = req.Seed
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
		// P35.6: Ollama withholds the response header until prefill finishes,
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
	return resp, nil
}

// errorMessage extracts a human-readable message from a chunk's "error"
// field. Ollama's native API spells it as a bare string
// ({"error":"model runner has unexpectedly stopped"}); the object spelling
// ({"error":{"message":"..."}}) is tolerated too in case a future version or
// a proxy in front of Ollama changes shape. Anything else — absent, null, or
// neither — returns "" so the caller treats the chunk as ordinary.
func errorMessage(raw json.RawMessage) string {
	// P35.12: try common alternate string fields Ollama/proxies use for the
	// message ("error", "detail") before giving up on the object shape and
	// falling back to a compacted single-line rendering — a present error
	// envelope must never be swallowed into "".
	return provider.ErrorMessage(raw, "error", "detail")
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
	Done               bool            `json:"done"`
	DoneReason         string          `json:"done_reason"`
	PromptEvalCount    int             `json:"prompt_eval_count"`
	EvalCount          int             `json:"eval_count"`
	LoadDuration       int64           `json:"load_duration"`        // nanoseconds
	PromptEvalDuration int64           `json:"prompt_eval_duration"` // nanoseconds; P35.7 prefill-cost diagnostic
	Error              json.RawMessage `json:"error"`
}

// missingCompletionChunk is reported when the NDJSON stream ends cleanly
// without a done:true chunk (P59.3) — see sse.Run, which enforces it.
const missingCompletionChunk = "read stream: the response ended without a completion chunk — " +
	"the generation was cut off mid-stream (the server closed the connection or the model runner exited)"

// chunkDecoder translates the native /api/chat NDJSON stream into provider
// events. It owns only the per-chunk decode (P61.6): the read loop, the idle
// watchdog (P59.2), the done:true requirement (P59.3), scanner-error
// classification (P50.1/P35.12) and the final EventDone all live in sse.Run,
// shared with the two SSE adapters.
type chunkDecoder struct {
	usage     *provider.Usage
	stop      provider.StopReason
	toolIndex int
	sawLength bool // a chunk reported done_reason "length" (context ceiling hit)
}

func newChunkDecoder() *chunkDecoder {
	return &chunkDecoder{usage: &provider.Usage{}, stop: provider.StopEndTurn}
}

// Finish reports the stop reason and usage accumulated across the stream.
// Native tool calls arrive whole, so there is nothing buffered to flush.
func (d *chunkDecoder) Finish(func(provider.Event) bool) (provider.StopReason, *provider.Usage, bool) {
	return d.stop, d.usage, true
}

func (d *chunkDecoder) Line(raw string, emit func(provider.Event) bool) sse.Status {
	usage := d.usage
	line := strings.TrimSpace(raw)
	if line == "" {
		return sse.StatusContinue
	}
	var chunk wireChunk
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return sse.StatusContinue
	}
	if chunk.DoneReason == "length" {
		d.sawLength = true
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
		if d.sawLength || provider.IsTruncatedToolCallError(msg) {
			emit(provider.Event{Type: provider.EventError, Err: provider.NewContextTruncationError("ollama", msg)})
			return sse.StatusAbort
		}
		// P33.16: classify the {"error":...} envelope so a transient failure
		// (worker crash, model-load failure, OOM) carries a retryable verdict
		// while a deterministic one (context overflow, invalid request) stays
		// terminal — retrying the latter only burns another prompt-eval.
		emit(provider.Event{Type: provider.EventError, Err: provider.NewStreamError("ollama", msg)})
		return sse.StatusAbort
	}

	if chunk.Message.Thinking != "" {
		if !emit(provider.Event{Type: provider.EventThinkingDelta, Text: chunk.Message.Thinking}) {
			return sse.StatusAbort
		}
	}
	if chunk.Message.Content != "" {
		if !emit(provider.Event{Type: provider.EventTextDelta, Text: chunk.Message.Content}) {
			return sse.StatusAbort
		}
	}
	for _, tc := range chunk.Message.ToolCalls {
		args := tc.Function.Arguments
		if len(bytes.TrimSpace(args)) == 0 {
			args = json.RawMessage("{}")
		}
		id := fmt.Sprintf("tu_%d", d.toolIndex)
		d.toolIndex++
		// Native tool calls arrive whole (no incremental-argument
		// streaming), so the start and fully-assembled events fire back
		// to back — still worth emitting both so a consumer keyed on
		// EventToolUseStart (P33.3's provisional tool card) behaves
		// identically to the openai-adapter path.
		if !emit(provider.Event{Type: provider.EventToolUseStart, ToolUse: &provider.ToolUseBlock{
			ID: id, Name: tc.Function.Name,
		}}) {
			return sse.StatusAbort
		}
		d.stop = provider.StopToolUse
		if !emit(provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: id, Name: tc.Function.Name, Input: args,
		}}) {
			return sse.StatusAbort
		}
	}

	if chunk.Done {
		// P35.13: prompt_eval_count is the FULL prompt/context token count
		// this turn, not a cache-hit delta. Live-verified on Ollama 0.30.10
		// (P35.10's "a P35.7 live run saw 37 after turn 1's 3944" was a
		// misread: 3981-3944=37 is the growth in the count, but the field
		// itself reported the full 3981): sending an identical prompt twice
		// returns the same full prompt_eval_count both times while
		// prompt_eval_duration collapses (84ms->24ms), and a warm Aegis turn
		// reported prompt_eval_count=7195 in 86ms — 7195 real prefill tokens
		// in 86ms is impossible, so the prefix was a cache hit yet the full
		// count was still reported. So on this Ollama, prompt_eval_duration
		// (not the count) is the only cache-hit signal; the count tracks
		// context size. Mapped straight through as usage.InputTokens. Older
		// Ollama versions may have reported deltas, so this stays
		// version-dependent — compaction must keep using an estimate (the
		// engine's estimateTokens / conv.estimatedTokens) rather than trust
		// InputTokens for context size across backends. See the
		// PromptEvalDurationMS comment and the InputTokens doc in
		// internal/provider/provider.go.
		usage.InputTokens = chunk.PromptEvalCount
		usage.OutputTokens = chunk.EvalCount
		usage.LoadDurationMS = chunk.LoadDuration / 1e6
		usage.PromptEvalDurationMS = chunk.PromptEvalDuration / 1e6
		if d.stop != provider.StopToolUse && chunk.DoneReason == "length" {
			d.stop = provider.StopMaxTokens
		}
		// P59.3: /api/chat always terminates with done:true, so this is
		// the turn really finishing. sse.Run keeps reading — a stream that
		// carried on past it is still well-formed — but the requirement it
		// enforces is now satisfied.
		return sse.StatusTerminal
	}
	return sse.StatusContinue
}
