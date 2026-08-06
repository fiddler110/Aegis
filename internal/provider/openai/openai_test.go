package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/sse"
)

func TestTranslateImage(t *testing.T) {
	msgs := []provider.Message{{
		Role: provider.RoleUser,
		Content: []provider.Block{
			provider.TextBlock{Text: "describe"},
			provider.ImageBlock{MediaType: "image/jpeg", Data: "aGk="},
		},
	}}
	wire, err := translate("", msgs)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(wire) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(wire), wire)
	}
	parts, ok := wire[0].Content.([]contentPart)
	if !ok {
		t.Fatalf("content = %T, want []contentPart", wire[0].Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("unexpected parts: %+v", parts)
	}
	if want := "data:image/jpeg;base64,aGk="; parts[1].ImageURL == nil || parts[1].ImageURL.URL != want {
		t.Errorf("image url = %+v, want %q", parts[1].ImageURL, want)
	}
}

const sampleStream = `data: {"choices":[{"delta":{"content":"Hello "}}]}

data: {"choices":[{"delta":{"content":"there"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"search","arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"cats\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}

data: [DONE]

`

func TestStreamParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing bearer auth header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sampleStream))
	}))
	defer srv.Close()

	a := New("k", WithBaseURL(srv.URL))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:     "gpt-test",
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
	var done *provider.Event
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			text += ev.Text
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
	if tu == nil || tu.Name != "search" || string(tu.Input) != `{"q":"cats"}` {
		t.Errorf("tool use assembled wrong: %+v", tu)
	}
	if done == nil || done.Stop != provider.StopToolUse {
		t.Errorf("stop reason wrong: %+v", done)
	}
	if done.Usage == nil || done.Usage.InputTokens != 11 || done.Usage.OutputTokens != 7 {
		t.Errorf("usage = %+v", done.Usage)
	}
}

// splitToolCallStream names a tool call in one delta and only IDs it in the
// next, the shape that makes the start event's ID unreliable (P33.3).
const splitToolCallStream = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"search"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"arguments":"{\"q\":\"cats\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

