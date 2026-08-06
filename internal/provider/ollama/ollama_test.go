package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/sse"
)

func TestTranslateToolResultUsesName(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "id1", Name: "search", Input: json.RawMessage(`{"q":"cats"}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "id1", Content: "3 results"},
		}},
	}
	wire := translate("be terse", msgs)
	if len(wire) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(wire), wire)
	}
	if wire[0].Role != "system" || wire[0].Content != "be terse" {
		t.Errorf("system message wrong: %+v", wire[0])
	}
	if wire[1].Role != "assistant" || len(wire[1].ToolCalls) != 1 || wire[1].ToolCalls[0].Function.Name != "search" {
		t.Errorf("assistant tool call wrong: %+v", wire[1])
	}
	if wire[2].Role != "tool" || wire[2].ToolName != "search" || wire[2].Content != "3 results" {
		t.Errorf("tool result wrong: %+v", wire[2])
	}
}

// TestTranslateReusedToolIDsResolvePositionally guards P35.9: the native
// adapter mints tool-use IDs from a per-request counter, so "tu_0" recurs
// across turns naming whatever tool was called first each time. A map built
// over the whole history (last write wins) would mislabel turn 1's result
// once turn 2 reuses "tu_0" for a different tool, and that mutated label
// would also change the serialized prefix of turn 1 between requests,
// defeating Ollama's prefix cache. translate must resolve each result
// against the nearest *preceding* use instead.
func TestTranslateReusedToolIDsResolvePositionally(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_0", Name: "read_file", Input: json.RawMessage(`{}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "tu_0", Content: "file contents"},
		}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_0", Name: "run_shell", Input: json.RawMessage(`{}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "tu_0", Content: "shell output"},
		}},
	}

	wire := translate("", msgs)
	if len(wire) != 4 {
		t.Fatalf("got %d messages, want 4: %+v", len(wire), wire)
	}
	if wire[1].Role != "tool" || wire[1].ToolName != "read_file" || wire[1].Content != "file contents" {
		t.Errorf("turn 1 result mislabelled: %+v", wire[1])
	}
	if wire[3].Role != "tool" || wire[3].ToolName != "run_shell" || wire[3].Content != "shell output" {
		t.Errorf("turn 2 result mislabelled: %+v", wire[3])
	}

	// Byte-stability: serializing turn 1's prefix must be unchanged by
	// appending turn 2 — the property Ollama's prefix cache depends on.
	prefixOnly := translate("", msgs[:2])
	full := translate("", msgs)
	b1, err := json.Marshal(prefixOnly)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := json.Marshal(full[:2])
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Errorf("turn 1 prefix mutated by appending turn 2:\nbefore: %s\nafter:  %s", b1, b2)
	}
}

func TestTranslateImage(t *testing.T) {
	msgs := []provider.Message{{
		Role: provider.RoleUser,
		Content: []provider.Block{
			provider.TextBlock{Text: "describe"},
			provider.ImageBlock{MediaType: "image/jpeg", Data: "aGk="},
		},
	}}
	wire := translate("", msgs)
	if len(wire) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(wire), wire)
	}
	if wire[0].Content != "describe" || len(wire[0].Images) != 1 || wire[0].Images[0] != "aGk=" {
		t.Errorf("unexpected user message: %+v", wire[0])
	}
}

const sampleStream = `{"message":{"role":"assistant","content":"Hello "},"done":false}
{"message":{"role":"assistant","content":"there"},"done":false}
{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"search","arguments":{"q":"cats"}}}]},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":11,"eval_count":7,"load_duration":8200000000,"prompt_eval_duration":150000000}
`

func TestStreamParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(sampleStream))
	}))
	defer srv.Close()

	a := New(WithBaseURL(srv.URL))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:     "llama3.2",
		MaxTokens: 50,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var tu *provider.ToolUseBlock
	var startSeen bool
	var done *provider.Event
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			text += ev.Text
		case provider.EventToolUseStart:
			startSeen = true
		case provider.EventToolUse:
			tu = ev.ToolUse
		case provider.EventDone:
			e := ev
			done = &e
		case provider.EventError:
			t.Fatalf("error event: %v", ev.Err)
		}
	}

	if text != "Hello there" {
		t.Errorf("text = %q", text)
	}
	if !startSeen {
		t.Errorf("expected EventToolUseStart before EventToolUse")
	}
	if tu == nil || tu.Name != "search" || tu.ID == "" || string(tu.Input) != `{"q":"cats"}` {
		t.Errorf("tool use assembled wrong: %+v", tu)
	}
	if done == nil || done.Stop != provider.StopToolUse {
		t.Errorf("stop reason wrong: %+v", done)
	}
	if done.Usage == nil || done.Usage.InputTokens != 11 || done.Usage.OutputTokens != 7 {
		t.Errorf("usage = %+v", done.Usage)
	}
	if done.Usage.IsEstimated {
		t.Errorf("usage should not be marked estimated: real prompt/eval counts were reported")
	}
	if done.Usage.LoadDurationMS != 8200 {
		t.Errorf("load duration = %d ms, want 8200", done.Usage.LoadDurationMS)
	}
	if done.Usage.PromptEvalDurationMS != 150 {
		t.Errorf("prompt eval duration = %d ms, want 150 (P35.7 prefill diagnostic)", done.Usage.PromptEvalDurationMS)
	}
}

const errorStream = `{"error":"model runner has unexpectedly stopped"}
`

func TestStreamMidStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(errorStream))
	}))
	defer srv.Close()

	a := New(WithBaseURL(srv.URL))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model: "llama3.2",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotErr error
	for ev := range stream {
		if ev.Type == provider.EventError {
			gotErr = ev.Err
		}
	}
	if gotErr == nil {
		t.Fatal("expected an error event")
	}
}

// streamError runs body through the adapter and returns the last EventError's
// error (nil if none), for the mid-stream error-classification tests.
func streamError(t *testing.T, body string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	stream, err := New(WithBaseURL(srv.URL)).Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotErr error
	for ev := range stream {
		if ev.Type == provider.EventError {
			gotErr = ev.Err
		}
	}
	return gotErr
}

// TestStreamContextTruncationVsMalformed is the P35.2 guard for the native
// path: a tool call cut off at the context ceiling arrives as the server's
// opaque "invalid tool call arguments ... unexpected end of JSON input" (the
// adapter never parses tool args itself), and must surface the actionable,
// discoverable context-limit error — while a genuinely malformed call, and an
// unrelated failure, still surface verbatim.
func TestStreamContextTruncationVsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name          string
		body          string
		wantTruncated bool
	}{
		{
			// The exact live shape from the roadmap: no done_reason on the
			// error line, so the message shape is the only signal.
			name:          "truncated tool call message shape",
			body:          `{"error":"llama-server returned invalid tool call arguments for \"read_file\": unexpected end of JSON input"}` + "\n",
			wantTruncated: true,
		},
		{
			// A prior chunk reported done_reason "length" before the error line.
			name: "prior length signal",
			body: `{"message":{"role":"assistant","content":"partial"},"done":true,"done_reason":"length"}` + "\n" +
				`{"error":"llama-server returned invalid tool call arguments for \"read_file\": unexpected end of JSON input"}` + "\n",
			wantTruncated: true,
		},
		{
			// Genuinely malformed model output — a syntax error, not a cut-off.
			name:          "malformed not truncated",
			body:          `{"error":"llama-server returned invalid tool call arguments for \"read_file\": invalid character 'x' looking for beginning of value"}` + "\n",
			wantTruncated: false,
		},
		{
			name:          "unrelated failure",
			body:          `{"error":"model runner has unexpectedly stopped"}` + "\n",
			wantTruncated: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := streamError(t, tc.body)
			if err == nil {
				t.Fatal("expected an error event")
			}
			gotTruncated := strings.Contains(err.Error(), "context limit") &&
				strings.Contains(err.Error(), "context_window")
			if gotTruncated != tc.wantTruncated {
				t.Errorf("truncation-error = %v, want %v; error was: %v", gotTruncated, tc.wantTruncated, err)
			}
			if tc.wantTruncated {
				var apiErr *provider.APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("error is not *provider.APIError: %T", err)
				}
				if apiErr.Retryable() {
					t.Errorf("a context-limit failure must be terminal, got retryable: %v", err)
				}
			}
		})
	}
}

// TestErrorMessage is the P35.12 guard for errorMessage: a present error
// envelope must never be swallowed into "", alternate string fields
// (message/error/detail) are honoured, and an object with none of those
// surfaces as a compacted single line rather than raw multi-line JSON.
func TestErrorMessage(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"empty", ``, ""},
		{"null", `null`, ""},
		{"plain string", `"model runner has unexpectedly stopped"`, "model runner has unexpectedly stopped"},
		{"object message", `{"message":"boom"}`, "boom"},
		{"object detail only", `{"detail":"boom detail"}`, "boom detail"},
		{"object error only", `{"error":"boom error"}`, "boom error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := errorMessage(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("errorMessage(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	// Object with none of the known string fields: must stay non-empty, be a
	// single compacted line (no raw newlines/indent), not the verbatim bytes.
	raw := "{\n  \"code\": 500,\n  \"nested\": {\n    \"x\": 1\n  }\n}"
	got := errorMessage(json.RawMessage(raw))
	if got == "" {
		t.Fatal("a present error envelope must never be swallowed into \"\"")
	}
	if strings.ContainsAny(got, "\n") {
		t.Errorf("compacted output still contains newlines: %q", got)
	}
	if got != `{"code":500,"nested":{"x":1}}` {
		t.Errorf("compacted output = %q, want compacted single line", got)
	}
}

// TestStreamOversizedLineActionable is the P35.12 guard: a single NDJSON line
// past the shared 4MiB scanner cap (sse.maxBufSize) trips bufio.ErrTooLong,
// and the native path must surface an actionable error naming the line-limit /
// tool-call-argument cause rather than the opaque "token too long".
func TestStreamOversizedLineActionable(t *testing.T) {
	// One NDJSON line well over 4MiB: a tool call whose argument payload is huge.
	var b strings.Builder
	b.WriteString(`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"x","arguments":{"blob":"`)
	b.WriteString(strings.Repeat("a", 5*1024*1024))
	b.WriteString(`"}}}]},"done":false}` + "\n")

	err := streamError(t, b.String())
	if err == nil {
		t.Fatal("expected an error event")
	}
	// The actionable message names the line limit and probable cause; the raw
	// wrapped "token too long" may still trail after %w, but must not be the
	// only thing surfaced.
	if !strings.Contains(err.Error(), "4MiB line limit") {
		t.Errorf("error is not the actionable line-limit message: %v", err)
	}
	if !strings.Contains(err.Error(), "tool-call argument") {
		t.Errorf("error does not name the probable cause: %v", err)
	}
}

func TestWithBaseURLStripsV1Suffix(t *testing.T) {
	a := New(WithBaseURL("http://localhost:11434/v1/"))
	if a.baseURL != "http://localhost:11434" {
		t.Errorf("baseURL = %q, want http://localhost:11434", a.baseURL)
	}
}

// TestRaiseContextWindow is the P47.5(b) guard for the Ollama adapter's runtime
// num_ctx escalation: RaiseContextWindow raises num_ctx only when the target
// exceeds the current value (monotonic — never shrinks), and the raised value is
// what the next Stream sends. This is what lets the phased drive claim more
// serving-context headroom after an overflow without rebuilding the adapter.
func TestRaiseContextWindow(t *testing.T) {
	a := New(WithNumCtx(65536))
	if a.RaiseContextWindow(32768) {
		t.Error("raising to a smaller window must be a no-op reporting false")
	}
	if a.numCtx != 65536 {
		t.Errorf("num_ctx must not shrink; got %d", a.numCtx)
	}
	if !a.RaiseContextWindow(131072) {
		t.Error("raising to a larger window must report true")
	}
	if a.numCtx != 131072 {
		t.Errorf("num_ctx = %d, want raised to 131072", a.numCtx)
	}

	// The raised value is what the next request actually sends.
	var gotBody wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()
	a2 := New(WithBaseURL(srv.URL), WithNumCtx(8192))
	a2.RaiseContextWindow(196608)
	stream, err := a2.Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	if gotBody.Options == nil || gotBody.Options.NumCtx != 196608 {
		t.Errorf("escalated num_ctx not sent; got %+v", gotBody.Options)
	}
}

// numCtxEcho stands up an /api/chat that records the options every request
// carried, for the per-request num_ctx tests below.
func numCtxEcho(t *testing.T) (*httptest.Server, func() *wireRequest) {
	t.Helper()
	var mu sync.Mutex
	var last wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		_ = json.NewDecoder(r.Body).Decode(&last)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() *wireRequest {
		mu.Lock()
		defer mu.Unlock()
		cp := last
		return &cp
	}
}

func drain(t *testing.T, a *Adapter, req provider.Request) {
	t.Helper()
	if req.Messages == nil {
		req.Messages = []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}}
	}
	stream, err := a.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
}

