package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scottymacleod/agentharness/internal/provider"
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
}

func TestMissingAPIKey(t *testing.T) {
	a := New("")
	_, err := a.Stream(context.Background(), provider.Request{Model: "m", MaxTokens: 1})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}
