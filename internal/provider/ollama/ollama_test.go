package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
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