// streamEvents runs body through the adapter and collects every event.
func streamEvents(t *testing.T, body string) []provider.Event {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	stream, err := New("k", WithBaseURL(srv.URL)).Stream(context.Background(), provider.Request{
		Model:     "gpt-test",
		MaxTokens: 50,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var out []provider.Event
	for ev := range stream {
		out = append(out, ev)
	}
	return out
}

// TestStreamEmitsToolUseStartBeforeArguments is the P33.3 guard: the adapter
// must announce a tool call as soon as a delta names it, rather than only
// once the whole stream has ended — generating a call's arguments is often
// the longest phase of a turn, and the client has nothing to show for it
// until this event arrives.
func TestStreamEmitsToolUseStartBeforeArguments(t *testing.T) {
	var starts []provider.ToolUseBlock
	var sawTerminal bool
	for _, ev := range streamEvents(t, sampleStream) {
		switch ev.Type {
		case provider.EventToolUseStart:
			if sawTerminal {
				t.Error("tool_use_start arrived after the assembled tool call — it must precede it")
			}
			starts = append(starts, *ev.ToolUse)
		case provider.EventToolUse:
			sawTerminal = true
		}
	}
	if len(starts) != 1 {
		t.Fatalf("got %d tool_use_start events, want exactly one per call: %+v", len(starts), starts)
	}
	if starts[0].Name != "search" || starts[0].ID != "call_1" {
		t.Errorf("start = %+v, want name \"search\" and id \"call_1\"", starts[0])
	}
	if len(starts[0].Input) != 0 {
		t.Errorf("start carried input %q; arguments are still streaming at that point", starts[0].Input)
	}
	if !sawTerminal {
		t.Error("the assembled EventToolUse must still follow the start event unchanged")
	}
}

// TestStreamToolUseStartAnnouncedOnceWhenIDArrivesLate covers a server that
// names a tool call before it IDs it: the start fires on the naming delta
// (ID-less, since that is all that's known yet) and the later ID delta must
// not announce the same call a second time. The assembled call still carries
// the ID.
func TestStreamToolUseStartAnnouncedOnceWhenIDArrivesLate(t *testing.T) {
	var starts []provider.ToolUseBlock
	var tu *provider.ToolUseBlock
	for _, ev := range streamEvents(t, splitToolCallStream) {
		switch ev.Type {
		case provider.EventToolUseStart:
			starts = append(starts, *ev.ToolUse)
		case provider.EventToolUse:
			tu = ev.ToolUse
		}
	}
	if len(starts) != 1 {
		t.Fatalf("got %d tool_use_start events, want exactly one: %+v", len(starts), starts)
	}
	if starts[0].Name != "search" || starts[0].ID != "" {
		t.Errorf("start = %+v, want name \"search\" and no id yet", starts[0])
	}
	if tu == nil || tu.ID != "call_1" || string(tu.Input) != `{"q":"cats"}` {
		t.Errorf("assembled call = %+v, want the late-arriving id and full arguments", tu)
	}
}

// TestStreamToolUseStartPerConcurrentCall checks each streamed tool call gets
// its own start event, since a chat-completions stream interleaves several by
// index.
func TestStreamToolUseStartPerConcurrentCall(t *testing.T) {
	const twoCalls = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"grep","arguments":"{\"pattern\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"x\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var names []string
	for _, ev := range streamEvents(t, twoCalls) {
		if ev.Type == provider.EventToolUseStart {
			names = append(names, ev.ToolUse.Name)
		}
	}
	if len(names) != 2 || names[0] != "read_file" || names[1] != "grep" {
		t.Errorf("start events = %v, want one per call in index order", names)
	}
}

func TestCustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gateway-Token") != "gw-secret" {
			t.Errorf("X-Gateway-Token = %q, want %q", r.Header.Get("X-Gateway-Token"), "gw-secret")
		}
		if r.Header.Get("X-Tenant-ID") != "tenant-42" {
			t.Errorf("X-Tenant-ID = %q, want %q", r.Header.Get("X-Tenant-ID"), "tenant-42")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	a := New("k", WithBaseURL(srv.URL), WithHeaders(map[string]string{
		"X-Gateway-Token": "gw-secret",
		"X-Tenant-ID":     "tenant-42",
	}))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model: "m", MaxTokens: 1,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
}

// TestReasoningModelUsesMaxCompletionTokens is the P9 regression: real OpenAI
// o1/o3-class reasoning models reject the max_tokens field outright (a 400)
// and require max_completion_tokens instead. A non-reasoning model must keep
// using max_tokens.
func TestReasoningModelUsesMaxCompletionTokens(t *testing.T) {
	tests := []struct {
		model           string
		wantField       string
		wantOtherAbsent string
	}{
		{"o1", "max_completion_tokens", "max_tokens"},
		{"o1-mini", "max_completion_tokens", "max_tokens"},
		{"o3", "max_completion_tokens", "max_tokens"},
		{"o3-mini", "max_completion_tokens", "max_tokens"},
		{"openai/o1-mini", "max_completion_tokens", "max_tokens"}, // vendor-prefixed (OpenRouter-style)
		{"gpt-4o", "max_tokens", "max_completion_tokens"},
		{"gpt-4.1-mini", "max_tokens", "max_completion_tokens"},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer srv.Close()

			a := New("k", WithBaseURL(srv.URL))
			stream, err := a.Stream(context.Background(), provider.Request{
				Model: tc.model, MaxTokens: 123,
				Messages: []provider.Message{
					{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
				},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for range stream {
			}

			if v, ok := body[tc.wantField]; !ok || v != float64(123) {
				t.Errorf("model %q: %s = %v (ok=%v), want 123", tc.model, tc.wantField, v, ok)
			}
			if _, ok := body[tc.wantOtherAbsent]; ok {
				t.Errorf("model %q: %s should be absent, got %v", tc.model, tc.wantOtherAbsent, body[tc.wantOtherAbsent])
			}
		})
	}
}

// TestStreamingClientHasNoWholeRequestTimeout is the P33.1 regression: the
// adapter used to build its client with Timeout: 10*time.Minute, which bounds
// the entire request including reading the streamed body, so a turn that
// streamed for longer than that died mid-answer as a transport error.
func TestStreamingClientHasNoWholeRequestTimeout(t *testing.T) {
	a := New("k")
	if a.client.Timeout != 0 {
		t.Errorf("client.Timeout = %v, want 0 (it would cap streamed turn length)", a.client.Timeout)
	}
	tr, ok := a.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport = %T, want *http.Transport", a.client.Transport)
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Errorf("ResponseHeaderTimeout = %v, want a bound on a server that never replies", tr.ResponseHeaderTimeout)
	}
}

// TestStreamOutlastsResponseHeaderTimeout proves the header timeout bounds
// only the wait for headers: a body that keeps streaming well past it still
// completes. The transport is rebuilt with a short timeout so the test does
// not have to outlast the production five-minute value.
func TestStreamOutlastsResponseHeaderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for _, chunk := range []string{"slow ", "but ", "finished"} {
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + chunk + `"}}]}` + "\n\n"))
			w.(http.Flusher).Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	a := New("k", WithBaseURL(srv.URL))
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 50 * time.Millisecond
	a.client = &http.Client{Timeout: 0, Transport: tr}

	stream, err := a.Stream(context.Background(), provider.Request{
		Model: "m", MaxTokens: 1,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var done bool
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			text += ev.Text
		case provider.EventDone:
			done = true
		case provider.EventError:
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	if text != "slow but finished" || !done {
		t.Errorf("text = %q, done = %v; stream did not survive past the header timeout", text, done)
	}
}

// TestResponseHeaderTimeoutRewrapped is the P35.6 regression: when an
// OpenAI-compat local backend (e.g. Ollama's compat endpoint) withholds its
// response header past ResponseHeaderTimeout, the bare Go transport string
// "net/http: timeout awaiting response headers" must not reach the caller
// unrewrapped — it names no cause and no remedy. Stream must instead return
// provider.NewResponseHeaderTimeoutError's actionable, non-retryable error
// naming provider.response_header_timeout and context_window.
func TestResponseHeaderTimeoutRewrapped(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never write a header until the client has given up
	}))
	defer srv.Close()
	defer close(block)

	a := New("k", WithBaseURL(srv.URL), WithResponseHeaderTimeout(50*time.Millisecond))

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

