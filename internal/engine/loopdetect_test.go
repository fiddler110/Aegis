package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/scottymacleod/agentharness/internal/provider"
	"github.com/scottymacleod/agentharness/internal/tool"
)

func TestLoopDetectorRecord(t *testing.T) {
	d := newLoopDetector(3)
	if d.record("a") || d.record("b") {
		t.Fatal("no loop expected before the window fills")
	}
	if d.record("a") {
		t.Fatal("differing signatures must not trip the detector")
	}
	// Three identical in a row trips threshold 3 on the third.
	d2 := newLoopDetector(3)
	if got := d2.record("x"); got {
		t.Fatal("first signature should not trip")
	}
	if got := d2.record("x"); got {
		t.Fatal("second signature should not trip")
	}
	if got := d2.record("x"); !got {
		t.Fatal("third identical signature should trip the detector")
	}
}

func TestTurnSignatureDistinguishesInputs(t *testing.T) {
	a := turnSignature([]provider.ToolUseBlock{{Name: "read", Input: json.RawMessage(`{"p":"a"}`)}})
	b := turnSignature([]provider.ToolUseBlock{{Name: "read", Input: json.RawMessage(`{"p":"b"}`)}})
	if a == b {
		t.Error("different inputs should yield different signatures")
	}
	same := turnSignature([]provider.ToolUseBlock{{Name: "read", Input: json.RawMessage(`{"p":"a"}`)}})
	if a != same {
		t.Error("identical calls should yield identical signatures")
	}
}

// TestEngineAbortsOnLoop runs an adapter that requests the same tool call every
// turn and asserts the engine bails out before maxIterations.
func TestEngineAbortsOnLoop(t *testing.T) {
	loopTurn := []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
		{Type: provider.EventDone, Stop: provider.StopToolUse},
	}
	turns := make([][]provider.Event, 10)
	for i := range turns {
		turns[i] = loopTurn
	}
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	et := &echoTool{}
	_ = reg.Register(et)
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test", LoopThreshold: 3})

	var gotErr error
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	})

	if err == nil || gotErr == nil {
		t.Fatal("expected the run to abort with a loop error")
	}
	if !strings.Contains(gotErr.Error(), "loop") {
		t.Errorf("expected a loop error, got %v", gotErr)
	}
	// threshold 3 => 3 looping turns execute the tool, then abort on the 3rd
	// before the 4th model call. Far fewer than maxIterations (25).
	if adapter.calls > 3 {
		t.Errorf("expected abort by the 3rd turn, made %d model calls", adapter.calls)
	}
}
