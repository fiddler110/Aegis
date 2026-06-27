package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
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
