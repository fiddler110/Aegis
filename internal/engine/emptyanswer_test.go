package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestEmptyAnswerNudgeRecoversTextlessRun is the P34.1 regression: a run whose
// tool rounds succeed but whose final turn carries no user-visible text at all
// (observed live with gpt-oss:20b, which emits its conclusion into the thinking
// channel and stops) must be nudged once for a plain-text answer rather than
// completing "successfully" with an empty reply.
func TestEmptyAnswerNudgeRecoversTextlessRun(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: a real tool round.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"looked"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 2: ends the turn with thinking only — zero user-visible text.
		{
			{Type: provider.EventThinkingDelta, Text: "The answer is clearly foo."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 3: the nudge lands and the model finally speaks.
		{
			{Type: provider.EventTextDelta, Text: "The answer is foo."},
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
		provider.TextBlock{Text: "what does the parser do?"},
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
		t.Fatalf("expected 3 model calls (tool round, text-less turn, nudge retry), got %d", adapter.calls)
	}

	var sawNudge bool
	for _, req := range capture.reqs {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if tb, ok := b.(provider.TextBlock); ok && strings.HasPrefix(tb.Text, emptyAnswerNudgePrefix) {
					sawNudge = true
				}
			}
		}
	}
	if !sawNudge {
		t.Error("expected an empty-answer nudge message in a later request")
	}

	if final := assistantText(conv.Messages[len(conv.Messages)-1]); final != "The answer is foo." {
		t.Errorf("final message = %q, want the recovered answer", final)
	}
	// Scaffolding must not survive into the durable transcript.
	for _, m := range conv.Messages {
		for _, b := range m.Content {
			if tb, ok := b.(provider.TextBlock); ok && strings.HasPrefix(tb.Text, emptyAnswerNudgePrefix) {
				t.Errorf("nudge scaffolding survived retraction: %q", tb.Text)
			}
		}
	}
}

// TestEmptyAnswerNudgeFiresOnceThenNotifies covers the stubborn-model case: the
// nudge is bounded to a single attempt, and when it too yields nothing the run
// explains the empty reply instead of ending silently.
func TestEmptyAnswerNudgeFiresOnceThenNotifies(t *testing.T) {
	textless := []provider.Event{
		{Type: provider.EventThinkingDelta, Text: "thinking, not speaking"},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{textless, textless, textless}}

	eng, err := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "what does the parser do?"},
	}})

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Exactly one nudge: the text-less turn, one retry, then give up.
	if adapter.calls != 2 {
		t.Fatalf("expected exactly 2 model calls (text-less turn + one bounded nudge), got %d", adapter.calls)
	}
	var explained bool
	for _, n := range notices {
		if strings.Contains(n, "no text") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("expected a notice naming the empty-reply condition, got %v", notices)
	}
}

// TestEmptyAnswerNudgeSkippedWhenTextPresent guards the common path: a turn
// that produced real text must never trigger the nudge.
func TestEmptyAnswerNudgeSkippedWhenTextPresent(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "It's a stack-based data structure."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	eng, err := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Model: "test"})
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
		t.Fatalf("expected 1 model call, got %d", adapter.calls)
	}
}

// TestEmptyAnswerNudgeTurnSuppressesThinking is the P79.4 regression test.
//
// The P34.1 nudge above exists because a reasoning model put its whole reply
// in the thinking channel and stopped. Re-asking over the same channel
// reproduces the same turn — measured against aegis-qwen35-9b on the first
// live_workflow run (C2), where the nudge came back empty too and the run
// ended on "model produced no text even after being asked". Only the nudge
// turn suppresses thinking: the model has already done whatever reasoning it
// was going to do, and what is left is to say it. Every other turn keeps the
// preamble, because that is where thinking earns its keep.
func TestEmptyAnswerNudgeTurnSuppressesThinking(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: thinking only, no visible text — this is what triggers the nudge.
		{
			{Type: provider.EventThinkingDelta, Text: "The answer is clearly foo."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 2: the nudge turn, which must have arrived with thinking off.
		{
			{Type: provider.EventTextDelta, Text: "The answer is foo."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	capture := &reqCapturingAdapter{inner: adapter}
	eng, err := New(Options{Adapter: capture, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "what does the parser do?"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(capture.reqs) != 2 {
		t.Fatalf("got %d requests, want 2 (the empty turn and its nudge)", len(capture.reqs))
	}
	if capture.reqs[0].SuppressThinking {
		t.Error("the user's own turn suppressed thinking; only the nudge turn may")
	}
	if !capture.reqs[1].SuppressThinking {
		t.Error("the nudge turn did not suppress thinking, so it re-asks over the channel that produced the empty reply")
	}
	// Guard against matching by accident: the second request must actually be
	// the nudge.
	if last := lastUserText(capture.reqs[1].Messages); !strings.HasPrefix(last, emptyAnswerNudgePrefix) {
		t.Errorf("second request's last user message = %q, want the empty-answer nudge", last)
	}
}