// TestMidStreamError is the other half of P33.1: Ollama reports a model OOM,
// context overflow or worker crash as an error object mid-stream, which the
// adapter used to drop on the floor — the turn just ended up truncated with
// no explanation. The error field is a bare string on Ollama and an object on
// OpenAI/vLLM; both must surface, and unrelated noise must stay ignored.
func TestMidStreamError(t *testing.T) {
	tests := []struct {
		name    string
		chunk   string
		wantErr string
		wantTxt string
	}{
		{
			name:    "ollama bare string",
			chunk:   `data: {"error":"model runner has unexpectedly stopped"}`,
			wantErr: "openai: model runner has unexpectedly stopped",
			wantTxt: "partial",
		},
		{
			name:    "openai error object",
			chunk:   `data: {"error":{"message":"context length exceeded","type":"invalid_request_error"}}`,
			wantErr: "openai: context length exceeded",
			wantTxt: "partial",
		},
		{
			name:    "object without message falls back to raw",
			chunk:   `data: {"error":{"code":500}}`,
			wantErr: `openai: {"code":500}`,
			wantTxt: "partial",
		},
		{
			name:    "undecodable chunk carrying an error",
			chunk:   `data: {"choices":"malformed","error":"worker crashed"}`,
			wantErr: "openai: worker crashed",
			wantTxt: "partial",
		},
		{
			name:    "malformed noise is skipped",
			chunk:   `data: {not json at all`,
			wantErr: "",
			wantTxt: "partialrest",
		},
		{
			name:    "null error is not an error",
			chunk:   `data: {"choices":[],"error":null}`,
			wantErr: "",
			wantTxt: "partialrest",
		},
		{
			name:    "unrelated error type is skipped",
			chunk:   `data: {"choices":[],"error":123}`,
			wantErr: "",
			wantTxt: "partialrest",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n" +
				tc.chunk + "\n\n" +
				`data: {"choices":[{"delta":{"content":"rest"}}]}` + "\n\n" +
				"data: [DONE]\n\n"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			a := New("k", WithBaseURL(srv.URL))
			stream, err := a.Stream(context.Background(), provider.Request{
				Model: "m", MaxTokens: 1,
				Messages: []provider.Message{
					{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
				},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}

			var text, gotErr string
			var done bool
			for ev := range stream {
				switch ev.Type {
				case provider.EventTextDelta:
					text += ev.Text
				case provider.EventError:
					gotErr = ev.Err.Error()
				case provider.EventDone:
					done = true
				}
			}
			if gotErr != tc.wantErr {
				t.Errorf("error = %q, want %q", gotErr, tc.wantErr)
			}
			if text != tc.wantTxt {
				t.Errorf("text = %q, want %q", text, tc.wantTxt)
			}
			if tc.wantErr != "" && done {
				t.Errorf("stream emitted Done after a fatal mid-stream error")
			}
			if tc.wantErr == "" && !done {
				t.Errorf("benign chunk aborted the stream")
			}
		})
	}
}

// TestStreamTruncatedToolCallSurfacesContextLimit is the P35.2 guard: a tool
// call whose arguments are cut off mid-JSON must be distinguished by the
// truncation signal (finish_reason "length"), not by the parse failure itself.
// With the length signal it surfaces the actionable, discoverable context-limit
// error; a genuinely malformed call that stopped cleanly (finish_reason "stop")
// still surfaces a plain parse error.
func TestStreamTruncatedToolCallSurfacesContextLimit(t *testing.T) {
	// A tool call whose arguments JSON is cut off partway through.
	cutOffArgs := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":"{\"path\":\"/very/long"}}]}}]}` + "\n\n"

	for _, tc := range []struct {
		name          string
		body          string
		wantTruncated bool
	}{
		{
			name:          "cut off with finish_reason length",
			body:          cutOffArgs + `data: {"choices":[{"delta":{},"finish_reason":"length"}]}` + "\n\ndata: [DONE]\n\n",
			wantTruncated: true,
		},
		{
			name:          "malformed with finish_reason stop stays generic",
			body:          cutOffArgs + `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n",
			wantTruncated: false,
		},
		{
			// The server may parse tool calls itself and report the failure as
			// a mid-stream {"error":...} envelope carrying the truncated shape.
			name:          "server error envelope with truncated shape",
			body:          `data: {"error":{"message":"invalid tool call arguments for \"read_file\": unexpected end of JSON input"}}` + "\n\ndata: [DONE]\n\n",
			wantTruncated: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotErr error
			for _, ev := range streamEvents(t, tc.body) {
				if ev.Type == provider.EventError {
					gotErr = ev.Err
				}
				if ev.Type == provider.EventToolUse {
					t.Fatalf("a broken tool call must not be emitted as a tool use: %+v", ev.ToolUse)
				}
			}
			if gotErr == nil {
				t.Fatal("expected an error event")
			}
			gotTruncated := strings.Contains(gotErr.Error(), "context limit") &&
				strings.Contains(gotErr.Error(), "context_window")
			if gotTruncated != tc.wantTruncated {
				t.Errorf("truncation-error = %v, want %v; error was: %v", gotTruncated, tc.wantTruncated, gotErr)
			}
		})
	}
}

