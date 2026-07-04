package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
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

// TestLoopDetectorCatchesAlternatingPair is the P9 regression: a model
// alternating between two distinct tool calls (A, B, A, B, …) is just as
// stuck as one repeating a single call, but the original "last N signatures
// are all identical" check never fired on it.
func TestLoopDetectorCatchesAlternatingPair(t *testing.T) {
	d := newLoopDetector(5)
	seq := []string{"a", "b", "a", "b", "a", "b"}
	var tripped bool
	for i, s := range seq {
		if d.record(s) {
			tripped = true
			if i < 3 {
				t.Fatalf("tripped too early at index %d, before two full A,B repeats", i)
			}
			break
		}
	}
	if !tripped {
		t.Fatal("alternating A,B,A,B,... should eventually trip the detector")
	}
}

// TestLoopDetectorDoesNotFlagVariedWork is the false-positive counterpart: a
// detector generalized to catch cycles must not start flagging ordinary,
// non-repeating work (e.g. reading a series of different files).
func TestLoopDetectorDoesNotFlagVariedWork(t *testing.T) {
	d := newLoopDetector(5)
	seq := []string{"read a", "read b", "read c", "read d", "read e", "read f", "read g", "read h"}
	for _, s := range seq {
		if d.record(s) {
			t.Fatalf("varied, non-repeating signatures must not trip the detector (at %q)", s)
		}
	}
}

// TestTurnSignatureIgnoresVolatileNonce is the P9 regression for exact-string
// matching being defeated by a single varying byte: two otherwise-identical
// tool calls that differ only in a timestamp/nonce-shaped field must still
// produce the same loop-detection signature.
func TestTurnSignatureIgnoresVolatileNonce(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
	}{
		{"timestamp", `{"cmd":"ls","ts":"2026-07-04T10:00:00Z"}`, `{"cmd":"ls","ts":"2026-07-04T10:00:01Z"}`},
		{"uuid", `{"cmd":"ls","id":"550e8400-e29b-41d4-a716-446655440000"}`, `{"cmd":"ls","id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8"}`},
		{"epoch", `{"cmd":"ls","nonce":1720094400123}`, `{"cmd":"ls","nonce":1720094401456}`},
		{"hex_nonce", `{"cmd":"ls","nonce":"a1b2c3d4e5f60718"}`, `{"cmd":"ls","nonce":"0011223344556677"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := turnSignature([]provider.ToolUseBlock{{Name: "shell", Input: json.RawMessage(tc.first)}})
			b := turnSignature([]provider.ToolUseBlock{{Name: "shell", Input: json.RawMessage(tc.second)}})
			if a != b {
				t.Errorf("signatures should match once the volatile field is normalized, got %q vs %q", a, b)
			}
		})
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
