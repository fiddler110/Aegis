package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
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

// TestBudgetGateStopsMaxTokenContinuation is the P9 dead-zone regression: the
// budget check used to sit only right before a tool round, so a turn that
// blew the budget while being cut off by the token limit (no tool calls, just
// a continuation `continue`) skipped it entirely and paid for another full
// turn before the iteration cap would ever catch it.
func TestBudgetGateStopsMaxTokenContinuation(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1 is cut off by the token limit; its usage alone blows the budget.
		{
			{Type: provider.EventTextDelta, Text: "partial"},
			{Type: provider.EventDone, Stop: provider.StopMaxTokens, Usage: &provider.Usage{InputTokens: 1_000_000}}, // $15 of opus
		},
		// The continuation turn must never run.
		{
			{Type: provider.EventTextDelta, Text: "should not reach"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	eng, err := New(Options{
		Adapter:   adapter,
		Tools:     tool.NewRegistry(),
		Cost:      cost.NewTracker(),
		BudgetUSD: 1.0,
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
	if adapter.calls != 1 {
		t.Errorf("continuation turn must not run once the budget is blown, calls = %d", adapter.calls)
	}
}

// TestMaxTokensPerRunStopsEstimatedUsage is the regression for P10.5: a local
// model (Ollama, or any provider that reports no real usage) gets its token
// counts marked IsEstimated, and BudgetUSD silently never fires for those
// turns since an estimate carries no dollar cost — the local-first default
// posture had no working spend guardrail at all. MaxTokensPerRun must still
// abort the run, because token counts (estimated or exact) are always
// present.
func TestMaxTokensPerRunStopsEstimatedUsage(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1 requests a tool and reports estimated usage that blows the
		// token budget. BudgetUSD would never catch this turn: an estimated
		// usage contributes $0.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 600_000, OutputTokens: 500_000, IsEstimated: true}},
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
		Adapter:         adapter,
		Tools:           reg,
		Cost:            cost.NewTracker(),
		BudgetUSD:       1.0, // never fires: usage is estimated, so cost.Add is skipped
		MaxTokensPerRun: 1_000_000,
		Model:           "llama3.1", // not in the pricing catalog either
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotErr error
	var gotCost float64
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	runErr := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
		if ev.Kind == KindTurnDone {
			gotCost = ev.CostUSD
		}
	})

	if runErr == nil {
		t.Fatal("expected the run to fail on the token budget")
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "token budget") {
		t.Errorf("expected a token budget error, got %v", gotErr)
	}
	if gotCost != 0 {
		t.Errorf("estimated usage must not contribute dollar cost, got %v", gotCost)
	}
	if et.called != 0 {
		t.Errorf("tool must not run after the token budget is exceeded, called %d", et.called)
	}
	if adapter.calls != 1 {
		t.Errorf("second model turn must not run, calls = %d", adapter.calls)
	}
}

// TestMaxGeneratedTokensPerRunCountsOutputOnly is the P59.4 regression. The two
// token budgets must not be interchangeable: a run whose prompt is re-counted in
// full every turn (the Ollama shape — a huge InputTokens, a small OutputTokens)
// blows through max_tokens_per_run long before it has generated much, which is
// exactly why the generation budget exists. So the generation cap must ignore
// input/cache tokens entirely, and must fire when — and only when — the model's
// own output crosses it.
func TestMaxGeneratedTokensPerRunCountsOutputOnly(t *testing.T) {
	// A turn shaped like a local run mid-conversation: 400k of re-counted
	// prompt, 500 tokens actually generated.
	turn := func(out int) []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 400_000, OutputTokens: out, IsEstimated: true}},
		}
	}

	run := func(t *testing.T, genCap int) (int, error) {
		t.Helper()
		adapter := &scriptedAdapter{turns: [][]provider.Event{turn(500), turn(500), {
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		}}}
		reg := tool.NewRegistry()
		if err := reg.Register(&echoTool{}); err != nil {
			t.Fatal(err)
		}
		eng, err := New(Options{
			Adapter:                  adapter,
			Tools:                    reg,
			Cost:                     cost.NewTracker(),
			MaxGeneratedTokensPerRun: genCap,
			Model:                    "llama3.1",
		})
		if err != nil {
			t.Fatal(err)
		}
		var gotErr error
		conv := &Conversation{}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
		eng.Run(context.Background(), conv, func(ev Event) {
			if ev.Kind == KindError {
				gotErr = ev.Err
			}
		})
		return adapter.calls, gotErr
	}

	// 800k input tokens accumulate across two turns and must not trip a
	// 1000-token generation cap — only the 1000 generated tokens do, which
	// happens exactly at the third gate check.
	calls, gotErr := run(t, 1_000)
	if gotErr == nil || !strings.Contains(gotErr.Error(), "generation budget") {
		t.Errorf("expected a generation budget error after 1000 generated tokens, got %v", gotErr)
	}
	if calls != 2 {
		t.Errorf("the third turn must not run once the generation budget is spent, calls = %d", calls)
	}

	// With headroom, the same 800k of input must not abort anything.
	calls, gotErr = run(t, 100_000)
	if gotErr != nil {
		t.Errorf("input tokens must not count toward the generation budget, got %v", gotErr)
	}
	if calls != 3 {
		t.Errorf("expected the run to finish all three turns, calls = %d", calls)
	}
}