func TestTranslateToolResults(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.TextBlock{Text: "calling"},
			provider.ToolUseBlock{ID: "c1", Name: "f", Input: []byte(`{"x":1}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "c1", Content: "done"},
		}},
	}
	out, err := translate("sys", msgs)
	if err != nil {
		t.Fatal(err)
	}
	// system, user, assistant(with tool_calls), tool
	if len(out) != 4 {
		t.Fatalf("got %d wire messages, want 4: %+v", len(out), out)
	}
	if out[0].Role != "system" {
		t.Errorf("first message should be system")
	}
	if out[2].Role != "assistant" || len(out[2].ToolCalls) != 1 || out[2].ToolCalls[0].Function.Name != "f" {
		t.Errorf("assistant tool_calls wrong: %+v", out[2])
	}
	if out[3].Role != "tool" || out[3].ToolCallID != "c1" || out[3].Content != "done" {
		t.Errorf("tool message wrong: %+v", out[3])
	}
}

// TestStreamMapsFinishReasonToStopReason covers the gap behind P34.2's
// false positive: the adapter only ever mapped "tool_calls", so `stop`
// defaulted to StopEndTurn and a response cut off at the token cap arrived
// indistinguishable from a model that chose to stop. The tool-calling probe
// read that as "made no tool call" and accused a capable model of not
// supporting tool calls.
func TestStreamMapsFinishReasonToStopReason(t *testing.T) {
	const toolCallDelta = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"list_files","arguments":"{}"}}]}}]}

`
	for _, tc := range []struct {
		name string
		body string
		want provider.StopReason
	}{
		{
			name: "length means truncated",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"thinking\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
			want: provider.StopMaxTokens,
		},
		{
			name: "stop means end of turn",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
			want: provider.StopEndTurn,
		},
		{
			name: "tool_calls wins",
			body: toolCallDelta + "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n",
			want: provider.StopToolUse,
		},
		{
			// A cap reached after the call already landed truncated nothing
			// the caller was waiting for — parity with the Ollama adapter.
			name: "a tool call already seen outranks a later length",
			body: toolCallDelta + "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
			want: provider.StopToolUse,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var done *provider.Event
			for _, ev := range streamEvents(t, tc.body) {
				if ev.Type == provider.EventDone {
					e := ev
					done = &e
				}
			}
			if done == nil {
				t.Fatal("no EventDone emitted")
			}
			if done.Stop != tc.want {
				t.Errorf("Stop = %q, want %q", done.Stop, tc.want)
			}
		})
	}
}

// TestStreamWithoutTerminatorIsAnError (P61.2): a body that closes cleanly
// mid-generation produces no read error, so before this fix the adapter emitted
// EventDone with zeroed usage and StopEndTurn — a truncated answer presented as
// a complete one. It must be classified as a transport failure instead, so the
// existing backend-unavailable resume path handles it. Mirrors P59.3 on the
// native Ollama adapter.
func TestStreamWithoutTerminatorIsAnError(t *testing.T) {
	// Well-formed content chunks, then a clean EOF — no finish_reason, no [DONE].
	body := `data: {"choices":[{"delta":{"content":"partial "}}]}

data: {"choices":[{"delta":{"content":"answer"}}]}

`
	var gotErr error
	sawDone := false
	for _, ev := range streamEvents(t, body) {
		switch ev.Type {
		case provider.EventError:
			gotErr = ev.Err
		case provider.EventDone:
			sawDone = true
		}
	}
	if sawDone {
		t.Error("emitted EventDone for a stream that never sent a terminator")
	}
	if gotErr == nil {
		t.Fatal("expected an error event for a stream truncated before its terminator")
	}
	if !provider.IsBackendUnavailableError(gotErr) {
		t.Errorf("truncated stream = %v, want it classified backend-unavailable so the P50.1 resume path handles it", gotErr)
	}
}

// TestStreamTerminatorsStillComplete guards the other side of P61.2: a stream
// that does end properly must still finish with EventDone and no error event.
// Both accepted terminators are covered — "[DONE]" is the OpenAI sentinel, but
// compat backends (Ollama's /v1 among them) can close after the final chunk
// carrying finish_reason without sending it, and failing those would be a worse
// regression than the bug.
func TestStreamTerminatorsStillComplete(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "finish_reason and [DONE]",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		},
		{
			name: "[DONE] alone",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n",
		},
		{
			name: "finish_reason alone",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var text string
			var done *provider.Event
			for _, ev := range streamEvents(t, tc.body) {
				switch ev.Type {
				case provider.EventTextDelta:
					text += ev.Text
				case provider.EventError:
					t.Fatalf("unexpected error event: %v", ev.Err)
				case provider.EventDone:
					e := ev
					done = &e
				}
			}
			if text != "hi" {
				t.Errorf("text = %q", text)
			}
			if done == nil {
				t.Fatal("expected EventDone")
			}
			if done.Stop != provider.StopEndTurn {
				t.Errorf("Stop = %q, want %q", done.Stop, provider.StopEndTurn)
			}
		})
	}
}

