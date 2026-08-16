package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/trace"
)

// collectTraces runs an engine and returns every turn trace it emitted, which is
// the record P66.11 widened. One per turn is itself an assertion: the final-answer
// branch now emits at the end rather than the top, so a missed exit would show up
// here as a lost turn.
func collectTraces(t *testing.T, eng *Engine, conv *Conversation) []trace.TurnTrace {
	t.Helper()
	var out []trace.TurnTrace
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindTrace && ev.Trace != nil {
			out = append(out, *ev.Trace)
		}
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out
}

// TestTraceCarriesRunIDAndStopReason is GAP-01's cheapest half: both values were
// already in hand at the emit site and thrown away, and both answer a question the
// old record could not — which request produced this turn, and why the turn ended.
func TestTraceCarriesRunIDAndStopReason(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{
		Adapter: &scriptedAdapter{turns: [][]provider.Event{
			{
				{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
				{Type: provider.EventDone, Stop: provider.StopToolUse},
			},
			endTurn(),
		}},
		Tools: reg,
		Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	traces := collectTraces(t, eng, &Conversation{System: "sys"})

	if len(traces) != 2 {
		t.Fatalf("got %d traces, want one per turn (2)", len(traces))
	}
	if traces[0].RunID == "" {
		t.Error("no run id on the first turn")
	}
	if traces[0].RunID != traces[1].RunID {
		t.Errorf("run ids differ within one run: %q vs %q", traces[0].RunID, traces[1].RunID)
	}
	if got := traces[0].StopReason; got != string(provider.StopToolUse) {
		t.Errorf("tool round stop reason = %q, want %q", got, provider.StopToolUse)
	}
	if got := traces[1].StopReason; got != string(provider.StopEndTurn) {
		t.Errorf("final turn stop reason = %q, want %q", got, provider.StopEndTurn)
	}
}

// TestRunIDsDifferBetweenRuns: the id identifies a run, not an engine. A session
// accumulates the traces of many requests, and an id shared across them would be
// no better than the field's absence.
func TestRunIDsDifferBetweenRuns(t *testing.T) {
	eng, err := New(Options{
		Adapter: &scriptedAdapter{turns: [][]provider.Event{endTurn(), endTurn()}},
		Model:   "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	first := collectTraces(t, eng, &Conversation{System: "sys"})
	second := collectTraces(t, eng, &Conversation{System: "sys"})
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("no traces")
	}
	if first[0].RunID == second[0].RunID {
		t.Errorf("both runs report run id %q", first[0].RunID)
	}
}

// TestTraceRecordsTheMaxTokensContinuation is the retry record (GAP-01). A run
// that burns through its iterations on continuation turns used to be
// indistinguishable, in the persisted record, from one that did the same work in
// the same number of ordinary turns.
func TestTraceRecordsTheMaxTokensContinuation(t *testing.T) {
	eng, err := New(Options{
		Adapter: &scriptedAdapter{turns: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "half an ans"},
				{Type: provider.EventDone, Stop: provider.StopMaxTokens},
			},
			endTurn(),
		}},
		Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	traces := collectTraces(t, eng, &Conversation{System: "sys"})
	if len(traces) != 2 {
		t.Fatalf("got %d traces, want 2", len(traces))
	}
	if got := traces[0].Correctives; len(got) != 1 || got[0] != "max_tokens_continuation" {
		t.Errorf("first turn correctives = %v, want [max_tokens_continuation]", got)
	}
	if got := traces[1].Correctives; len(got) != 0 {
		t.Errorf("the answering turn records correctives %v, want none", got)
	}
}

// TestTraceRecordsTheGuardVerdict: the verdict already rode a KindGuard event that
// nothing persisted, so after the fact "did the guard pass, fail, or fail open"
// was unanswerable on the one turn of a run where it matters most.
//
// Both outcomes are covered, because a pass and a retry take different exits from
// the final-answer branch and the trace has to survive each.
func TestTraceRecordsTheGuardVerdict(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		eng, err := New(Options{
			Adapter: &scriptedAdapter{turns: [][]provider.Event{endTurn()}},
			Model:   "test",
			OutputGuard: func(context.Context, guard.Input) (bool, string, guard.Status) {
				return true, "", guard.StatusPassed
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		traces := collectTraces(t, eng, &Conversation{System: "sys"})
		if len(traces) != 1 {
			t.Fatalf("got %d traces, want 1", len(traces))
		}
		g := traces[0].Guard
		if g == nil {
			t.Fatal("no guard verdict on the traced turn")
		}
		if !g.Passed || g.Status != string(guard.StatusPassed) {
			t.Errorf("verdict = %+v, want a genuine pass", g)
		}
	})

	t.Run("fail then retry", func(t *testing.T) {
		var calls int
		eng, err := New(Options{
			Adapter: &scriptedAdapter{turns: [][]provider.Event{endTurn(), endTurn()}},
			Model:   "test",
			OutputGuard: func(context.Context, guard.Input) (bool, string, guard.Status) {
				calls++
				if calls == 1 {
					return false, "missing the required section", guard.StatusFailed
				}
				return true, "", guard.StatusPassed
			},
			OutputGuardMaxRetries: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		traces := collectTraces(t, eng, &Conversation{System: "sys"})
		if len(traces) != 2 {
			t.Fatalf("got %d traces, want one per turn (2)", len(traces))
		}
		first := traces[0].Guard
		if first == nil || first.Passed || !first.Retrying {
			t.Fatalf("first turn verdict = %+v, want a failure that retried", first)
		}
		if !strings.Contains(first.Reason, "missing the required section") {
			t.Errorf("verdict reason = %q, want the guard's own reason", first.Reason)
		}
		if got := traces[0].Correctives; len(got) != 1 || got[0] != "guard" {
			t.Errorf("first turn correctives = %v, want [guard]", got)
		}
		if second := traces[1].Guard; second == nil || !second.Passed {
			t.Fatalf("second turn verdict = %+v, want a pass", second)
		}
	})
}

// TestTraceRecordsTheCompactionEvent is the field the measurement items actually
// need: LLM-02's closure condition is "the turn at which compaction actually
// fires", and before this the only way to answer it was to read Info logs.
func TestTraceRecordsTheCompactionEvent(t *testing.T) {
	const window = 4_000
	eng, err := New(Options{
		Adapter:             &scriptedAdapter{turns: [][]provider.Event{endTurn()}},
		Compactor:           &noticeCompactor{},
		Model:               "test",
		MaxTokens:           100,
		ContextWindowTokens: window,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A conversation comfortably over the trigger, so beforeTurn attempts a
	// compaction on the very first turn.
	conv := &Conversation{System: "sys"}
	for i := 0; i < 20; i++ {
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
			provider.TextBlock{Text: strings.Repeat("filler words here ", 60)},
		}})
	}
	traces := collectTraces(t, eng, conv)
	if len(traces) == 0 {
		t.Fatal("no traces")
	}
	c := traces[0].Compaction
	if c == nil {
		t.Fatal("no compaction event on a turn that compacted")
	}
	if !c.Applied {
		t.Errorf("compaction event = %+v, want applied", c)
	}
	if c.Trigger != compactionTrigger(window, 100) {
		t.Errorf("recorded trigger %d, want the engine's %d", c.Trigger, compactionTrigger(window, 100))
	}
	if c.Estimate <= c.Trigger {
		t.Errorf("recorded estimate %d is not above the trigger %d — the event does not describe the decision", c.Estimate, c.Trigger)
	}
	if c.MessagesBefore <= c.MessagesAfter {
		t.Errorf("messages %d -> %d, want the rewrite recorded", c.MessagesBefore, c.MessagesAfter)
	}
}

// TestNoCompactionEventOnAQuietTurn: the event is nil when nothing happened, so a
// reader can tell "did not compact" from "compacted and freed nothing". A stale
// event carried from a previous turn would be worse than no field at all.
func TestNoCompactionEventOnAQuietTurn(t *testing.T) {
	eng, err := New(Options{
		Adapter:             &scriptedAdapter{turns: [][]provider.Event{endTurn()}},
		Compactor:           &noticeCompactor{},
		Model:               "test",
		MaxTokens:           100,
		ContextWindowTokens: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	traces := collectTraces(t, eng, bigConversation())
	if len(traces) == 0 {
		t.Fatal("no traces")
	}
	if c := traces[0].Compaction; c != nil {
		t.Errorf("compaction event %+v on a turn nowhere near the trigger", c)
	}
}