// TestTokenBudgetErrorNamesTheOtherKey is the other half of P59.4: the point of
// the split is lost if a user who set max_tokens_per_run as a work budget is
// told only "used N of M". The abort has to say that the number counts the whole
// prompt every turn and name the key that counts generated output instead.
func TestTokenBudgetErrorNamesTheOtherKey(t *testing.T) {
	// Usage is added *after* newRunBudget baselines the tracker (P80.3: the
	// baseline must not swallow spend that belongs to this run) so it counts
	// as this run's own spend, matching how Engine.Run itself orders things —
	// newRunBudget once at the start, tracker.AddTokens per turn after.
	tracker := cost.NewTracker()
	e := &Engine{cost: tracker, maxTokensPerRun: 10_000}
	b := e.newRunBudget()
	tracker.AddTokens(provider.Usage{InputTokens: 50_000, OutputTokens: 100, IsEstimated: true})
	err := b.tokensExceeded()
	if err == nil {
		t.Fatal("expected the context-token budget to fire")
	}
	for _, want := range []string{"the whole prompt on every turn", "cost.max_generated_tokens_per_run"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("abort message must contain %q, got: %v", want, err)
		}
	}

	// The generation budget is checked first, so a run that has blown both is
	// told the more specific thing rather than the misleading one.
	tracker2 := cost.NewTracker()
	e = &Engine{cost: tracker2, maxTokensPerRun: 10_000, maxGenTokens: 50}
	b2 := e.newRunBudget()
	tracker2.AddTokens(provider.Usage{InputTokens: 50_000, OutputTokens: 100, IsEstimated: true})
	err = b2.tokensExceeded()
	if err == nil || !strings.Contains(err.Error(), "generation budget") {
		t.Errorf("expected the generation budget to take precedence, got %v", err)
	}
}

// TestBudgetResetsPerRunNotPerTracker is the P80.2-drive-investigation
// regression: internal/drive shares one Engine (and therefore one
// *cost.Tracker) across every phase's Run call, resetting only what Run
// itself resets — exactly the design runBudget's own doc comment describes
// ("a Run is one phase turn... one global cap would guillotine a long build
// mid-phase"). Before the fix, costExceeded/tokensExceeded read the
// tracker's raw cumulative totals, so a second Run on the same tracker
// inherited the first Run's spend as a head start against its own budget —
// the opposite of "per-Run". This drives the same Engine through two Run
// calls, each individually well under BudgetUSD, whose combined spend would
// have tripped the old cumulative check.
func TestBudgetResetsPerRunNotPerTracker(t *testing.T) {
	// $15/1M input tokens for opus (internal/cost's pricing table): 60,000
	// input tokens costs $0.90.
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Run A: a single turn that ends the run outright, spending $0.90 of
		// a $1.00 budget.
		{
			{Type: provider.EventTextDelta, Text: "a"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 60_000}},
		},
		// Run B: a turn requesting a tool, spending another $0.50. Combined
		// with run A's $0.90 that is $1.40 — over budget under the old
		// cumulative check, but this run alone only spent $0.50.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 33_334}}, // ~$0.50
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	reg := tool.NewRegistry()
	et := &echoTool{}
	if err := reg.Register(et); err != nil {
		t.Fatal(err)
	}

	tracker := cost.NewTracker()
	eng, err := New(Options{
		Adapter:   adapter,
		Tools:     reg,
		Cost:      tracker,
		BudgetUSD: 1.0,
		Model:     "claude-opus-4-8",
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "phase A"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run A: unexpected error: %v", err)
	}
	if spent := tracker.TotalUSD(); spent < 0.85 || spent > 0.95 {
		t.Fatalf("run A: expected ~$0.90 spent, got $%.4f", spent)
	}

	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "phase B"}}})
	var gotErr error
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	}); err != nil {
		gotErr = err
	}

	if gotErr != nil {
		t.Errorf("run B must not inherit run A's spend against its own budget, got error: %v", gotErr)
	}
	if et.called != 1 {
		t.Errorf("run B's tool call must run since its own spend is under budget, called %d", et.called)
	}
	if adapter.calls != 3 {
		t.Errorf("run B must complete both its turns, calls = %d", adapter.calls)
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