// TestStreamIdleTimeoutWiring (P61.1) covers this adapter's half of the idle
// bound: the resolution semantics and that the resolved value actually reaches
// sse.Run. The watchdog *mechanism* — reset-per-line, the message, the
// transport classification — is owned and tested once in internal/provider/sse
// since P61.6, so re-testing it here would only duplicate it.
//
// The semantics must match ollama.resolveStreamIdleTimeout exactly:
// provider.stream_idle_timeout is one config key, so it cannot mean one thing
// on the native path and another on the compat path — which is the same
// endpoint on the same machine for a local user.
func TestStreamIdleTimeoutWiring(t *testing.T) {
	if got := New("k").resolveStreamIdleTimeout(); got != sse.DefaultStreamIdleTimeout {
		t.Errorf("unset = %v, want the %v default", got, sse.DefaultStreamIdleTimeout)
	}
	if got := New("k", WithStreamIdleTimeout(-1)).resolveStreamIdleTimeout(); got != 0 {
		t.Errorf("negative config = %v, want 0 (bound disabled)", got)
	}
	if got := New("k", WithStreamIdleTimeout(90*time.Second)).resolveStreamIdleTimeout(); got != 90*time.Second {
		t.Errorf("explicit config = %v, want 90s", got)
	}
}

// TestStreamIdleTimeoutAbortsAStalledRunner is the end-to-end half: headers
// arrive, one chunk streams, then the server wedges. This adapter is what talks
// to Ollama's OpenAI-compat /v1 endpoint, so the stall P59.2 was filed for
// reaches it as readily as the native path. It must end as a transport error —
// the classification a crashed server gets, so the existing wait-and-resume
// path handles a wedged one too — and never as EventDone.
func TestStreamIdleTimeoutAbortsAStalledRunner(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"thinking\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	stream, err := New("k", WithBaseURL(srv.URL), WithStreamIdleTimeout(100*time.Millisecond)).
		Stream(context.Background(), provider.Request{
			Model:    "m",
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
		t.Fatal("the stream never ended — the idle bound did not reach sse.Run")
	}
}

// clampCapture streams one request through a stub server and returns the
// max_tokens (or max_completion_tokens) that reached the wire.
func clampCapture(t *testing.T, req provider.Request, opts ...Option) map[string]any {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	stream, err := New("k", append([]Option{WithBaseURL(srv.URL)}, opts...)...).Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	return body
}

// TestStreamClampsMaxTokensToHeadroom (P61.4) is the compat-path counterpart of
// the native adapter's TestStreamClampsNumPredictToHeadroom: when the backend
// spends one budget on prompt and completion, a max_tokens larger than what the
// prompt leaves is a request to be cut off mid-answer. Same arithmetic, same
// floor, same one-directionality — the two adapters can be aimed at the same
// server, so they must not disagree.
func TestStreamClampsMaxTokensToHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name      string
		numCtx    int
		maxTokens int
		prompt    string
		check     func(t *testing.T, sent int)
	}{
		{
			// The shipped default pair on a stock Ollama window: 8x the whole
			// window asked for in output.
			name: "default max_tokens against a stock window", numCtx: 4096, maxTokens: 32768, prompt: "hi",
			check: func(t *testing.T, sent int) {
				if sent >= 4096 || sent <= 0 {
					t.Errorf("max_tokens = %d, want a positive value below the 4096 window", sent)
				}
			},
		},
		{
			name: "room to spare passes through untouched", numCtx: 131072, maxTokens: 8192, prompt: "hi",
			check: func(t *testing.T, sent int) {
				if sent != 8192 {
					t.Errorf("max_tokens = %d, want the requested 8192 — the clamp must never lower a request that fits", sent)
				}
			},
		},
		{
			name: "a caller asking for little still gets little", numCtx: 131072, maxTokens: 64, prompt: "hi",
			check: func(t *testing.T, sent int) {
				if sent != 64 {
					t.Errorf("max_tokens = %d, want the requested 64 — the clamp is one-directional", sent)
				}
			},
		},
		{
			// The prompt has already eaten the window; the floor is what keeps
			// the model able to say anything at all.
			name:   "prompt already over the window floors rather than going negative",
			numCtx: 512, maxTokens: 32768, prompt: strings.Repeat("token ", 4000),
			check: func(t *testing.T, sent int) {
				if sent != minCompletionTokens {
					t.Errorf("max_tokens = %d, want the %d floor", sent, minCompletionTokens)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := clampCapture(t, provider.Request{
				Model:     "llama3.2",
				MaxTokens: tc.maxTokens,
				NumCtx:    tc.numCtx,
				Messages:  []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: tc.prompt}}}},
			}, WithSharedContextWindow(true))
			v, ok := body["max_tokens"].(float64)
			if !ok {
				t.Fatalf("max_tokens absent from the request body: %v", body)
			}
			tc.check(t, int(v))
		})
	}
}