// TestStreamPerRequestNumCtx is the P52.4 guard: one shared daemon adapter
// serves several models, so the serving window has to travel on the request
// (which already carries the model) rather than sit in adapter state. A turn
// routed to the small model must not ask Ollama to allocate the primary
// model's KV cache for it.
func TestStreamPerRequestNumCtx(t *testing.T) {
	srv, last := numCtxEcho(t)
	a := New(WithBaseURL(srv.URL), WithNumCtx(65536)) // primary model's window

	drain(t, a, provider.Request{Model: "small:1b", NumCtx: 4096})
	if got := last(); got.Options == nil || got.Options.NumCtx != 4096 {
		t.Errorf("small-model num_ctx = %+v, want 4096 (not the primary's 65536)", got.Options)
	}

	// A request that says nothing still gets the adapter's configured window —
	// the pre-P52.4 behavior every non-server caller (CLI drive, sub-agents)
	// relies on.
	drain(t, a, provider.Request{Model: "primary:70b"})
	if got := last(); got.Options == nil || got.Options.NumCtx != 65536 {
		t.Errorf("fallback num_ctx = %+v, want the adapter's 65536", got.Options)
	}
}

// TestStreamEscalationOutranksPerRequestNumCtx documents the P52.4/P47.5b
// interaction: a RaiseContextWindow escalation is a response to an overflow
// that already happened, so it must act as a floor over the (older, smaller)
// window the caller computed before the run — otherwise escalating a
// daemon-shared adapter would be silently undone on the very next request.
func TestStreamEscalationOutranksPerRequestNumCtx(t *testing.T) {
	srv, last := numCtxEcho(t)
	a := New(WithBaseURL(srv.URL), WithNumCtx(8192))
	if !a.RaiseContextWindow(32768) {
		t.Fatal("escalation to a larger window should report true")
	}

	drain(t, a, provider.Request{Model: "primary:70b", NumCtx: 8192})
	if got := last(); got.Options == nil || got.Options.NumCtx != 32768 {
		t.Errorf("num_ctx = %+v, want the escalated 32768", got.Options)
	}
	// A request asking for more than the escalation still gets what it asked
	// for — the floor never shrinks a window either.
	drain(t, a, provider.Request{Model: "primary:70b", NumCtx: 65536})
	if got := last(); got.Options == nil || got.Options.NumCtx != 65536 {
		t.Errorf("num_ctx = %+v, want 65536", got.Options)
	}
}

func TestStreamSendsOptions(t *testing.T) {
	var gotBody wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	a := New(WithBaseURL(srv.URL), WithNumCtx(8192), WithKeepAlive("30m"))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:     "llama3.2",
		MaxTokens: 512,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}

	if gotBody.Options == nil {
		t.Fatal("expected options to be set")
	}
	if gotBody.Options.NumCtx != 8192 {
		t.Errorf("num_ctx = %d, want 8192", gotBody.Options.NumCtx)
	}
	if gotBody.Options.NumPredict != 512 {
		t.Errorf("num_predict = %d, want 512", gotBody.Options.NumPredict)
	}
	if gotBody.KeepAlive != "30m" {
		t.Errorf("keep_alive = %q, want %q", gotBody.KeepAlive, "30m")
	}
}

// TestStreamOmitsKeepAliveByDefault is the P33.10 guard: with no WithKeepAlive
// option the field is omitted entirely (never "-1"/pin-forever), so Ollama's
// own 5m default stays in effect.
func TestStreamOmitsKeepAliveByDefault(t *testing.T) {
	var rawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	a := New(WithBaseURL(srv.URL))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	if strings.Contains(string(rawBody), "keep_alive") {
		t.Errorf("keep_alive must be omitted when unset, got body: %s", rawBody)
	}
}

