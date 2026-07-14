package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

func TestLooksActionable(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"fix the bug in auth.go", true},
		{"Please implement a retry loop for the client", true},
		{"Could you add error handling to the parser?", true},
		{"Can you run the tests and report back", true},
		{"write a function that reverses a string", true},
		{"refactor the session store to use a pool", true},
		{"", false},
		{"what does this function do?", false},
		{"why is the build failing", false},
		{"hi", false},
		{"thanks, that looks good", false},
	}
	for _, c := range cases {
		if got := looksActionable(c.text); got != c.want {
			t.Errorf("looksActionable(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// TestZeroToolNudgeRetriesOnActionableTextOnlyTurn is the P28.3 regression:
// a model responding text-only to a plainly actionable request — the
// deepseek-r1:8b live-eval failure mode — must be nudged to reconsider and
// actually call a tool, rather than the run silently accepting the text-only
// turn as done.
func TestZeroToolNudgeRetriesOnActionableTextOnlyTurn(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: text-only "reasoning dump" with no tool call.
		{
			{Type: provider.EventTextDelta, Text: "I would fix this by editing the file to add a nil check."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 2: the nudge lands, model calls the tool.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"fixed"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 3: final answer.
		{
			{Type: provider.EventTextDelta, Text: "Fixed the nil check."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	capture := &reqCapturingAdapter{inner: adapter}
	eng, err := New(Options{Adapter: capture, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil pointer bug in parser.go"},
	}})

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if adapter.calls != 3 {
		t.Fatalf("expected 3 model calls (text-only, nudge retry, final), got %d", adapter.calls)
	}

	var sawNudge bool
	for _, req := range capture.reqs {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if tb, ok := b.(provider.TextBlock); ok && strings.HasPrefix(tb.Text, zeroToolNudgePrefix) {
					sawNudge = true
				}
			}
		}
	}
	if !sawNudge {
		t.Error("expected a nudge message in a later request")
	}
	if len(notices) != 1 {
		t.Errorf("expected 1 KindNotice event for the nudge, got %d: %v", len(notices), notices)
	}

	// Scaffolding must be retracted once the run settles: only the original
	// user message, the tool round, and the final answer should remain.
	for _, m := range conv.Messages {
		for _, b := range m.Content {
			if tb, ok := b.(provider.TextBlock); ok {
				if strings.HasPrefix(tb.Text, zeroToolNudgePrefix) {
					t.Errorf("nudge scaffolding survived retraction: %q", tb.Text)
				}
				if strings.Contains(tb.Text, "I would fix this by editing") {
					t.Errorf("text-only failed turn survived retraction: %q", tb.Text)
				}
			}
		}
	}
	if final := assistantText(conv.Messages[len(conv.Messages)-1]); final != "Fixed the nil check." {
		t.Errorf("final message = %q, want the corrected answer", final)
	}
}

// TestZeroToolNudgeSkippedOnNonActionablePrompt verifies a plain question gets
// no nudge at all — the model's text-only answer is simply accepted, matching
// pre-P28.3 behavior.
func TestZeroToolNudgeSkippedOnNonActionablePrompt(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "It's a stack-based data structure."},
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
		provider.TextBlock{Text: "what is a stack?"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != 1 {
		t.Errorf("expected 1 model call (no nudge retry), got %d", adapter.calls)
	}
}

// TestZeroToolNudgeSkippedWithoutTools verifies the nudge never fires when no
// tools are registered — there is nothing actionable it could ask the model
// to call.
func TestZeroToolNudgeSkippedWithoutTools(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "I would fix this by editing the file."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}
	eng, err := New(Options{Adapter: adapter, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil pointer bug in parser.go"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != 1 {
		t.Errorf("expected 1 model call (no tools registered, no nudge), got %d", adapter.calls)
	}
}

// TestZeroToolNudgeExhaustedSurfacesTextAnswer verifies that once the nudge
// retry is used and the model still produces zero tool calls, the run
// surfaces the text-only answer rather than looping forever.
func TestZeroToolNudgeExhaustedSurfacesTextAnswer(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "I would fix this by editing the file."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
		{
			{Type: provider.EventTextDelta, Text: "Again, I would edit the file to add a check."},
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
		provider.TextBlock{Text: "fix the nil pointer bug in parser.go"},
	}})
	var doneEvents int
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindDone {
			doneEvents++
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != 2 {
		t.Fatalf("expected 2 model calls (initial + 1 nudge retry), got %d", adapter.calls)
	}
	if doneEvents != 1 {
		t.Errorf("expected exactly 1 KindDone, got %d", doneEvents)
	}
	if final := assistantText(conv.Messages[len(conv.Messages)-1]); final != "Again, I would edit the file to add a check." {
		t.Errorf("final message = %q, want the surfaced second attempt", final)
	}
}

// TestZeroToolNudgeDisabledByNegativeOption verifies a negative
// ZeroToolNudgeMaxRetries fully disables the feature.
func TestZeroToolNudgeDisabledByNegativeOption(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "I would fix this by editing the file."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", ZeroToolNudgeMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil pointer bug in parser.go"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != 1 {
		t.Errorf("expected 1 model call (nudge disabled), got %d", adapter.calls)
	}
}

// TestZeroToolNudgeSkippedAfterToolRound verifies the nudge only ever fires
// before any tool round has completed this run — a text-only turn that comes
// after real tool use (e.g. a wrap-up summary) is a legitimate final answer,
// not a suspicious zero-tool-call completion.
func TestZeroToolNudgeSkippedAfterToolRound(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"looking"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		{
			{Type: provider.EventTextDelta, Text: "I would fix this by editing the file next."},
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
		provider.TextBlock{Text: "fix the nil pointer bug in parser.go"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != 2 {
		t.Errorf("expected 2 model calls (tool round + final text, no nudge), got %d", adapter.calls)
	}
}
