package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/scottymacleod/aegis/internal/cost"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/tool"
)

// TestBudgetGateStopsRun verifies the run aborts before the next tool round once
// the estimated cost exceeds the configured budget.
func TestBudgetGateStopsRun(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1 requests a tool and reports usage that blows the budget.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 1_000_000}}, // $15 of opus
		},
		// Turn 2 should never run.
		{
			{Type: provider.EventTextDelta, Text: "should not reach"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	reg := tool.NewRegistry()
	et := &echoTool{}
	if err := reg.Register(et); err != nil {
		t.Fatal(err)
	}

	eng, err := New(Options{
		Adapter:   adapter,
		Tools:     reg,
		Cost:      cost.NewTracker(),
		BudgetUSD: 1.0, // $1 budget, turn 1 costs ~$15
		Model:     "claude-opus-4-8",
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotErr error
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	runErr := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	})

	if runErr == nil {
		t.Fatal("expected the run to fail on budget")
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "budget") {
		t.Errorf("expected a budget error, got %v", gotErr)
	}
	if et.called != 0 {
		t.Errorf("tool must not run after budget is exceeded, called %d", et.called)
	}
	if adapter.calls != 1 {
		t.Errorf("second model turn must not run, calls = %d", adapter.calls)
	}
}

// TestCostReportedOnTurnDone verifies cumulative cost rides along on KindTurnDone.
func TestCostReportedOnTurnDone(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "hi"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 1_000_000}},
		},
	}}
	eng, _ := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Cost: cost.NewTracker(), Model: "claude-opus-4-8"})

	var gotCost float64
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindTurnDone {
			gotCost = ev.CostUSD
		}
	}); err != nil {
		t.Fatal(err)
	}
	if gotCost <= 0 {
		t.Errorf("expected positive cost on turn-done, got %v", gotCost)
	}
}