// TestStreamMaxTokensUnclampedWithoutAWindow: NumCtx is how a genuinely
// resolved window reaches this adapter, and the compat endpoint cannot report
// one on its own. With no window the adapter must send the caller's max_tokens
// untouched rather than invent a bound — a wrong clamp truncates a legitimate
// long generation, which is worse than the bug being fixed.
func TestStreamMaxTokensUnclampedWithoutAWindow(t *testing.T) {
	body := clampCapture(t, provider.Request{
		Model:     "llama3.2",
		MaxTokens: 32768,
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	}, WithSharedContextWindow(true))
	if v, _ := body["max_tokens"].(float64); int(v) != 32768 {
		t.Errorf("max_tokens = %v, want the unclamped 32768 when no window is known", body["max_tokens"])
	}
}

// TestStreamMaxTokensUnclampedOnANonSharedBudgetBackend is the correctness
// constraint that matters most here: real OpenAI, LM Studio, liteLLM and any
// gateway fronting a cloud model bill max_tokens against a *separate* output
// allowance, so the window in NumCtx says nothing about how long a completion
// may be. Without the explicit declaration — the zero value, and therefore what
// every such config gets — nothing is clamped no matter how full the window is.
func TestStreamMaxTokensUnclampedOnANonSharedBudgetBackend(t *testing.T) {
	req := provider.Request{
		Model:     "gpt-4o",
		MaxTokens: 32768,
		NumCtx:    4096, // a window the daemon resolved and stamped on every request
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: strings.Repeat("token ", 2000)}}}},
	}
	body := clampCapture(t, req)
	if v, _ := body["max_tokens"].(float64); int(v) != 32768 {
		t.Errorf("max_tokens = %v, want the unclamped 32768 — a non-shared-budget backend must never be clamped", body["max_tokens"])
	}
}

