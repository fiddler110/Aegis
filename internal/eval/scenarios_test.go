package eval

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// scriptedAdapter returns a predefined event sequence for each successive
// call, one []provider.Event per model turn. Mirrors the pattern used
// throughout internal/engine's own tests.
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

// echoTool returns its "msg" argument back as text; a read-capability tool
// with no side effects.
type echoTool struct{ calls int }

func (e *echoTool) Name() string                 { return "echo" }
func (e *echoTool) Description() string          { return "echo the msg argument" }
func (e *echoTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (e *echoTool) Capability() tool.Capability  { return tool.CapRead }
func (e *echoTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	e.calls++
	var args struct {
		Msg string `json:"msg"`
	}
	_ = json.Unmarshal(input, &args)
	return tool.Result{Content: "echo:" + args.Msg}, nil
}

// writeFileTool simulates a destructive write; Execute increments calls only
// if it actually runs, so scenarios can assert a permission denial stopped
// it before any side effect occurred.
type writeFileTool struct{ calls int }

func (w *writeFileTool) Name() string                 { return "write_file" }
func (w *writeFileTool) Description() string          { return "write a file" }
func (w *writeFileTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (w *writeFileTool) Capability() tool.Capability  { return tool.CapWrite }
func (w *writeFileTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	w.calls++
	return tool.Result{Content: "wrote file"}, nil
}

// TestScenario_ToolRoundTrip exercises the harness's basic path: a tool call
// followed by a final answer, asserted via checks and pinned with a golden
// transcript so any change to event emission (kinds, text accumulation,
// tool-name plumbing) is caught even if it doesn't trip a more targeted unit
// test.
func TestScenario_ToolRoundTrip(t *testing.T) {
	reg := tool.NewRegistry()
	et := &echoTool{}
	if err := reg.Register(et); err != nil {
		t.Fatal(err)
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "let me check"},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 5}},
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 20, OutputTokens: 3}},
		},
	}}

	s := Scenario{
		Name:   "tool round trip",
		System: "sys",
		Options: engine.Options{
			Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100,
		},
		Turns: []string{"hello"},
	}
	r := RunAndCheck(t, context.Background(), s,
		ExpectNoError(),
		ExpectToolCalled("echo"),
		ExpectFinalTextContains("done"),
	)
	if et.calls != 1 {
		t.Errorf("echo tool executed %d times, want 1", et.calls)
	}
	AssertGolden(t, "testdata/tool_round_trip.golden.json", r)
}

// TestScenario_PlanModeBlocksWrite verifies a persona/session running in plan
// (read-only) mode never actually executes a write-capability tool, even when
// the model asks for it — the permission gate must deny before Execute runs.
// This is the persona/mode regression case P9.1 calls out: a change to
// engine.runTools that started calling Execute before checking the gate
// would slip past every existing unit test that doesn't wire up a real gate.
func TestScenario_PlanModeBlocksWrite(t *testing.T) {
	reg := tool.NewRegistry()
	wt := &writeFileTool{}
	if err := reg.Register(wt); err != nil {
		t.Fatal(err)
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "write_file", Input: json.RawMessage(`{}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "I could not write the file in plan mode"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	s := Scenario{
		Name: "plan mode blocks write",
		Options: engine.Options{
			Adapter: adapter, Tools: reg, Model: "test",
			Gate: permission.New(permission.ModePlan, permission.AutoDeny{}),
		},
		Turns: []string{"please write a file"},
	}
	RunAndCheck(t, context.Background(), s,
		ExpectNoError(),
		ExpectFinalTextContains("could not write"),
	)
	if wt.calls != 0 {
		t.Errorf("write_file.Execute called %d times, want 0 (plan mode must deny before execution)", wt.calls)
	}
}

// TestScenario_BudgetAbortsSecondTurn verifies a session that blows its cost
// budget on the first model turn never reaches the second — the harness
// asserts both the error surfaced and that the would-be-forbidden turn never
// ran, which a mechanism-only unit test on the budget check alone wouldn't
// catch if e.g. a future change made the abort advisory instead of fatal.
func TestScenario_BudgetAbortsSecondTurn(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 1_000_000}}, // ~$15 of opus
		},
		{
			{Type: provider.EventTextDelta, Text: "should never run"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	s := Scenario{
		Name: "budget aborts before second turn",
		Options: engine.Options{
			Adapter: adapter, Tools: reg, Model: "claude-opus-4-8",
			Cost: cost.NewTracker(), BudgetUSD: 1.0,
		},
		Turns: []string{"go"},
	}
	r := RunAndCheck(t, context.Background(), s, ExpectErrorContains("budget"))
	if got := r.FinalText(); got != "" {
		t.Errorf("final text = %q, want empty (second turn must not run)", got)
	}
}

// TestScenario_MultiTurnContinuity sends two sequential user turns on the
// same conversation and verifies the second turn's tool call still runs
// correctly and its answer is distinct from the first — a regression guard
// for context/history bugs (e.g. a compaction or persistence change that
// corrupts prior turns) that per-mechanism unit tests, which mostly run a
// single turn, don't exercise.
func TestScenario_MultiTurnContinuity(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "first answer"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 2}},
		},
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_2", Name: "echo", Input: json.RawMessage(`{"msg":"again"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 2}},
		},
		{
			{Type: provider.EventTextDelta, Text: "second answer"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 2}},
		},
	}}

	s := Scenario{
		Name: "multi-turn continuity",
		Options: engine.Options{
			Adapter: adapter, Tools: reg, Model: "test",
		},
		Turns: []string{"hello", "do it again"},
	}
	r := RunAndCheck(t, context.Background(), s, ExpectNoError())
	if len(r.Turns) != 2 {
		t.Fatalf("got %d turn results, want 2", len(r.Turns))
	}
	if r.Turns[0].FinalText != "first answer" {
		t.Errorf("turn 1 final text = %q, want %q", r.Turns[0].FinalText, "first answer")
	}
	if r.Turns[1].FinalText != "second answer" {
		t.Errorf("turn 2 final text = %q, want %q", r.Turns[1].FinalText, "second answer")
	}
	if len(r.Turns[1].ToolCalls) != 1 || r.Turns[1].ToolCalls[0] != "echo" {
		t.Errorf("turn 2 tool calls = %v, want [echo]", r.Turns[1].ToolCalls)
	}
}