// TestWithResponseHeaderTimeout is the P35.5/P38.1 adapter-level regression:
// WithResponseHeaderTimeout must actually change the transport's configured
// ResponseHeaderTimeout, and an adapter built with no such option keeps
// sse.DefaultResponseHeaderTimeout (30m as of P38.1).
func TestWithResponseHeaderTimeout(t *testing.T) {
	def := New()
	tr, ok := def.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", def.client.Transport)
	}
	if tr.ResponseHeaderTimeout != 30*time.Minute {
		t.Errorf("default ResponseHeaderTimeout = %v, want 30m", tr.ResponseHeaderTimeout)
	}

	custom := New(WithResponseHeaderTimeout(20 * time.Minute))
	tr, ok = custom.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", custom.client.Transport)
	}
	if tr.ResponseHeaderTimeout != 20*time.Minute {
		t.Errorf("ResponseHeaderTimeout = %v, want 20m", tr.ResponseHeaderTimeout)
	}
}

// TestResponseHeaderTimeoutRewrapped is the P35.6 regression: when a server
// withholds its response header past the configured ResponseHeaderTimeout
// (Ollama does this until prefill finishes, per P35.5), the bare Go transport
// string "net/http: timeout awaiting response headers" must not reach the
// caller unrewrapped — it names no cause and no remedy. Stream must instead
// return provider.NewResponseHeaderTimeoutError's actionable, non-retryable
// error naming provider.response_header_timeout and context_window.
func TestResponseHeaderTimeoutRewrapped(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never write a header until the client has given up
	}))
	defer srv.Close()
	defer close(block)

	a := New(WithBaseURL(srv.URL), WithResponseHeaderTimeout(50*time.Millisecond))

	_, err := a.Stream(context.Background(), provider.Request{
		Model: "m",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "provider.response_header_timeout") || !strings.Contains(err.Error(), "context_window") {
		t.Errorf("error does not name the levers: %v", err)
	}
	if strings.Contains(err.Error(), "request failed") {
		t.Errorf("bare transport string leaked through unrewrapped: %v", err)
	}

	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *provider.APIError: %T", err)
	}
	if apiErr.Retryable() {
		t.Errorf("a response-header timeout must be terminal, got retryable: %v", err)
	}
}

