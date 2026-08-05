package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// sleepTool burns a fixed amount of wall-clock time when called, so a test can
// push a run past a wall-clock budget through tool execution rather than
// through model turns (the scripted adapter returns instantly).
type sleepTool struct {
	called int
	d      time.Duration
}

func (s *sleepTool) Name() string                 { return "sleep" }
func (s *sleepTool) Description() string          { return "sleep for a fixed duration" }
func (s *sleepTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *sleepTool) Capability() tool.Capability  { return tool.CapRead }
func (s *sleepTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	s.called++
	select {
	case <-time.After(s.d):
	case <-ctx.Done():
	}
	return tool.Result{Content: "slept"}, nil
}

// fakeClock advances by step on every reading after the first, so a test can
// put a run over its budget without sleeping and without depending on the
// host's monotonic-clock resolution.
//
// That dependency is why this exists. The original test expressed "already over
// budget" as a 1ns limit, reasoning that time.Since is always at least a
// nanosecond by the time the first gate runs. That is true on macOS and Linux
// and false on Windows, where Go's nanotime is ~0.5ms-granular and two
// back-to-back time.Now calls return the *same* instant — so the gate saw
// elapsed == 0, never fired, and the test failed on one OS while passing on the
// other. Real budgets are configured in whole seconds, so nothing about the
// production gate was wrong; only the test's clock assumption was.
type fakeClock struct {
	t     time.Time
	step  time.Duration
	reads int
}

func (c *fakeClock) now() time.Time {
	c.reads++
	if c.reads > 1 {
		c.t = c.t.Add(c.step)
	}
	return c.t
}

// TestWallClockBudgetStopsRun is the P52.15 regression: none of the three
// pre-existing budgets bound elapsed time, so a slow local model could burn
// all 40 iterations — potentially hours at ~7 tok/s — with no safety valve.
//
// The run starts on budget and the very first gate reading finds an hour gone,
// which asserts the gate fires and, importantly, that it fires *before* the
// first paid model call.
func TestWallClockBudgetStopsRun(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "should not reach"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	eng, err := New(Options{
		Adapter:            adapter,
		Tools:              tool.NewRegistry(),
		Cost:               cost.NewTracker(),
		MaxWallClockPerRun: time.Minute,
		Model:              "llama3.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	eng.now = (&fakeClock{t: time.Now(), step: time.Hour}).now

	var gotErr error
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	runErr := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	})

	if runErr == nil {
		t.Fatal("expected the run to fail on the wall-clock budget")
	}
	if !errors.Is(runErr, ErrWallClockLimit) {
		t.Errorf("run error must wrap ErrWallClockLimit so a caller can classify it, got %v", runErr)
	}
	if gotErr == nil || !errors.Is(gotErr, ErrWallClockLimit) {
		t.Errorf("expected a wall-clock error on the event stream, got %v", gotErr)
	}
	// "ran out of time" must be distinguishable from "ran out of iterations"
	// and from the cost/token budgets, in the message as well as the sentinel.
	if gotErr != nil && !strings.Contains(gotErr.Error(), "max_wall_clock_per_run") {
		t.Errorf("error should name the knob that raises it, got %q", gotErr.Error())
	}
	if adapter.calls != 0 {
		t.Errorf("no model turn should run once the budget is already spent, calls = %d", adapter.calls)
	}
}

