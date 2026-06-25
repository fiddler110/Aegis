package engine

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/tool"
)

// scriptedAdapter returns a predefined event sequence for each successive call.
type scriptedAdapter struct {
	turns [][]provider.Event
	calls int
}

func (s *scriptedAdapter) Name() string { return "scripted" }

func (s *scriptedAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	events := s.turns[s.calls]
	s.calls++
	ch := make(chan provider.Event, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// echoTool returns its "msg" argument back as text.
type echoTool struct{ called int }

func (e *echoTool) Name() string                { return "echo" }
func (e *echoTool) Description() string          { return "echo the msg argument" }
func (e *echoTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (e *echoTool) Capability() tool.Capability  { return tool.CapRead }
func (e *echoTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	e.called++
	var args struct {
		Msg string `json:"msg"`
	}
	_ = json.Unmarshal(input, &args)
	return tool.Result{Content: "echo:" + args.Msg}, nil
}

func TestRunToolRoundTrip(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: assistant asks to call the echo tool.
		{
			{Type: provider.EventTextDelta, Text: "let me check"},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 5}},
		},
		// Turn 2: assistant produces the final answer.
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 20, OutputTokens: 3}},
		},
	}}

	reg := tool.NewRegistry()
	et := &echoTool{}
	if err := reg.Register(et); err != nil {
		t.Fatal(err)
	}

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var kinds []EventKind
	var finalText string
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})

	err = eng.Run(context.Background(), conv, func(ev Event) {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == KindText {
			finalText += ev.Text
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if et.called != 1 {
		t.Errorf("echo tool called %d times, want 1", et.called)
	}
	if finalText != "let me checkdone" {
		t.Errorf("accumulated text = %q", finalText)
	}
	// user + assistant(turn1) + tool_result(user) + assistant(turn2)
	if len(conv.Messages) != 4 {
		t.Fatalf("conversation has %d messages, want 4", len(conv.Messages))
	}
	if conv.Messages[3].Role != provider.RoleAssistant {
		t.Errorf("final message role = %s, want assistant", conv.Messages[3].Role)
	}
	if !slices.Contains(kinds, KindToolCall) || !slices.Contains(kinds, KindToolResult) || !slices.Contains(kinds, KindDone) {
		t.Errorf("missing expected event kinds, got %v", kinds)
	}
}

func TestRunUnknownTool(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "nope", Input: json.RawMessage(`{}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "recovered"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}
	reg := tool.NewRegistry()
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test"})

	var gotErrResult bool
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindToolResult && ev.ToolIsError {
			gotErrResult = true
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !gotErrResult {
		t.Error("expected an error tool result for the unknown tool")
	}
}

// recordingHook vetoes a named tool and counts post-call invocations.
type recordingHook struct {
	veto      string
	postCalls int
}

func (h *recordingHook) PreToolUse(_ context.Context, name string, _ json.RawMessage) error {
	if name == h.veto {
		return errInterruptHook
	}
	return nil
}
func (h *recordingHook) PostToolUse(context.Context, string, json.RawMessage, string, bool) {
	h.postCalls++
}

var errInterruptHook = &hookErr{}

type hookErr struct{}

func (*hookErr) Error() string { return "blocked" }

func TestRunHookVeto(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "ok"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}
	reg := tool.NewRegistry()
	et := &echoTool{}
	_ = reg.Register(et)
	hook := &recordingHook{veto: "echo"}
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Hooks: hook, Model: "test"})

	var blocked bool
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindToolResult && ev.ToolIsError {
			blocked = true
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !blocked {
		t.Error("expected vetoed tool to return an error result")
	}
	if et.called != 0 {
		t.Errorf("vetoed tool should not execute, called %d", et.called)
	}
	if hook.postCalls != 0 {
		t.Errorf("PostToolUse should not run for a vetoed call, got %d", hook.postCalls)
	}
}

func TestRunInterrupt(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{{Type: provider.EventTextDelta, Text: "x"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}}
	eng, _ := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Model: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conv := &Conversation{}
	err := eng.Run(ctx, conv, nil)
	if err != ErrInterrupted {
		t.Errorf("err = %v, want ErrInterrupted", err)
	}
}