// TestStreamRetriesWhenModelRejectsThink is the P38.5 regression: a model
// that 400s the instant `think` is sent at all (observed on
// supergoatscriptguy/mythos-sec:24b) must not abort the run with a raw
// provider error. Stream should retry once with `think` omitted and succeed.
func TestStreamRetriesWhenModelRejectsThink(t *testing.T) {
	var thinkFields []bool // whether "think" key was present, per request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &raw)
		_, hasThink := raw["think"]
		thinkFields = append(thinkFields, hasThink)
		if hasThink {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"\"mythos-sec:24b\" does not support thinking"}`))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	falseVal := false
	a := New(WithBaseURL(srv.URL), WithThink(&falseVal))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:    "mythos-sec:24b",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotText string
	for ev := range stream {
		if ev.Type == provider.EventTextDelta {
			gotText += ev.Text
		}
	}
	if gotText != "hi" {
		t.Errorf("gotText = %q, want %q (retry-without-think should have succeeded)", gotText, "hi")
	}
	if len(thinkFields) != 2 || !thinkFields[0] || thinkFields[1] {
		t.Fatalf("expected [think-present, think-absent] requests, got %v", thinkFields)
	}
}

// TestStreamLatchesThinkRejectionPerModel is the P52.5 regression: the P38.5
// retry was correct but stateless, so every subsequent turn re-sent `think`,
// re-took the 400, and re-warned — a wasted round trip and a duplicated log
// line on every turn of a session. After one proven retry the adapter must send
// no `think` at all for that model, and warn exactly once. The latch is keyed
// by model, not held per-adapter: a daemon adapter serves a mix of models, and
// one model's rejection must not strip `think` from a sibling that supports it.
func TestStreamLatchesThinkRejectionPerModel(t *testing.T) {
	type sent struct {
		model     string
		withThink bool
	}
	var mu sync.Mutex
	var reqs []sent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			Model string          `json:"model"`
			Think json.RawMessage `json:"think"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &raw)
		hasThink := len(raw.Think) > 0
		mu.Lock()
		reqs = append(reqs, sent{model: raw.Model, withThink: hasThink})
		mu.Unlock()
		// Only "rejector" refuses the parameter; "supporter" accepts it.
		if hasThink && raw.Model == "rejector" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"\"rejector\" does not support thinking"}`))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	var logBuf bytes.Buffer
	falseVal := false
	a := New(WithBaseURL(srv.URL), WithThink(&falseVal),
		WithLogger(slog.New(slog.NewTextHandler(&logBuf, nil))))

	run := func(model string) {
		t.Helper()
		stream, err := a.Stream(context.Background(), provider.Request{
			Model:    model,
			Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
		})
		if err != nil {
			t.Fatalf("Stream(%s): %v", model, err)
		}
		var got string
		for ev := range stream {
			if ev.Type == provider.EventTextDelta {
				got += ev.Text
			}
		}
		if got != "hi" {
			t.Fatalf("Stream(%s) text = %q, want %q", model, got, "hi")
		}
	}

	run("rejector") // 400 with think, then the successful retry without it
	run("rejector") // must skip the doomed first attempt entirely

	mu.Lock()
	after := append([]sent(nil), reqs...)
	mu.Unlock()
	want := []sent{{"rejector", true}, {"rejector", false}, {"rejector", false}}
	if len(after) != len(want) {
		t.Fatalf("got %d requests %v, want %d %v (second turn must not re-send think)", len(after), after, len(want), want)
	}
	for i := range want {
		if after[i] != want[i] {
			t.Fatalf("request %d = %+v, want %+v (full sequence %v)", i, after[i], want[i], after)
		}
	}

	if n := strings.Count(logBuf.String(), "rejected the think parameter"); n != 1 {
		t.Errorf("think-rejection warning fired %d times, want exactly 1; log:\n%s", n, logBuf.String())
	}

	// The latch is per-model: a second model must still get `think`.
	run("supporter")
	mu.Lock()
	last := reqs[len(reqs)-1]
	mu.Unlock()
	if last.model != "supporter" || !last.withThink {
		t.Errorf("second model's request = %+v, want think sent (the latch must not leak across models)", last)
	}
}

// fakeCapStore is a minimal in-memory CapabilityStore standing in for
// *modelcaps.Store, so this package's tests stay free of the store's file I/O.
type fakeCapStore struct {
	mu       sync.Mutex
	rejected map[string]bool
	writes   int
}

func newFakeCapStore() *fakeCapStore { return &fakeCapStore{rejected: map[string]bool{}} }

func (f *fakeCapStore) ThinkRejected(model string) (bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.rejected[model]
	return v, ok
}

func (f *fakeCapStore) SetThinkRejected(model string, rejected bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected[model] = rejected
	f.writes++
}

// TestThinkRejectionPersistsAcrossAdapters is the P53.5 regression: the P52.5
// latch was process-lifetime only, so every daemon restart re-sent `think` to a
// model already proven to reject it and re-paid the 400. With a capability
// store wired in, the second adapter — standing in for the next process — must
// send no `think` at all, and must not re-write what it already knows.
func TestThinkRejectionPersistsAcrossAdapters(t *testing.T) {
	var mu sync.Mutex
	var withThink []bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			Think json.RawMessage `json:"think"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &raw)
		has := len(raw.Think) > 0
		mu.Lock()
		withThink = append(withThink, has)
		mu.Unlock()
		if has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"\"rejector\" does not support thinking"}`))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	caps := newFakeCapStore()
	falseVal := false
	run := func(a *Adapter) {
		t.Helper()
		stream, err := a.Stream(context.Background(), provider.Request{
			Model:    "rejector",
			Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		for range stream {
		}
	}

	first := New(WithBaseURL(srv.URL), WithThink(&falseVal), WithCapabilityStore(caps))
	run(first)
	if rejected, known := caps.ThinkRejected("rejector"); !known || !rejected {
		t.Fatalf("rejection not persisted: rejected=%v known=%v", rejected, known)
	}

	// A fresh adapter with an empty in-memory latch: the store must carry the
	// verdict, so the doomed first attempt never happens.
	second := New(WithBaseURL(srv.URL), WithThink(&falseVal), WithCapabilityStore(caps))
	run(second)
	run(second)

	mu.Lock()
	got := append([]bool(nil), withThink...)
	mu.Unlock()
	want := []bool{true, false, false, false} // one discovery 400, then never again
	if len(got) != len(want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d sent think=%v, want %v (full sequence %v)", i, got[i], want[i], got)
		}
	}
	if caps.writes != 1 {
		t.Errorf("store written %d times, want 1 — the verdict is a one-time finding", caps.writes)
	}
}

// TestDeclaredThinkSupportOverridesStore covers the precedence rule that keeps a
// wrong persisted value recoverable: a store reporting "not rejected" (a user
// declaring the model does accept `think`) must leave live discovery in charge
// rather than latching, so the parameter is sent and the model gets to answer
// for itself.
func TestDeclaredThinkSupportOverridesStore(t *testing.T) {
	var mu sync.Mutex
	var sawThink bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			Think json.RawMessage `json:"think"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &raw)
		mu.Lock()
		sawThink = sawThink || len(raw.Think) > 0
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	caps := newFakeCapStore()
	caps.rejected["m"] = false // the shape a `think: true` declaration produces

	falseVal := false
	a := New(WithBaseURL(srv.URL), WithThink(&falseVal), WithCapabilityStore(caps))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:    "m",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawThink {
		t.Error("a store saying \"not rejected\" suppressed think; a wrong cached value would then be unrecoverable")
	}
}

// TestRaiseContextWindowConcurrentWithStream is the P52.6 regression, and is
// meaningful only under `go test -race`: RaiseContextWindow's write to numCtx
// and doChat's read of it must be synchronized. Before the fix this was an
// unguarded read/write pair, safe only because the sole caller was a
// single-session CLI process — an invariant that dies the moment the phased
// drive runs inside the daemon, where one adapter is shared by every concurrent
// session. Escalation stays monotonic, so whatever value a request observes is
// always a real (never-shrinking) window.
func TestRaiseContextWindowConcurrentWithStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got wireRequest
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.Options != nil && got.Options.NumCtx < 4096 {
			t.Errorf("observed a shrunken num_ctx %d; escalation must be monotonic", got.Options.NumCtx)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	a := New(WithBaseURL(srv.URL), WithNumCtx(4096))

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) { // writers: concurrent context-window escalations
			defer wg.Done()
			a.RaiseContextWindow(4096 + i*1024)
		}(i)
		wg.Add(1)
		go func() { // readers: concurrent Streams reading numCtx in doChat
			defer wg.Done()
			stream, err := a.Stream(context.Background(), provider.Request{
				Model:    "llama3.2",
				Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
			})
			if err != nil {
				t.Errorf("Stream: %v", err)
				return
			}
			for range stream {
			}
		}()
	}
	wg.Wait()

	if got := a.contextWindow(); got != 4096+(n-1)*1024 {
		t.Errorf("final num_ctx = %d, want %d (highest escalation wins)", got, 4096+(n-1)*1024)
	}
}

