package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// proseToolCall is the exact signature the P34.2 live eval produced: a
// tool-call-shaped JSON object printed into the answer text, with no
// structured tool call anywhere in the turn.
const proseToolCall = "I'll list the files.\n\n" +
	"```json\n{\"name\": \"echo\", \"arguments\": {\"msg\": \"looked\"}}\n```\n\n" +
	"The directory contains main.go and README.md."

// TestToolCallAsTextNotice is the P34.2 regression: a model that cannot emit
// structured tool calls prints them as prose instead (observed live with
// qwen2.5-coder:1.5b, whose manifest claims tool support), and the run
// completes silently — nothing distinguishes "model chose not to use tools"
// from "model cannot speak the protocol". The run must say so.
func TestToolCallAsTextNotice(t *testing.T) {
	prose := []provider.Event{
		{Type: provider.EventTextDelta, Text: proseToolCall},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{prose, prose}}

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "list the files in this directory"},
	}})

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	var warned int
	for _, n := range notices {
		if strings.Contains(n, "tool call as text") {
			warned++
		}
	}
	if warned != 1 {
		t.Errorf("expected exactly one tool-call-as-text notice, got %d of %v", warned, notices)
	}
}

// TestToolCallAsTextNoticeSkippedAfterRealToolCall guards the strongest
// false-positive case: a model that already made a structured tool call this
// run has demonstrably proven it speaks the protocol, so JSON in its final
// answer is quotation, not incapacity.
func TestToolCallAsTextNoticeSkippedAfterRealToolCall(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"looked"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		{
			{Type: provider.EventTextDelta, Text: "I called it as " + proseToolCall},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "list the files in this directory"},
	}})

	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "tool call as text") {
			t.Errorf("notice fired after a real tool call: %q", ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestLooksLikeToolCallJSON(t *testing.T) {
	names := map[string]struct{}{"echo": {}, "shell": {}}

	tests := []struct {
		name string
		text string
		want bool
	}{
		{"observed live signature", `{"name": "shell", "arguments": {"cmd": "ls -la"}}`, true},
		{"fenced in prose", proseToolCall, true},
		{"parameters variant", `Calling {"name":"echo","parameters":{"msg":"hi"}} now.`, true},
		{"openai string arguments", `{"name":"echo","arguments":"{\"msg\":\"hi\"}"}`, true},
		{"nested inside a wrapper", `{"tool_call": {"name": "echo", "arguments": {}}}`, true},
		{"plain prose", "I listed the files: main.go and README.md.", false},
		{"json that is not a tool call", `{"name": "aegis", "version": "1.0"}`, false},
		{"unknown tool name", `{"name": "kubectl", "arguments": {"ns": "prod"}}`, false},
		{"arguments without a name", `{"arguments": {"msg": "hi"}}`, false},
		{"unparseable brace soup", "func f() { if x { return } }", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeToolCallJSON(tc.text, names); got != tc.want {
				t.Errorf("looksLikeToolCallJSON(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