// TestClampRefusesTheRealOpenAIEndpoint: the option is the gate, but
// api.openai.com is never a shared-budget backend whatever a caller declares,
// so the adapter refuses structurally too. This is the belt to the factory's
// braces — a miswired option must not be able to truncate cloud generations.
func TestClampRefusesTheRealOpenAIEndpoint(t *testing.T) {
	a := New("k", WithSharedContextWindow(true))
	msgs := []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: strings.Repeat("token ", 4000)}}}}
	if got := a.clampMaxTokens(32768, 4096, "", msgs); got != 32768 {
		t.Errorf("clampMaxTokens against %s = %d, want the request untouched", defaultBaseURL, got)
	}
}

// TestClampAppliesToReasoningModelsField: the clamp is computed before the
// max_tokens / max_completion_tokens split, so an o1/o3-named model served by
// Ollama gets the same reconciled number in its own field rather than slipping
// past the clamp because of where the value lands on the wire.
func TestClampAppliesToReasoningModelsField(t *testing.T) {
	body := clampCapture(t, provider.Request{
		Model:     "o3-mini",
		MaxTokens: 32768,
		NumCtx:    4096,
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	}, WithSharedContextWindow(true))
	v, ok := body["max_completion_tokens"].(float64)
	if !ok {
		t.Fatalf("max_completion_tokens absent: %v", body)
	}
	if int(v) >= 4096 || int(v) <= 0 {
		t.Errorf("max_completion_tokens = %d, want a positive value below the 4096 window", int(v))
	}
}

// --- P50.1 liveness probe (added when the OpenAI-compat path was found to have none) ---

// TestHealthy is the probe's verdict guard. A reachable server is healthy and
// must be asked with a side-effect-free GET <base>/models — never a completion,
// which would cost money on a metered backend and load a model on a local one —
// and an unreachable server is not healthy.
func TestHealthy(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	a := New("", WithBaseURL(srv.URL+"/v1"))
	if !a.Healthy(context.Background()) {
		t.Fatal("Healthy() = false against a live server answering /v1/models")
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/models" {
		t.Errorf("probe hit %s %s, want GET /v1/models", gotMethod, gotPath)
	}

	// An unreachable server (closed listener) is not healthy — the case the
	// whole mechanism exists for.
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()
	if New("", WithBaseURL(downURL)).Healthy(context.Background()) {
		t.Error("Healthy() = true against an unreachable server")
	}
}

