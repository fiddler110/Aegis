package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
)

// sampleStream is a minimal but representative Anthropic SSE response that
// includes a text block and a tool-use block.
const sampleStream = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":42,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_99","name":"search"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"cats\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":17}}

event: message_stop
data: {"type":"message_stop"}

`

func TestToWireMessagesImage(t *testing.T) {
	msgs := []provider.Message{{
		Role: provider.RoleUser,
		Content: []provider.Block{
			provider.TextBlock{Text: "what is this?"},
			provider.ImageBlock{MediaType: "image/png", Data: "aGVsbG8="},
		},
	}}
	wire, err := toWireMessages(msgs)
	if err != nil {
		t.Fatalf("toWireMessages: %v", err)
	}
	if len(wire) != 1 || len(wire[0].Content) != 2 {
		t.Fatalf("unexpected wire shape: %+v", wire)
	}
	img := wire[0].Content[1]
	if img.Type != "image" || img.Source == nil {
		t.Fatalf("second block = %+v, want image with source", img)
	}
	if img.Source.Type != "base64" || img.Source.MediaType != "image/png" || img.Source.Data != "aGVsbG8=" {
		t.Errorf("image source = %+v", img.Source)
	}
}

func TestStreamParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing/incorrect api key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing anthropic-version header")
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(sampleStream))
	}))
	defer srv.Close()

	a := New("test-key", WithBaseURL(srv.URL))
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:     "claude-test",
		MaxTokens: 100,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var tool *provider.ToolUseBlock
	var done *provider.Event
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			text += ev.Text
		case provider.EventToolUse:
			tool = ev.ToolUse
		case provider.EventDone:
			e := ev
			done = &e
		case provider.EventError:
			t.Fatalf("unexpected error event: %v", ev.Err)
		}
	}

	if text != "Hello world" {
		t.Errorf("text = %q, want %q", text, "Hello world")
	}
	if tool == nil {
		t.Fatal("expected a tool-use block")
	}
	if tool.ID != "tu_99" || tool.Name != "search" {
		t.Errorf("tool id/name = %q/%q", tool.ID, tool.Name)
	}
	if string(tool.Input) != `{"q":"cats"}` {
		t.Errorf("tool input = %s, want {\"q\":\"cats\"}", tool.Input)
	}
	if done == nil {
		t.Fatal("expected a done event")
	}
	if done.Stop != provider.StopToolUse {
		t.Errorf("stop reason = %q, want tool_use", done.Stop)
	}
	if done.Usage == nil || done.Usage.InputTokens != 42 || done.Usage.OutputTokens != 17 {
		t.Errorf("usage = %+v, want input=42 output=17", done.Usage)
	}
}

// thinkingStream is an SSE response containing a thinking block (with a
// signature) followed by a text answer.
const thinkingStream = `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reason."}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG123"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_stop
data: {"type":"message_stop"}

`

func TestThinkingStreamParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(thinkingStream))
	}))
	defer srv.Close()

	a := New("k", WithBaseURL(srv.URL), WithThinking(2048))
	stream, err := a.Stream(context.Background(), provider.Request{Model: "m", MaxTokens: 100})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var thinkDelta, text string
	var assembled *provider.ThinkingBlock
	for ev := range stream {
		switch ev.Type {
		case provider.EventThinkingDelta:
			thinkDelta += ev.Text
		case provider.EventThinking:
			assembled = ev.Thinking
		case provider.EventTextDelta:
			text += ev.Text
		case provider.EventError:
			t.Fatalf("unexpected error: %v", ev.Err)
		}
	}
	if thinkDelta != "Let me reason." {
		t.Errorf("thinking deltas = %q", thinkDelta)
	}
	if assembled == nil || assembled.Text != "Let me reason." || assembled.Signature != "SIG123" {
		t.Errorf("assembled thinking = %+v", assembled)
	}
	if text != "Answer" {
		t.Errorf("text = %q", text)
	}
}

func TestThinkingRequestBody(t *testing.T) {
	// With thinking enabled, the request must carry a thinking block and omit
	// temperature (the API only allows the default while thinking).
	temp := 0.7
	body := captureBodyReq(t, provider.Request{Model: "m", MaxTokens: 4096, Temperature: &temp},
		WithThinking(2048))
	th, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking missing from body: %v", body["thinking"])
	}
	if th["type"] != "enabled" || th["budget_tokens"].(float64) != 2048 {
		t.Errorf("thinking = %+v", th)
	}
	if _, present := body["temperature"]; present {
		t.Error("temperature must be omitted when thinking is enabled")
	}
}

func TestThinkingBlockSerialized(t *testing.T) {
	// A thinking block in history must be replayed with its text and signature.
	body := captureBodyReq(t, provider.Request{
		Model:     "m",
		MaxTokens: 100,
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Content: []provider.Block{
				provider.ThinkingBlock{Text: "reasoned", Signature: "SIG"},
				provider.TextBlock{Text: "hi"},
			}},
		},
	})
	msgs := body["messages"].([]any)
	blocks := msgs[0].(map[string]any)["content"].([]any)
	first := blocks[0].(map[string]any)
	if first["type"] != "thinking" || first["thinking"] != "reasoned" || first["signature"] != "SIG" {
		t.Errorf("thinking block = %+v", first)
	}
}

func TestStreamErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"bad key"}}`))
	}))
	defer srv.Close()

	a := New("x", WithBaseURL(srv.URL))
	_, err := a.Stream(context.Background(), provider.Request{Model: "m", MaxTokens: 1})
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *provider.APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Retryable() {
		t.Error("401 should not be retryable")
	}
}

