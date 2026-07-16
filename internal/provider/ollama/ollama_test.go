package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":11,"eval_count":7,"load_duration":8200000000}
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

	a := New(WithBaseURL(srv.URL), WithNumCtx(8192))
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
}