// TestWallClockBudgetCountsToolTime covers the gate that matters in practice:
// the run is well under budget when it starts, and it is the *tool round* that
// pushes it over. The second model turn must not run.
func TestWallClockBudgetCountsToolTime(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "sleep", Input: json.RawMessage(`{}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "should not reach"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	reg := tool.NewRegistry()
	// The margin is deliberately wide (a 120ms tool against a 50ms budget) and
	// only ever errs in the safe direction: a slow machine makes the tool
	// overshoot further, which still trips the budget. Everything before the
	// tool round is in-memory and takes microseconds.
	st := &sleepTool{d: 120 * time.Millisecond}
	if err := reg.Register(st); err != nil {
		t.Fatal(err)
	}

	eng, err := New(Options{
		Adapter:            adapter,
		Tools:              reg,
		Cost:               cost.NewTracker(),
		MaxWallClockPerRun: 50 * time.Millisecond,
		Model:              "llama3.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	runErr := eng.Run(context.Background(), conv, func(Event) {})

	if !errors.Is(runErr, ErrWallClockLimit) {
		t.Fatalf("expected a wall-clock abort after the tool round, got %v", runErr)
	}
	if st.called != 1 {
		t.Errorf("the first tool round should have run, called %d", st.called)
	}
	if adapter.calls != 1 {
		t.Errorf("second model turn must not run once the budget is spent, calls = %d", adapter.calls)
	}
}

// TestWallClockBudgetOffByDefault and its under-budget sibling are the
// false-positive guards. A wall-clock cap cannot tell a stalled run from a slow
// one making real progress, which is exactly why it is opt-in — an accidental
// default would guillotine legitimate long work.
func TestWallClockBudgetOffByDefault(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit time.Duration
	}{
		{"unset", 0},
		{"generously under budget", time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &scriptedAdapter{turns: [][]provider.Event{
				{
					{Type: provider.EventTextDelta, Text: "done"},
					{Type: provider.EventDone, Stop: provider.StopEndTurn},
				},
			}}
			eng, err := New(Options{
				Adapter:            adapter,
				Tools:              tool.NewRegistry(),
				Cost:               cost.NewTracker(),
				MaxWallClockPerRun: tc.limit,
				Model:              "llama3.1",
			})
			if err != nil {
				t.Fatal(err)
			}
			conv := &Conversation{}
			conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
			if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
				t.Fatalf("run must complete normally, got %v", err)
			}
			if adapter.calls != 1 {
				t.Errorf("expected exactly one model turn, got %d", adapter.calls)
			}
		})
	}
}

// blockingAdapter stalls inside Stream until its context is cancelled, standing
// in for a local model runner that wedges mid-generation. It never emits a Done
// event, so nothing the engine polls between turns can end the run.
type blockingAdapter struct{ started chan struct{} }

func (b *blockingAdapter) Name() string { return "blocking" }

func (b *blockingAdapter) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Event, error) {
	out := make(chan provider.Event)
	go func() {
		defer close(out)
		if b.started != nil {
			close(b.started)
		}
		<-ctx.Done()
		select {
		case out <- provider.Event{Type: provider.EventError, Err: ctx.Err()}:
		case <-time.After(time.Second):
		}
	}()
	return out, nil
}

// TestWallClockBudgetFiresDuringATurn (P59.2) is the gap the two polled gates
// could not cover: the run wedges *inside* a model turn, so neither the
// pre-turn nor the pre-tool-round check ever runs again. The budget must still
// end the run, and must end it as a wall-clock abort rather than as a bare
// interrupt or an adapter transport error a caller would try to resume from.
func TestWallClockBudgetFiresDuringATurn(t *testing.T) {
	eng, err := New(Options{
		Adapter:            &blockingAdapter{started: make(chan struct{})},
		Cost:               cost.NewTracker(),
		MaxWallClockPerRun: 100 * time.Millisecond,
		Model:              "llama3.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	done := make(chan error, 1)
	go func() { done <- eng.Run(context.Background(), conv, func(Event) {}) }()

	select {
	case runErr := <-done:
		if !errors.Is(runErr, ErrWallClockLimit) {
			t.Fatalf("a turn that hangs past the budget must abort on the wall clock, got %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run never returned — the wall-clock budget cannot fire mid-turn")
	}
}

// TestWallClockOffLeavesAHungTurnAlone is the false-positive guard for the
// deadline above: with no budget configured, nothing in the engine may bound a
// turn. A run that hangs stays hung until its caller cancels — the pre-existing
// contract, and the reason the wall clock is opt-in.
func TestWallClockOffLeavesAHungTurnAlone(t *testing.T) {
	started := make(chan struct{})
	eng, err := New(Options{
		Adapter: &blockingAdapter{started: started},
		Cost:    cost.NewTracker(),
		Model:   "llama3.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx, conv, func(Event) {}) }()

	<-started
	select {
	case runErr := <-done:
		t.Fatalf("the run ended on its own with no budget configured: %v", runErr)
	case <-time.After(300 * time.Millisecond):
	}

	cancel()
	select {
	case runErr := <-done:
		if errors.Is(runErr, ErrWallClockLimit) {
			t.Errorf("a caller cancel must not be reported as a wall-clock abort, got %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not return after its context was cancelled")
	}
}