// captureBody decodes the JSON request body the adapter sends.
func captureBody(t *testing.T, opts ...Option) map[string]any {
	t.Helper()
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	a := New("k", append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:     "m",
		MaxTokens: 10,
		System:    "you are a test",
		Tools: []provider.ToolSchema{
			{Name: "a", Description: "tool a", InputSchema: json.RawMessage(`{}`)},
			{Name: "b", Description: "tool b", InputSchema: json.RawMessage(`{}`)},
		},
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream { //nolint:revive // drain
	}
	return captured
}

// captureBodyReq decodes the JSON body for an explicit request, used to assert
// request-shaping options like thinking.
func captureBodyReq(t *testing.T, req provider.Request, opts ...Option) map[string]any {
	t.Helper()
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	a := New("k", append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	stream, err := a.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream { //nolint:revive // drain
	}
	return captured
}

func TestPromptCachingBreakpoints(t *testing.T) {
	body := captureBody(t)

	// system is an array whose block carries cache_control.
	sys, ok := body["system"].([]any)
	if !ok || len(sys) != 1 {
		t.Fatalf("system not a single-block array: %#v", body["system"])
	}
	if _, has := sys[0].(map[string]any)["cache_control"]; !has {
		t.Error("system block missing cache_control")
	}

	// last tool carries cache_control; first does not.
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %#v", body["tools"])
	}
	if _, has := tools[0].(map[string]any)["cache_control"]; has {
		t.Error("first tool should not have cache_control")
	}
	if _, has := tools[1].(map[string]any)["cache_control"]; !has {
		t.Error("last tool missing cache_control")
	}

	// last message's last content block carries cache_control.
	msgs := body["messages"].([]any)
	lastMsg := msgs[len(msgs)-1].(map[string]any)
	content := lastMsg["content"].([]any)
	lastBlock := content[len(content)-1].(map[string]any)
	if _, has := lastBlock["cache_control"]; !has {
		t.Error("last message block missing cache_control")
	}
}

func TestPromptCachingDisabled(t *testing.T) {
	body := captureBody(t, WithPromptCaching(false))
	sys := body["system"].([]any)
	if _, has := sys[0].(map[string]any)["cache_control"]; has {
		t.Error("caching disabled but system has cache_control")
	}
}

func TestCacheUsageParsing(t *testing.T) {
	const s = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":200}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(s))
	}))
	defer srv.Close()

	a := New("k", WithBaseURL(srv.URL))
	stream, err := a.Stream(context.Background(), provider.Request{Model: "m", MaxTokens: 1})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var done *provider.Event
	for ev := range stream {
		if ev.Type == provider.EventDone {
			e := ev
			done = &e
		}
	}
	if done == nil || done.Usage == nil {
		t.Fatal("no usage")
	}
	if done.Usage.CacheCreationTokens != 100 || done.Usage.CacheReadTokens != 200 {
		t.Errorf("cache usage = %+v, want creation=100 read=200", done.Usage)
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
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
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

func TestMissingAPIKey(t *testing.T) {
	a := New("")
	_, err := a.Stream(context.Background(), provider.Request{Model: "m", MaxTokens: 1})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}
