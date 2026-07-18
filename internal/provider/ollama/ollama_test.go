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
			name:          "prior length signal",
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

func TestWithBaseURLStripsV1Suffix(t *testing.T) {
	a := New(WithBaseURL("http://localhost:11434/v1/"))
	if a.baseURL != "http://localhost:11434" {
		t.Errorf("baseURL = %q, want http://localhost:11434", a.baseURL)
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

// TestWithResponseHeaderTimeout is the P35.5 adapter-level regression:
// WithResponseHeaderTimeout must actually change the transport's configured
// ResponseHeaderTimeout, and an adapter built with no such option keeps
// sse.DefaultResponseHeaderTimeout (5m).
func TestWithResponseHeaderTimeout(t *testing.T) {
	def := New()
	tr, ok := def.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", def.client.Transport)
	}
	if tr.ResponseHeaderTimeout != 5*time.Minute {
		t.Errorf("default ResponseHeaderTimeout = %v, want 5m", tr.ResponseHeaderTimeout)
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
