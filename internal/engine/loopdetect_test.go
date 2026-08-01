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

// TestEngineAbortsOnErrorLoop runs an adapter that requests the same *failing*
// tool call every turn and asserts the engine bails out before maxIterations.
// This is the P53.2 fatal half: a model repeating a call that keeps erroring has
// exhausted its own recovery, so the outcome is exactly what it was before
// P53.2 split recoverable from fatal.
func TestEngineAbortsOnErrorLoop(t *testing.T) {
	turns := make([][]provider.Event, 10)
	for i := range turns {
		turns[i] = failTurn("t", `{"old":"x"}`)
	}
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	_ = reg.Register(&failEditTool{})
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
	if strings.Contains(gotErr.Error(), "corrective prompt") {
		t.Errorf("an error loop must abort on the first trigger, not after a nudge: %v", gotErr)
	}
	// threshold 3 => 3 looping turns execute the tool, then abort on the 3rd
	// before the 4th model call. Far fewer than maxIterations (25).
	if adapter.calls > 3 {
		t.Errorf("expected abort by the 3rd turn, made %d model calls", adapter.calls)
	}
	if n := countNudges(conv.Messages, loopNudgePrefix); n != 0 {
		t.Errorf("an error loop must not be nudged, got %d nudge(s)", n)
	}
}