// TestStreamDoesNotRetryOtherBadRequests guards against over-matching: a 400
// unrelated to `think` support must surface as-is, not trigger a silent
// think-omitted retry that could mask a real request error.
func TestStreamDoesNotRetryOtherBadRequests(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid request"}`))
	}))
	defer srv.Close()

	falseVal := false
	a := New(WithBaseURL(srv.URL), WithThink(&falseVal))
	_, err := a.Stream(context.Background(), provider.Request{
		Model:    "m",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 request (no think-rejection retry), got %d", calls)
	}
}

// TestStreamThinkRetryFailureKeepsBothErrors is the P61.5 regression: when the
// think-omitted retry *also* fails, the retry's error used to overwrite the
// original 400 outright, so the "does not support thinking" signal — the thing
// that explains why a second request was sent at all — vanished. Both must
// survive.
//
// The joined order is asserted too, and it is not cosmetic: errors.As stops at
// the first match, so the retry's error has to come first or every downstream
// classifier (retryable, IsBackendUnavailableError) would read the stale
// terminal 400 and refuse to retry a plainly retryable failure.
func TestStreamThinkRetryFailureKeepsBothErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &raw)
		if _, hasThink := raw["think"]; hasThink {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"\"m\" does not support thinking"}`))
			return
		}
		// The retry fails too — a transient 503, which must stay retryable.
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"server busy"}`))
	}))
	defer srv.Close()

	falseVal := false
	a := New(WithBaseURL(srv.URL), WithThink(&falseVal))
	_, err := a.Stream(context.Background(), provider.Request{
		Model:    "m",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "does not support thinking") {
		t.Errorf("the original think rejection was lost: %v", err)
	}
	if !strings.Contains(err.Error(), "server busy") {
		t.Errorf("the retry's own failure was lost: %v", err)
	}

	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *provider.APIError: %T", err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("errors.As found status %d, want the retry's 503 first", apiErr.StatusCode)
	}
	if !apiErr.Retryable() {
		t.Error("a 503 from the retry must stay retryable through the join")
	}

	// The latch must not fire: nothing proved think-omitted works for this model.
	if _, latched := a.thinkRejected.Load("m"); latched {
		t.Error("think rejection latched on a retry that never succeeded")
	}
}

// TestHealthProbeUsesTheAdapterTransport is the P61.5 regression: the liveness
// probe ran on a package-level http.Client with its own default transport, so a
// user's proxy/TLS/dialer configuration did not reach the probe that gates P50.1
// recovery. It must share the adapter's transport — while keeping its own short
// timeout, since the streaming client deliberately has none (P59.2).
func TestHealthProbeUsesTheAdapterTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"x"}`))
	}))
	defer srv.Close()

	var probes int
	a := New(WithBaseURL(srv.URL))
	inner := a.client.Transport
	a.client = &http.Client{Transport: roundTripCounter{n: &probes, inner: inner}}

	if !a.Healthy(context.Background()) {
		t.Fatal("Healthy() = false against a live server")
	}
	if probes != 1 {
		t.Errorf("the probe went through the adapter's transport %d time(s), want 1", probes)
	}

	hc := a.healthClient()
	if hc.Transport != a.client.Transport {
		t.Error("probe client does not share the adapter's transport")
	}
	if hc.Timeout != healthProbeTimeout {
		t.Errorf("probe Client.Timeout = %v, want %v — it must not inherit the streaming client's unbounded one",
			hc.Timeout, healthProbeTimeout)
	}
	if a.client.Timeout != 0 {
		t.Errorf("streaming client's Timeout = %v, want 0 (P59.2)", a.client.Timeout)
	}
}

// roundTripCounter counts requests and delegates, standing in for whatever
// transport a user's proxy/TLS configuration produced.
type roundTripCounter struct {
	n     *int
	inner http.RoundTripper
}

func (c roundTripCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	*c.n++
	return c.inner.RoundTrip(r)
}

// TestHealthy is the P50.1 liveness-probe guard: a reachable server answering
// /api/version with 200 is healthy; a non-200, a wrong path, and an unreachable
// server are all "not healthy". It also confirms the probe uses GET /api/version
// and never a model-loading request, so it cannot itself perturb the backend.
func TestHealthy(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte(`{"version":"0.30.10"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	a := New(WithBaseURL(srv.URL))
	if !a.Healthy(context.Background()) {
		t.Fatal("Healthy() = false against a live server answering /api/version")
	}
	if gotMethod != http.MethodGet || gotPath != "/api/version" {
		t.Errorf("probe hit %s %s, want GET /api/version", gotMethod, gotPath)
	}

	// A server that 500s the version endpoint is not healthy.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if New(WithBaseURL(bad.URL)).Healthy(context.Background()) {
		t.Error("Healthy() = true against a server returning 500")
	}

	// An unreachable server (closed listener) is not healthy.
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()
	if New(WithBaseURL(downURL)).Healthy(context.Background()) {
		t.Error("Healthy() = true against an unreachable server")
	}
}