// TestHealthyTreatsAnAnsweringServerAsAlive pins the deliberate difference from
// the native adapter's "200 or nothing": the probe asks about *liveness*, not
// usability. recoverBackendDown already knows the turn failed; the only thing
// it needs from the probe is whether a server is there again. A 401 from a
// gateway that wants a key, or a 404 from a backend that routes
// /chat/completions but not /models, both prove one is — and calling those
// unhealthy would be worse than having no probe, because the drive would then
// burn the entire recovery budget waiting for a server that never left.
func TestHealthyTreatsAnAnsweringServerAsAlive(t *testing.T) {
	for _, code := range []int{
		http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusTooManyRequests, http.StatusInternalServerError,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		if !New("k", WithBaseURL(srv.URL)).Healthy(context.Background()) {
			t.Errorf("Healthy() = false for status %d, want true — a server that answers at all is reachable", code)
		}
		srv.Close()
	}
}

// TestHealthyRejectsGatewayUpstreamFailures is the carve-out to the rule above:
// on 502/503/504 the thing answering is a proxy explicitly reporting that the
// upstream model server — the thing the drive is waiting for — is not there.
// Reporting healthy would resume the phase straight back into the same failure.
func TestHealthyRejectsGatewayUpstreamFailures(t *testing.T) {
	for _, code := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		if New("k", WithBaseURL(srv.URL)).Healthy(context.Background()) {
			t.Errorf("Healthy() = true for status %d, want false — the upstream model server is down", code)
		}
		srv.Close()
	}
}

// TestHealthySendsConfiguredHeaders: a gateway may need its auth/routing headers
// to answer at all, so the probe carries the same headers Stream does. The
// bearer is sent only when a key was configured, so an unauthenticated local
// server is not handed an empty one.
func TestHealthySendsConfiguredHeaders(t *testing.T) {
	var gotHeader, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader, gotAuth = r.Header.Get("X-Gateway"), r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if !New("sk-test", WithBaseURL(srv.URL), WithHeaders(map[string]string{"X-Gateway": "tenant-a"})).Healthy(context.Background()) {
		t.Fatal("Healthy() = false against a live server")
	}
	if gotHeader != "tenant-a" {
		t.Errorf("X-Gateway = %q, want the configured header forwarded", gotHeader)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want the configured key", gotAuth)
	}

	if !New("", WithBaseURL(srv.URL)).Healthy(context.Background()) {
		t.Fatal("Healthy() = false with no API key configured")
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q with no key configured, want it omitted", gotAuth)
	}
}

// TestHealthProbeUsesTheAdapterTransport mirrors the native adapter's P61.5
// guard: the probe must run on the adapter's own transport, so a user's
// proxy/TLS/dialer configuration reaches the probe that gates P50.1 recovery —
// while keeping its own short timeout, since the streaming client's is
// deliberately unbounded so a long prefill isn't cut off (P59.2).
func TestHealthProbeUsesTheAdapterTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var probes int
	a := New("k", WithBaseURL(srv.URL))
	inner := a.client.Transport
	a.client = &http.Client{Transport: probeCounter{n: &probes, inner: inner}}

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

// probeCounter counts requests and delegates, standing in for whatever
// transport a user's proxy/TLS configuration produced.
type probeCounter struct {
	n     *int
	inner http.RoundTripper
}

func (c probeCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	*c.n++
	return c.inner.RoundTrip(r)
}

// TestHealthyReachableThroughDecorators: the drive never holds a bare adapter —
// it asks provider.CheckBackendHealth, which unwraps the retry/failover
// decorators. A probe the helper cannot reach is a probe P50.1 does not have.
func TestHealthyReachableThroughDecorators(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	wrapped := provider.WithRetry(New("k", WithBaseURL(srv.URL)), provider.DefaultRetryPolicy(), nil)
	healthy, supported := provider.CheckBackendHealth(context.Background(), wrapped)
	if !supported {
		t.Fatal("CheckBackendHealth could not reach the openai adapter through the retry decorator")
	}
	if !healthy {
		t.Error("CheckBackendHealth = unhealthy against a live server")
	}
}