// echoLoopTurn scripts one model turn that calls the always-succeeding echo tool
// with the same arguments every time — the P53.2 recoverable-loop shape.
func echoLoopTurn() []provider.Event {
	return []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: "t", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`),
		}},
		{Type: provider.EventDone, Stop: provider.StopToolUse},
	}
}

// TestEngineNudgesOnSucceedingLoop is the P53.2(b) recoverable half: a model
// repeating a *succeeding* call is confused, not broken, so the first trigger
// injects one corrective, resets the window, and lets the run continue — and if
// the model then answers, the run completes normally with the scaffolding
// retracted from the durable transcript.
func TestEngineNudgesOnSucceedingLoop(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		echoLoopTurn(), echoLoopTurn(), echoLoopTurn(),
		textTurn("Here is the answer."),
	}}

	reg := tool.NewRegistry()
	_ = reg.Register(&echoTool{})
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test", LoopThreshold: 3})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	var notices []string
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
		if ev.Kind == KindError {
			t.Errorf("unexpected error event: %v", ev.Err)
		}
	})
	if err != nil {
		t.Fatalf("a succeeding loop must be recoverable, got %v", err)
	}
	if adapter.calls != 4 {
		t.Errorf("expected the run to continue past the trigger (4 model calls), got %d", adapter.calls)
	}
	var found bool
	for _, n := range notices {
		if strings.Contains(n, "repeated the same succeeding tool calls") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a notice about the repeated succeeding calls, got %v", notices)
	}
	if n := countNudges(conv.Messages, loopNudgePrefix); n != 0 {
		t.Errorf("the loop nudge must be retracted once the run settles, found %d", n)
	}
}

// TestEngineAbortsOnSecondSucceedingLoop asserts the nudge is one-shot: a model
// that resumes the same succeeding cycle after being told to change approach
// ends the run, rather than being nudged again every threshold turns.
func TestEngineAbortsOnSecondSucceedingLoop(t *testing.T) {
	turns := make([][]provider.Event, 12)
	for i := range turns {
		turns[i] = echoLoopTurn()
	}
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	_ = reg.Register(&echoTool{})
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test", LoopThreshold: 3})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	var gotErr error
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	})
	if err == nil || gotErr == nil {
		t.Fatal("expected the second loop trigger to abort the run")
	}
	if !strings.Contains(gotErr.Error(), "corrective prompt did not break the cycle") {
		t.Errorf("expected the post-nudge abort message, got %v", gotErr)
	}
	// Trigger at turn 3 (nudge + window reset), second trigger at turn 6.
	if adapter.calls != 6 {
		t.Errorf("expected the window reset to buy exactly 3 more turns, got %d model calls", adapter.calls)
	}
	if n := countNudges(conv.Messages, loopNudgePrefix); n != 1 {
		t.Errorf("expected exactly one loop nudge, got %d", n)
	}
}

// TestLoopDetectorOutcomeClassification covers the record/noteOutcome pairing
// directly: the turn that trips the detector never has an outcome of its own, so
// the verdict comes from the earlier entries of the detected window.
func TestLoopDetectorOutcomeClassification(t *testing.T) {
	ok := newLoopDetector(3)
	ok.record("a")
	ok.noteOutcome(false)
	ok.record("a")
	ok.noteOutcome(false)
	if !ok.record("a") {
		t.Fatal("third identical signature should trip")
	}
	if ok.cycleHadError() {
		t.Error("a window of successful rounds must classify as a succeeding loop")
	}

	bad := newLoopDetector(3)
	bad.record("a")
	bad.noteOutcome(false)
	bad.record("a")
	bad.noteOutcome(true) // one failing round is enough
	if !bad.record("a") {
		t.Fatal("third identical signature should trip")
	}
	if !bad.cycleHadError() {
		t.Error("any errored round in the window must classify as an error loop")
	}
}

// TestLoopDetectorResetClearsWindow asserts a nudged run gets a genuinely fresh
// window — otherwise the corrective would be answered by an instant re-trigger.
func TestLoopDetectorResetClearsWindow(t *testing.T) {
	d := newLoopDetector(3)
	d.record("a")
	d.record("a")
	if !d.record("a") {
		t.Fatal("third identical signature should trip")
	}
	d.reset()
	if d.record("a") || d.record("a") {
		t.Fatal("after a reset the window must refill before tripping again")
	}
	if !d.record("a") {
		t.Fatal("three more identical signatures should trip again")
	}
	// noteOutcome on a freshly reset detector must not panic or misattribute.
	d.reset()
	d.noteOutcome(true)
	if d.cycleHadError() {
		t.Error("an empty window has no outcomes to report")
	}
}

// pollTool is a stand-in for a wait-shaped builtin (task_get, team_inbox): its
// repeated calls are legitimate, so it opts out of loop detection per call.
type pollTool struct{ called int }

func (p *pollTool) Name() string                 { return "poll" }
func (p *pollTool) Description() string          { return "poll external state" }
func (p *pollTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (p *pollTool) Capability() tool.Capability  { return tool.CapRead }
func (p *pollTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	p.called++
	return tool.Result{Content: "still running"}, nil
}

// PollExempt only claims the wait-shaped arguments, exercising the per-call
// contract rather than a blanket per-tool flag.
func (p *pollTool) PollExempt(input json.RawMessage) bool {
	var args struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return false
	}
	return args.Mode == "wait"
}

// TestTurnSignatureExcludesPollExemptCalls is the P53.2(a) unit: exempt calls
// leave no trace in the signature, so two turns differing only in their polls
// compare equal, and a turn of nothing but polls is not recorded at all (an
// empty signature would form its own perfect period-1 cycle).
func TestTurnSignatureExcludesPollExemptCalls(t *testing.T) {
	exempt := func(tu provider.ToolUseBlock) bool { return tu.Name == "poll" }

	mixed := []provider.ToolUseBlock{
		{Name: "poll", Input: json.RawMessage(`{"id":"j1"}`)},
		{Name: "read", Input: json.RawMessage(`{"p":"a"}`)},
	}
	sig, rec := turnSignatureExcludingPolls(mixed, exempt)
	if !rec {
		t.Fatal("a turn with a non-exempt call must still be recorded")
	}
	want, _ := turnSignatureExcludingPolls([]provider.ToolUseBlock{{Name: "read", Input: json.RawMessage(`{"p":"a"}`)}}, exempt)
	if sig != want {
		t.Errorf("poll-exempt calls must not appear in the signature: %q vs %q", sig, want)
	}
	if strings.Contains(sig, "poll") {
		t.Errorf("signature still mentions the exempt tool: %q", sig)
	}

	allPolls := []provider.ToolUseBlock{
		{Name: "poll", Input: json.RawMessage(`{"id":"j1"}`)},
		{Name: "poll", Input: json.RawMessage(`{"id":"j2"}`)},
	}
	if _, rec := turnSignatureExcludingPolls(allPolls, exempt); rec {
		t.Error("a turn consisting entirely of polls must not be recorded")
	}

	// A nil exemption function reproduces the pre-P53.2 signature exactly.
	plain, rec := turnSignatureExcludingPolls(mixed, nil)
	if !rec || plain != turnSignature(mixed) {
		t.Error("nil pollExempt must reproduce the original signature")
	}
}

// TestEngineDoesNotAbortOnPollLoop is the P53.2(a) regression: an agent waiting
// on a long-running job re-issues the identical call every turn and must not be
// killed for it.
func TestEngineDoesNotAbortOnPollLoop(t *testing.T) {
	pollTurn := func() []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "t", Name: "poll", Input: json.RawMessage(`{"mode":"wait","id":"job-1"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		}
	}
	turns := [][]provider.Event{pollTurn(), pollTurn(), pollTurn(), pollTurn(), pollTurn(), textTurn("job finished")}
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	pt := &pollTool{}
	_ = reg.Register(pt)
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test", LoopThreshold: 3})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "wait for the job"}}})
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			t.Errorf("polling must not trip the loop detector: %v", ev.Err)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if pt.called != 5 {
		t.Errorf("expected all 5 polls to run, got %d", pt.called)
	}
	if n := countNudges(conv.Messages, loopNudgePrefix); n != 0 {
		t.Errorf("polling must not be nudged, got %d", n)
	}
}

// TestEngineStillDetectsLoopWhenPollAccompaniesIt guards the narrow-exemption
// rule: a poll riding along with a genuinely repeated non-exempt call must not
// launder that call past the detector.
func TestEngineStillDetectsLoopWhenPollAccompaniesIt(t *testing.T) {
	mixedTurn := func() []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "p", Name: "poll", Input: json.RawMessage(`{"mode":"wait","id":"job-1"}`),
			}},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "f", Name: "fail_edit", Input: json.RawMessage(`{"old":"x"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		}
	}
	turns := make([][]provider.Event, 10)
	for i := range turns {
		turns[i] = mixedTurn()
	}
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	_ = reg.Register(&pollTool{})
	_ = reg.Register(&failEditTool{})
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test", LoopThreshold: 3})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	var gotErr error
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	})
	if err == nil || gotErr == nil || !strings.Contains(gotErr.Error(), "loop") {
		t.Fatalf("a repeated non-exempt call must still trip the detector, got %v", err)
	}
}
