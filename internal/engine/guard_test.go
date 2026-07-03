package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/tool"
)

// scriptAdapter returns one text response per Stream call, in order.
type scriptAdapter struct {
	replies []string
	i       int
}

func (a *scriptAdapter) Name() string { return "script" }
func (a *scriptAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	text := "done"
	if a.i < len(a.replies) {
		text = a.replies[a.i]
	}
	a.i++
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}}
	close(ch)
	return ch, nil
}

func runWith(t *testing.T, opts Options) []Event {
	t.Helper()
	eng, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	var got []Event
	if err := eng.Run(context.Background(), conv, func(ev Event) { got = append(got, ev) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	return got
}

func kinds(evs []Event) []EventKind {
	var k []EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func TestGuardPassEmitsDoneOnly(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"final"}}, Model: "m",
		OutputGuard:           func(context.Context, string) (bool, string) { return true, "" },
		OutputGuardMaxRetries: 2,
	})
	for _, k := range kinds(evs) {
		if k == KindGuard {
			t.Error("passing guard should emit no KindGuard event")
		}
	}
}

func TestGuardFailThenPass(t *testing.T) {
	calls := 0
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"bad", "good"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard: func(_ context.Context, text string) (bool, string) {
			calls++
			if text == "good" {
				return true, ""
			}
			return false, "needs work"
		},
	})
	var guardEvents int
	for _, e := range evs {
		if e.Kind == KindGuard {
			guardEvents++
			if e.GuardReason != "needs work" {
				t.Errorf("guard reason = %q", e.GuardReason)
			}
		}
	}
	if guardEvents != 1 {
		t.Errorf("expected 1 KindGuard event, got %d", guardEvents)
	}
	if calls != 2 {
		t.Errorf("expected guard called twice, got %d", calls)
	}
}

// TestGuardCorrectiveMentionsToolsAfterToolRound is a regression for a real
// failure mode: a model asked to "write a document" calls write_file (with
// placeholder/TODO content), then gives a final chat answer that itself
// mentions the unfinished placeholders, failing the "no placeholders or
// TODOs" default output-guard rubric. The old corrective prompt ("Revise and
// produce a corrected final answer") only asked for better *phrasing*, so the
// model could satisfy it with another content-free reply like "Let me do
// this properly" without ever re-calling a tool to fix the actual file. The
// corrective prompt must tell the model to use its tools when a tool round
// already happened this run.
func TestGuardCorrectiveMentionsToolsAfterToolRound(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: writes a file via a tool call.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"draft with TODO"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 2: final answer that fails the guard (mentions TODOs).
		{
			{Type: provider.EventTextDelta, Text: "Done, but there are still some TODOs to fill in."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 3: corrected final answer.
		{
			{Type: provider.EventTextDelta, Text: "Done, the document is complete."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	eng, err := New(Options{
		Adapter: adapter, Tools: reg, Model: "test",
		OutputGuardMaxRetries: 1,
		OutputGuard: func(_ context.Context, text string) (bool, string) {
			if strings.Contains(text, "TODO") {
				return false, "contains TODOs"
			}
			return true, ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "write a document"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	var correctiveText string
	for _, m := range conv.Messages {
		if m.Role != provider.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if tb, ok := b.(provider.TextBlock); ok && strings.Contains(tb.Text, "did not pass output validation") {
				correctiveText = tb.Text
			}
		}
	}
	if correctiveText == "" {
		t.Fatal("expected a corrective message to be appended to the conversation")
	}
	if !strings.Contains(correctiveText, "call your file tools now") {
		t.Errorf("expected corrective prompt to direct tool use after a tool round, got: %q", correctiveText)
	}
	if !strings.Contains(correctiveText, "Do not reply with only an acknowledgment") {
		t.Errorf("expected corrective prompt to forbid a content-free reply, got: %q", correctiveText)
	}
}

// TestGuardCorrectiveOmitsToolMentionWithoutToolRound verifies the
// tool-specific instruction is only added when a tool round actually
// happened this run — a plain text-only conversation has nothing to "fix" via
// file tools.
func TestGuardCorrectiveOmitsToolMentionWithoutToolRound(t *testing.T) {
	eng, err := New(Options{
		Adapter: &scriptAdapter{replies: []string{"bad", "good"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard: func(_ context.Context, text string) (bool, string) {
			if text == "good" {
				return true, ""
			}
			return false, "needs work"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	for _, m := range conv.Messages {
		if m.Role != provider.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if tb, ok := b.(provider.TextBlock); ok && strings.Contains(tb.Text, "did not pass output validation") {
				if strings.Contains(tb.Text, "call your file tools now") {
					t.Errorf("did not expect tool-use instruction with no prior tool round, got: %q", tb.Text)
				}
			}
		}
	}
}

func TestGuardExhaustedSurfaces(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"a", "b", "c", "d"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard:           func(context.Context, string) (bool, string) { return false, "always bad" },
	})
	var guardEvents, doneEvents int
	for _, e := range evs {
		switch e.Kind {
		case KindGuard:
			guardEvents++
		case KindDone:
			doneEvents++
		}
	}
	// 2 retries => 2 failure events on retries + 1 final exhausted event = 3.
	if guardEvents != 3 {
		t.Errorf("expected 3 KindGuard events, got %d", guardEvents)
	}
	if doneEvents != 1 {
		t.Errorf("expected exactly 1 KindDone, got %d", doneEvents)
	}
}
