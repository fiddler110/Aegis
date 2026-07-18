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