// TestHealthyImplementsCapability confirms the native adapter satisfies
// provider.HealthChecker and is reachable through the unwrapping helper, so the
// drive's CheckBackendHealth finds it through the retry decorator.
func TestHealthyImplementsCapability(t *testing.T) {
	var _ provider.HealthChecker = New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"x"}`))
	}))
	defer srv.Close()
	wrapped := provider.WithRetry(New(WithBaseURL(srv.URL)), provider.DefaultRetryPolicy(), nil)
	healthy, supported := provider.CheckBackendHealth(context.Background(), wrapped)
	if !supported {
		t.Fatal("CheckBackendHealth could not reach the Ollama adapter through the retry decorator")
	}
	if !healthy {
		t.Error("CheckBackendHealth = unhealthy against a live server")
	}
}

// TestStreamMidStreamConnectionDropIsBackendUnavailable is the P50.1 guard for
// the case that motivated the fix: the model server going away *while a response
// is streaming* (an `ollama serve` kill/crash mid-turn) produces a mid-stream
// read failure — connection reset / unexpected EOF. That must surface as a
// transport APIError so provider.IsBackendUnavailableError classifies it and the
// phased drive waits + resumes, rather than a bare unclassifiable error that
// aborts the drive. The server hijacks the connection, writes a partial chunked
// body, then closes uncleanly so the client read errors mid-stream.
func TestStreamMidStreamConnectionDropIsBackendUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server does not support hijacking")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		// Valid 200 header + one *partial* chunk (declares 26 bytes, sends fewer),
		// then abrupt close → the client's chunked reader fails with unexpected EOF.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/x-ndjson\r\nTransfer-Encoding: chunked\r\n\r\n")
		_, _ = buf.WriteString("1a\r\n{\"message\":{\"content\":\"hel")
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()

	stream, err := New(WithBaseURL(srv.URL)).Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotErr error
	for ev := range stream {
		if ev.Type == provider.EventError {
			gotErr = ev.Err
		}
	}
	if gotErr == nil {
		t.Fatal("expected a mid-stream error event")
	}
	if !provider.IsBackendUnavailableError(gotErr) {
		t.Errorf("mid-stream connection drop = %v, want it classified backend-unavailable (P50.1)", gotErr)
	}
}

// TestStreamWithoutDoneChunkIsAnError (P59.3): a body that closes cleanly
// mid-generation produces no read error, so before this fix the adapter emitted
// EventDone with zeroed usage and StopEndTurn — a truncated answer presented as
// a complete one. It must be classified as a transport failure instead, so the
// existing backend-unavailable resume path handles it.
func TestStreamWithoutDoneChunkIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// Well-formed content chunks, then a clean EOF — no done:true ever.
		_, _ = w.Write([]byte("{\"message\":{\"content\":\"partial \"}}\n"))
		_, _ = w.Write([]byte("{\"message\":{\"content\":\"answer\"}}\n"))
	}))
	defer srv.Close()

	stream, err := New(WithBaseURL(srv.URL)).Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotErr error
	sawDone := false
	for ev := range stream {
		switch ev.Type {
		case provider.EventError:
			gotErr = ev.Err
		case provider.EventDone:
			sawDone = true
		}
	}
	if sawDone {
		t.Error("emitted EventDone for a stream that never sent a completion chunk")
	}
	if gotErr == nil {
		t.Fatal("expected an error event for a stream truncated before done:true")
	}
	if !provider.IsBackendUnavailableError(gotErr) {
		t.Errorf("truncated stream = %v, want it classified backend-unavailable so the P50.1 resume path handles it", gotErr)
	}
}

// TestStreamDoneChunkStillCompletes guards the other side of P59.3: a normal
// stream that does end with done:true must still finish with EventDone and no
// error event.
func TestStreamDoneChunkStillCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"message\":{\"content\":\"hi\"}}\n"))
		_, _ = w.Write([]byte("{\"done\":true,\"done_reason\":\"stop\",\"prompt_eval_count\":11,\"eval_count\":2}\n"))
	}))
	defer srv.Close()

	stream, err := New(WithBaseURL(srv.URL)).Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var done *provider.Event
	for ev := range stream {
		if ev.Type == provider.EventError {
			t.Fatalf("unexpected error event: %v", ev.Err)
		}
		if ev.Type == provider.EventDone {
			e := ev
			done = &e
		}
	}
	if done == nil {
		t.Fatal("expected EventDone")
	}
	if done.Stop != provider.StopEndTurn {
		t.Errorf("stop = %q, want %q", done.Stop, provider.StopEndTurn)
	}
	if done.Usage == nil || done.Usage.InputTokens != 11 || done.Usage.OutputTokens != 2 {
		t.Errorf("usage = %+v, want 11/2", done.Usage)
	}
}

// TestStreamIdleTimeoutAbortsAStalledRunner (P59.2): headers arrive, one chunk
// streams, then the runner wedges and sends nothing more. ResponseHeaderTimeout
// no longer applies and the streaming client has no overall timeout, so before
// the idle bound this blocked forever. It must end as a transport error — the
// same classification a crashed server gets, so the existing resume path
// handles it — and the message must name the knob.
func TestStreamIdleTimeoutAbortsAStalledRunner(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"message\":{\"content\":\"thinking\"}}\n"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	stream, err := New(WithBaseURL(srv.URL), WithStreamIdleTimeout(100*time.Millisecond)).
		Stream(context.Background(), provider.Request{
			Model:    "llama3.2",
			Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
		})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	type outcome struct {
		err  error
		done bool
	}
	res := make(chan outcome, 1)
	go func() {
		var o outcome
		for ev := range stream {
			switch ev.Type {
			case provider.EventError:
				o.err = ev.Err
			case provider.EventDone:
				o.done = true
			}
		}
		res <- o
	}()

	select {
	case o := <-res:
		if o.done {
			t.Error("a stalled stream must not produce EventDone")
		}
		if o.err == nil {
			t.Fatal("expected an error event once the idle bound elapsed")
		}
		if !provider.IsBackendUnavailableError(o.err) {
			t.Errorf("stalled stream = %v, want it classified backend-unavailable so the resume path handles it", o.err)
		}
		if !strings.Contains(o.err.Error(), "stream_idle_timeout") {
			t.Errorf("error should name the knob that relaxes it, got %q", o.err.Error())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stream never ended — the idle bound did not fire")
	}
}

// TestStreamIdleTimeoutResetsPerChunk is the false-positive guard: a model that
// keeps emitting, slowly, must never be cut off. Chunks arrive at ~60ms against
// a 200ms bound, for longer in total than the bound itself — only a bound that
// resets per chunk survives that.
func TestStreamIdleTimeoutResetsPerChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for i := 0; i < 8; i++ {
			_, _ = w.Write([]byte("{\"message\":{\"content\":\"tok \"}}\n"))
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
		_, _ = w.Write([]byte("{\"done\":true,\"done_reason\":\"stop\"}\n"))
	}))
	defer srv.Close()

	stream, err := New(WithBaseURL(srv.URL), WithStreamIdleTimeout(200*time.Millisecond)).
		Stream(context.Background(), provider.Request{
			Model:    "llama3.2",
			Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
		})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	sawDone := false
	for ev := range stream {
		if ev.Type == provider.EventError {
			t.Fatalf("a slow but progressing stream was aborted: %v", ev.Err)
		}
		if ev.Type == provider.EventDone {
			sawDone = true
		}
	}
	if !sawDone {
		t.Error("expected the slow stream to complete normally")
	}
}

// TestStreamIdleTimeoutDisabled: a negative configured value must turn the
// bound off entirely, leaving the pre-P59.2 behavior available to anyone who
// wants an unbounded stream.
func TestStreamIdleTimeoutDisabled(t *testing.T) {
	a := New(WithStreamIdleTimeout(-1))
	if got := a.resolveStreamIdleTimeout(); got != 0 {
		t.Errorf("negative config = %v, want 0 (disabled)", got)
	}
	if got := New().resolveStreamIdleTimeout(); got != sse.DefaultStreamIdleTimeout {
		t.Errorf("unset = %v, want the %v default", got, sse.DefaultStreamIdleTimeout)
	}
}

// TestStreamClampsNumPredictToHeadroom (P59.1): num_ctx covers prompt and
// completion out of one budget, so a max_tokens larger than what the prompt
// leaves is a request to be truncated mid-answer. The adapter must send what
// actually fits — and must never inflate a caller's smaller request.
func TestStreamClampsNumPredictToHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name      string
		numCtx    int
		maxTokens int
		prompt    string
		check     func(t *testing.T, sent int)
	}{
		{
			// The shipped default pair on a stock Ollama window.
			name: "default max_tokens against a stock window", numCtx: 4096, maxTokens: 32768, prompt: "hi",
			check: func(t *testing.T, sent int) {
				if sent >= 4096 || sent <= 0 {
					t.Errorf("num_predict = %d, want a positive value below the 4096 window", sent)
				}
			},
		},
		{
			name: "room to spare passes through untouched", numCtx: 131072, maxTokens: 8192, prompt: "hi",
			check: func(t *testing.T, sent int) {
				if sent != 8192 {
					t.Errorf("num_predict = %d, want the requested 8192 — the clamp must never lower a request that fits", sent)
				}
			},
		},
		{
			name: "a caller asking for little still gets little", numCtx: 131072, maxTokens: 64, prompt: "hi",
			check: func(t *testing.T, sent int) {
				if sent != 64 {
					t.Errorf("num_predict = %d, want the requested 64 — the clamp is one-directional", sent)
				}
			},
		},
		{
			// The prompt has already eaten the window. A negative num_predict
			// means "generate until the context is full" to Ollama, which is
			// the exact behavior being avoided, so a floor applies.
			name:   "prompt already over the window floors rather than going negative",
			numCtx: 512, maxTokens: 32768, prompt: strings.Repeat("token ", 4000),
			check: func(t *testing.T, sent int) {
				if sent != minNumPredict {
					t.Errorf("num_predict = %d, want the %d floor", sent, minNumPredict)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody wireRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"message":{"content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
			}))
			defer srv.Close()

			stream, err := New(WithBaseURL(srv.URL), WithNumCtx(tc.numCtx)).Stream(context.Background(), provider.Request{
				Model:     "llama3.2",
				MaxTokens: tc.maxTokens,
				Messages:  []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: tc.prompt}}}},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for range stream {
			}
			if gotBody.Options == nil {
				t.Fatal("expected options to be sent")
			}
			tc.check(t, gotBody.Options.NumPredict)
		})
	}
}

// TestStreamWithoutNumCtxLeavesMaxTokensAlone: with no window known, the
// adapter has nothing to reconcile against and must not invent a bound — the
// pre-P59.1 behavior for any caller that never sets a context window.
func TestStreamWithoutNumCtxLeavesMaxTokensAlone(t *testing.T) {
	var gotBody wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	stream, err := New(WithBaseURL(srv.URL)).Stream(context.Background(), provider.Request{
		Model:     "llama3.2",
		MaxTokens: 32768,
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	if gotBody.Options == nil || gotBody.Options.NumPredict != 32768 {
		t.Errorf("num_predict = %+v, want the unclamped 32768 when no window is known", gotBody.Options)
	}
}
