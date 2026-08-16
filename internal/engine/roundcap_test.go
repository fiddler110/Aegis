package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// bulkTool returns a fixed-size payload, so a round of N calls to it lands N
// times that size in one message — the P67.1 shape.
type bulkTool struct{ size int }

func (bulkTool) Name() string                 { return "bulk" }
func (bulkTool) Description() string          { return "returns a large fixed payload" }
func (bulkTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (bulkTool) Capability() tool.Capability  { return tool.CapRead }
func (b bulkTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: strings.Repeat("x", b.size)}, nil
}

// roundOf builds one assistant turn requesting n bulk calls, which the engine
// dispatches concurrently (n > 1 takes the parallel path).
func roundOf(n int) []provider.Event {
	evs := make([]provider.Event, 0, n+1)
	for i := 0; i < n; i++ {
		evs = append(evs, provider.Event{
			Type:    provider.EventToolUse,
			ToolUse: &provider.ToolUseBlock{ID: fmt.Sprintf("tu-%d", i), Name: "bulk", Input: json.RawMessage(`{}`)},
		})
	}
	return append(evs, provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse})
}

// engineForRound builds an engine over a bulk-tool registry with the given cap
// hook, and returns it alongside the conversation to run.
func engineForRound(t *testing.T, size int, calls int, cap RoundCapFunc) (*Engine, *Conversation) {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(bulkTool{size: size}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{
		Adapter:        &scriptedAdapter{turns: [][]provider.Event{roundOf(calls), endTurn()}},
		Tools:          reg,
		Model:          "test",
		RoundResultCap: cap,
	})
	if err != nil {
		t.Fatal(err)
	}
	return eng, &Conversation{System: "sys"}
}

// resultSizes pulls the tool_result sizes out of a finished conversation, which is
// the quantity this bound is about: what the *model* is sent, not what the tool
// produced.
func resultSizes(conv *Conversation) []int {
	var out []int
	for _, m := range conv.Messages {
		for _, blk := range m.Content {
			if tr, ok := blk.(provider.ToolResultBlock); ok {
				out = append(out, len(tr.Content))
			}
		}
	}
	return out
}

// TestRoundResultCapAppliesToAParallelRound is P67.1 at the engine seam: the hook
// is handed the round's results in order and what it returns is what lands in the
// conversation.
func TestRoundResultCapAppliesToAParallelRound(t *testing.T) {
	const (
		size  = 10_000
		calls = 4
	)
	var gotSizes []int
	eng, conv := engineForRound(t, size, calls, func(_ context.Context, results []string) []string {
		gotSizes = nil
		out := make([]string, len(results))
		for i, r := range results {
			gotSizes = append(gotSizes, len(r))
			out[i] = r[:100] // a stand-in for the real policy
		}
		return out
	})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(gotSizes) != calls {
		t.Fatalf("hook saw %d results, want the round's %d", len(gotSizes), calls)
	}
	for i, n := range gotSizes {
		if n != size {
			t.Errorf("hook saw result %d at %d bytes, want the tool's own %d — the cap is not layered above the per-call caps", i, n, size)
		}
	}
	for i, n := range resultSizes(conv) {
		if n != 100 {
			t.Errorf("conversation result %d is %d bytes, want the capped 100", i, n)
		}
	}
}

// TestRoundResultCapPreservesToolUseIDs: the results are rewritten in place, so
// every tool_result must still carry the id of the call it answers. Getting this
// wrong produces a request every provider rejects, which is a worse failure than
// the unbounded round the cap exists to prevent.
func TestRoundResultCapPreservesToolUseIDs(t *testing.T) {
	eng, conv := engineForRound(t, 5_000, 3, func(_ context.Context, results []string) []string {
		out := make([]string, len(results))
		for i := range results {
			out[i] = "trimmed"
		}
		return out
	})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var ids []string
	for _, m := range conv.Messages {
		for _, blk := range m.Content {
			if tr, ok := blk.(provider.ToolResultBlock); ok {
				ids = append(ids, tr.ToolUseID)
				if tr.Content != "trimmed" {
					t.Errorf("result for %s was not capped: %q", tr.ToolUseID, tr.Content)
				}
			}
		}
	}
	want := []string{"tu-0", "tu-1", "tu-2"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("tool_result ids after capping = %v, want %v", ids, want)
	}
}

// TestRoundResultCapDoesNotChangeWhatTheUserSaw: the cap runs after emission
// deliberately. The human has already seen each tool's full output and the trace
// records what ran; trimming before emission would hide output from the user to
// save the model's context, which is the wrong trade in both directions.
func TestRoundResultCapDoesNotChangeWhatTheUserSaw(t *testing.T) {
	const size = 8_000
	eng, conv := engineForRound(t, size, 3, func(_ context.Context, results []string) []string {
		out := make([]string, len(results))
		for i := range results {
			out[i] = "trimmed"
		}
		return out
	})
	var emitted []int
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindToolResult {
			emitted = append(emitted, len(ev.ToolResult))
		}
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, n := range emitted {
		if n != size {
			t.Errorf("emitted result %d is %d bytes, want the full %d — the cap must not reach the UI", i, n, size)
		}
	}
}

// TestRoundResultCapIgnoresAMiscountingHook: a hook that returns the wrong number
// of results is refused rather than trusted, because applying it would pair a
// tool_result with the wrong tool_use.
func TestRoundResultCapIgnoresAMiscountingHook(t *testing.T) {
	const size = 4_000
	eng, conv := engineForRound(t, size, 3, func(_ context.Context, results []string) []string {
		return []string{"only one"}
	})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, n := range resultSizes(conv) {
		if n != size {
			t.Errorf("result %d is %d bytes; a miscounting hook must be ignored, not applied", i, n)
		}
	}
}

// TestNoRoundResultCapKeepsThePreP671Behaviour: nil is the zero value every
// embedder and test gets, and it must leave each result bounded by its own cap
// with nothing bounding the sum.
func TestNoRoundResultCapKeepsThePreP671Behaviour(t *testing.T) {
	const size = 6_000
	eng, conv := engineForRound(t, size, 4, nil)
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, n := range resultSizes(conv) {
		if n != size {
			t.Errorf("result %d is %d bytes with no cap configured, want the tool's own %d", i, n, size)
		}
	}
}

// TestRoundResultCapSkipsASingleCallRound: a round of one takes the sequential
// path and is already bounded by its tool's own cap. The hook is not called, so a
// caller cannot accidentally override a per-call posture through it.
func TestRoundResultCapSkipsASingleCallRound(t *testing.T) {
	var called bool
	eng, conv := engineForRound(t, 50_000, 1, func(_ context.Context, results []string) []string {
		called = true
		return results
	})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Error("the round cap ran on a single-call round")
	}
}
