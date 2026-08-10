package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// dripAdapter streams `count` text deltas `gap` apart and then finishes. It is
// the false-positive fixture: a turn whose *total* duration comfortably exceeds
// the stall bound while no single silent gap comes close to it. That is the
// shape of every legitimate slow local-model turn, and it is precisely what
// distinguishes this detector from a wall-clock budget.
type dripAdapter struct {
	gap   time.Duration
	count int
	calls int
	// toolCall makes the first turn request the named tool instead of ending,
	// so a test can reach the tool round.
	toolCall string
}

func (d *dripAdapter) Name() string { return "drip" }

func (d *dripAdapter) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Event, error) {
	turn := d.calls
	d.calls++
	out := make(chan provider.Event)
	go func() {
		defer close(out)
		// A cancelled stream reports an error, exactly as a real adapter does.
		// Without this a spurious stall would merely truncate the turn — the
		// engine would read a reply with no tool calls, treat it as a final
		// answer and return nil, so a false-positive test asserting only
		// "Run returned no error" would pass while the detector misfired. That
		// is not hypothetical: it is what the P39.17 mutation run caught.
		fail := func() {
			select {
			case out <- provider.Event{Type: provider.EventError, Err: ctx.Err()}:
			case <-time.After(time.Second):
			}
		}
		for i := 0; i < d.count; i++ {
			select {
			case <-time.After(d.gap):
			case <-ctx.Done():
				fail()
				return
			}
			select {
			case out <- provider.Event{Type: provider.EventTextDelta, Text: "."}:
			case <-ctx.Done():
				fail()
				return
			}
		}
		if d.toolCall != "" && turn == 0 {
			select {
			case out <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "t1", Name: d.toolCall, Input: json.RawMessage(`{}`)}}:
			case <-ctx.Done():
				fail()
				return
			}
			select {
			case out <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse}:
			case <-ctx.Done():
				fail()
			}
			return
		}
		select {
		case out <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}:
		case <-ctx.Done():
			fail()
		}
	}()
	return out, nil
}

// hangingTool blocks until its context is cancelled, standing in for a wedged
// sandbox exec or an MCP server that accepted a call and never answered. It is
// the tool-phase counterpart of blockingAdapter (wallclock_test.go): nothing in
// the engine emits, streams or counts while it sits there.
type hangingTool struct {
	entered chan struct{}
	once    bool
}

func (h *hangingTool) Name() string                 { return "hang" }
func (h *hangingTool) Description() string          { return "block until cancelled" }
func (h *hangingTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (h *hangingTool) Capability() tool.Capability  { return tool.CapRead }
func (h *hangingTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	if h.entered != nil && !h.once {
		h.once = true
		close(h.entered)
	}
	<-ctx.Done()
	return tool.Result{}, ctx.Err()
}

// slowWriteTool sleeps, and declares CapWrite so that several calls to it in one
// round are *serialized* by the engine's exec lock rather than fanned out. That
// is the only shape in which a single tool round can outlast the stall bound
// with no provider stream event anywhere in between — which makes it the only
// fixture that actually depends on the tool-phase beats rather than on the
// stream ones.
type slowWriteTool struct {
	d      time.Duration
	mu     sync.Mutex
	called int
}

func (s *slowWriteTool) Name() string                 { return "slow_write" }
func (s *slowWriteTool) Description() string          { return "sleep, serialized" }
func (s *slowWriteTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *slowWriteTool) Capability() tool.Capability  { return tool.CapWrite }
func (s *slowWriteTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	s.mu.Lock()
	s.called++
	s.mu.Unlock()
	select {
	case <-time.After(s.d):
	case <-ctx.Done():
		return tool.Result{}, ctx.Err()
	}
	return tool.Result{Content: "written"}, nil
}

func (s *slowWriteTool) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called
}

// runWithTimeout drives eng.Run on a goroutine and fails the test if it never
// returns. Every case here asserts something about a run that is *supposed* to
// hang without the detector, so a bare synchronous call would wedge the whole
// package on a regression rather than reporting one.
func runWithTimeout(t *testing.T, eng *Engine, ctx context.Context, wait time.Duration) (error, bool) {
	t.Helper()
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx, conv, func(Event) {}) }()
	select {
	case err := <-done:
		return err, true
	case <-time.After(wait):
		return nil, false
	}
}

// TestTurnStallAbortsBothPhases is P39.17's core claim: a turn can go silent in
// exactly two places — waiting on the model stream, or waiting on a tool — and
// the detector must end the run from either, with the same named error.
//
// Neither case is reachable by any other guard in this package. The no-progress
// nudge counts turns, the loop detector compares tool calls and the failure
// breaker counts failed rounds; all three need turns to keep completing, and
// here none ever does.
func TestTurnStallAbortsBothPhases(t *testing.T) {
	// Large enough that the detection-latency assertion below has room for
	// Windows' coarse timer granularity, small enough to stay a unit test.
	const limit = 400 * time.Millisecond

	for _, tc := range []struct {
		name  string
		build func(t *testing.T) (provider.Adapter, *tool.Registry, func(t *testing.T))
	}{
		{
			name: "model stream never produces an event",
			build: func(t *testing.T) (provider.Adapter, *tool.Registry, func(t *testing.T)) {
				return &blockingAdapter{started: make(chan struct{})}, tool.NewRegistry(), func(*testing.T) {}
			},
		},
		{
			name: "tool call never returns",
			build: func(t *testing.T) (provider.Adapter, *tool.Registry, func(t *testing.T)) {
				adapter := &scriptedAdapter{turns: [][]provider.Event{
					{
						{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "hang", Input: json.RawMessage(`{}`)}},
						{Type: provider.EventDone, Stop: provider.StopToolUse},
					},
					{
						{Type: provider.EventTextDelta, Text: "should not reach"},
						{Type: provider.EventDone, Stop: provider.StopEndTurn},
					},
				}}
				reg := tool.NewRegistry()
				ht := &hangingTool{entered: make(chan struct{})}
				if err := reg.Register(ht); err != nil {
					t.Fatal(err)
				}
				return adapter, reg, func(t *testing.T) {
					select {
					case <-ht.entered:
					default:
						t.Error("the tool never ran, so this case did not exercise the tool phase")
					}
					if adapter.calls != 1 {
						t.Errorf("the second model turn must not run after a stall, calls = %d", adapter.calls)
					}
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, reg, check := tc.build(t)
			eng, err := New(Options{
				Adapter:      adapter,
				Tools:        reg,
				Cost:         cost.NewTracker(),
				MaxTurnStall: limit,
				Model:        "llama3.1",
			})
			if err != nil {
				t.Fatal(err)
			}

			start := time.Now()
			runErr, returned := runWithTimeout(t, eng, context.Background(), 10*time.Second)
			if !returned {
				t.Fatal("the run never returned — the stall detector did not fire")
			}
			// "It fired eventually" is not the contract; the bound is the
			// contract. Sampling costs at most limit/stallPollDivisor, so twice
			// the limit is a generous ceiling — and it is what stops a
			// regression that quietly multiplies the threshold from passing
			// because the test merely waited longer.
			if elapsed := time.Since(start); elapsed > 2*limit {
				t.Errorf("the stall took %v to detect against a %v bound — detection latency is unbounded", elapsed, limit)
			}
			if !errors.Is(runErr, ErrTurnStalled) {
				t.Fatalf("a silent turn must abort as ErrTurnStalled, got %v", runErr)
			}
			// A hang must never be mistaken for the operator's own time bound,
			// nor for a plain interrupt a caller would resume from.
			if errors.Is(runErr, ErrWallClockLimit) || errors.Is(runErr, ErrInterrupted) {
				t.Errorf("a stall must not be reported as a wall-clock abort or an interrupt: %v", runErr)
			}
			if !strings.Contains(runErr.Error(), "max_turn_stall") {
				t.Errorf("the error must name the knob that raises it, got %q", runErr.Error())
			}
			check(t)
		})
	}
}

// TestTurnStallSurfacedOnTheEventStream pins the reporting half: the abort has
// to reach the client as a KindError event, the way every other engine abort
// does, not only as Run's return value. An unattended drive reads the stream.
func TestTurnStallSurfacedOnTheEventStream(t *testing.T) {
	eng, err := New(Options{
		Adapter:      &blockingAdapter{started: make(chan struct{})},
		Cost:         cost.NewTracker(),
		MaxTurnStall: 150 * time.Millisecond,
		Model:        "llama3.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	events := make(chan error, 8)
	done := make(chan error, 1)
	go func() {
		done <- eng.Run(context.Background(), conv, func(ev Event) {
			if ev.Kind == KindError {
				select {
				case events <- ev.Err:
				default:
				}
			}
		})
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run never returned")
	}
	select {
	case got := <-events:
		if !errors.Is(got, ErrTurnStalled) {
			t.Fatalf("expected a stall error on the event stream, got %v", got)
		}
	default:
		t.Fatal("the stall was never emitted as a KindError event")
	}
}

// TestTurnStallNoFalsePositive is the guard that decides whether this knob can
// ship on by default. Both cases run *longer* than the bound while never going
// silent for it: a slow drip of tokens, and a tool round of several short calls.
// If either tripped, a default-on detector would guillotine ordinary work on
// exactly the slow local hardware Aegis targets.
func TestTurnStallNoFalsePositive(t *testing.T) {
	// 60ms drips against a 400ms bound. Two properties matter and both are
	// asserted below: no single gap comes close to the bound (a ~7x margin, so a
	// loaded machine stretching one sleep cannot flake this into a failure), and
	// the *total* comfortably exceeds it (600ms) — without which the turn would
	// finish before the watchdog's first relevant tick and the case would depend
	// on nothing at all.
	const (
		limit = 400 * time.Millisecond
		gap   = 60 * time.Millisecond
		drips = 10
	)

	t.Run("slow but steady model stream", func(t *testing.T) {
		adapter := &dripAdapter{gap: gap, count: drips}
		eng, err := New(Options{
			Adapter:      adapter,
			Tools:        tool.NewRegistry(),
			Cost:         cost.NewTracker(),
			MaxTurnStall: limit,
			Model:        "llama3.1",
		})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		runErr, returned := runWithTimeout(t, eng, context.Background(), 10*time.Second)
		if !returned {
			t.Fatal("the run never returned")
		}
		if runErr != nil {
			t.Fatalf("a steadily streaming turn must complete normally, got %v", runErr)
		}
		if adapter.calls != 1 {
			t.Errorf("expected exactly one model turn, got %d", adapter.calls)
		}
		// Without this the case could pass by finishing before the detector had
		// anything to say, proving nothing — which is exactly how it survived
		// the "delete the stream beat" mutation on its first draft.
		if elapsed := time.Since(start); elapsed <= limit {
			t.Errorf("the turn finished in %v, inside the %v bound — it never reached the detector", elapsed, limit)
		}
	})

	t.Run("many short tool rounds outlasting the bound", func(t *testing.T) {
		// Eight rounds of one 60ms tool each: ~480ms of work against the 400ms
		// bound, with no individual gap above ~60ms.
		//
		// Rounds rather than one round of eight calls, on purpose. A single
		// multi-call round runs concurrently (runTools fans out), so eight 60ms
		// calls would finish in ~60ms and the round would never reach the bound
		// at all — the test would have proved nothing. One call per round takes
		// the sequential path and actually accumulates the elapsed time.
		const rounds = 8
		reg := tool.NewRegistry()
		st := &sleepTool{d: 60 * time.Millisecond}
		if err := reg.Register(st); err != nil {
			t.Fatal(err)
		}

		var turns [][]provider.Event
		for i := 0; i < rounds; i++ {
			turns = append(turns, []provider.Event{
				{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
					ID: fmt.Sprintf("s%d", i), Name: "sleep", Input: json.RawMessage(`{}`)}},
				{Type: provider.EventDone, Stop: provider.StopToolUse},
			})
		}
		turns = append(turns, []provider.Event{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		})

		adapter := &scriptedAdapter{turns: turns}
		eng, err := New(Options{
			Adapter:       adapter,
			Tools:         reg,
			Cost:          cost.NewTracker(),
			MaxTurnStall:  limit,
			MaxIterations: rounds + 2,
			// The eight rounds are deliberately identical, which is the loop
			// detector's abort condition — a different guard entirely, and one
			// that would mask what this case is measuring.
			LoopThreshold: -1,
			Model:         "llama3.1",
		})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		runErr, returned := runWithTimeout(t, eng, context.Background(), 30*time.Second)
		if !returned {
			t.Fatal("the run never returned")
		}
		if runErr != nil {
			t.Fatalf("a busy sequence of tool rounds must not be read as a stall, got %v", runErr)
		}
		if st.called != rounds {
			t.Errorf("expected all %d tool calls to run, got %d", rounds, st.called)
		}
		// The premise of the case: without this the run could finish inside the
		// bound and assert nothing at all.
		if elapsed := time.Since(start); elapsed <= limit {
			t.Errorf("the run finished in %v, inside the %v bound — this case never reached the detector", elapsed, limit)
		}
	})

	t.Run("one serialized tool round outlasting the bound", func(t *testing.T) {
		// The case above is beaten by the *stream* events of each new round, so
		// it would still pass if the tool-phase beats were deleted. This one
		// cannot be: eight write-capability calls in a single round take the
		// exec lock one at a time, so the whole 480ms sits between two provider
		// events with nothing but tool activity in between.
		const calls = 8
		reg := tool.NewRegistry()
		wt := &slowWriteTool{d: 60 * time.Millisecond}
		if err := reg.Register(wt); err != nil {
			t.Fatal(err)
		}

		round := make([]provider.Event, 0, calls+1)
		for i := 0; i < calls; i++ {
			round = append(round, provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: fmt.Sprintf("w%d", i), Name: "slow_write", Input: json.RawMessage(`{}`)}})
		}
		round = append(round, provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse})

		adapter := &scriptedAdapter{turns: [][]provider.Event{
			round,
			{
				{Type: provider.EventTextDelta, Text: "done"},
				{Type: provider.EventDone, Stop: provider.StopEndTurn},
			},
		}}
		eng, err := New(Options{
			Adapter:      adapter,
			Tools:        reg,
			Cost:         cost.NewTracker(),
			MaxTurnStall: limit,
			Model:        "llama3.1",
		})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		runErr, returned := runWithTimeout(t, eng, context.Background(), 30*time.Second)
		if !returned {
			t.Fatal("the run never returned")
		}
		if runErr != nil {
			t.Fatalf("a serialized tool round must not be read as a stall, got %v", runErr)
		}
		if got := wt.calls(); got != calls {
			t.Errorf("expected all %d serialized calls to run, got %d", calls, got)
		}
		if elapsed := time.Since(start); elapsed <= limit {
			t.Errorf("the round finished in %v, inside the %v bound — the calls did not serialize", elapsed, limit)
		}
	})
}

// TestTurnStallDisabled is the opt-out contract. With the knob at 0 nothing in
// the engine may bound a turn — a hung run stays hung until its caller cancels,
// which is the pre-P39.17 behaviour and must remain reachable for an operator
// who really does run a tool that blocks silently for hours.
//
// It also asserts the shape of the cancel: a caller's own ctx cancellation must
// not be dressed up as a stall, or `aegis` would report every Ctrl-C as a hang.
func TestTurnStallDisabled(t *testing.T) {
	started := make(chan struct{})
	eng, err := New(Options{
		Adapter:      &blockingAdapter{started: started},
		Cost:         cost.NewTracker(),
		MaxTurnStall: 0,
		Model:        "llama3.1",
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
		t.Fatalf("the run ended on its own with the stall detector disabled: %v", runErr)
	case <-time.After(500 * time.Millisecond):
	}

	cancel()
	select {
	case runErr := <-done:
		if errors.Is(runErr, ErrTurnStalled) {
			t.Errorf("a caller cancel must not be reported as a stall, got %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not return after its context was cancelled")
	}
}

// TestWallClockWinsOverStall pins the precedence at the abort sites. Both bounds
// cancel the same context, so a run that has crossed *both* could plausibly
// report either — and it must report the wall clock, because that is the
// operator's own explicit instruction and it is what the drive's fatality rules
// and the docs are written around. A stall is the diagnosis of last resort.
//
// Asserted on the two override helpers directly rather than through a run: the
// engine-level race ("which bound crossed first") is real and timing-dependent,
// while the ordering rule the abort sites encode is not, and it is the ordering
// rule a future edit could quietly invert.
func TestWallClockWinsOverStall(t *testing.T) {
	budget := &runBudget{
		wallClock: time.Minute,
		now:       func() time.Time { return time.Now().Add(time.Hour) },
		start:     time.Now(),
	}
	stall := &stallWatch{limit: time.Minute, fired: true, idle: 90 * time.Second, stop: make(chan struct{})}

	// The order used at every abort site in Run.
	got := budget.override(stall.override(errors.New("transport: connection reset")))
	if !errors.Is(got, ErrWallClockLimit) {
		t.Fatalf("a run past both bounds must report the wall clock, got %v", got)
	}

	// With the wall clock unset, the stall is what survives — the same call
	// must not swallow it.
	noBudget := &runBudget{now: time.Now, start: time.Now()}
	got = noBudget.override(stall.override(errors.New("transport: connection reset")))
	if !errors.Is(got, ErrTurnStalled) {
		t.Fatalf("with no wall-clock bound the stall must propagate, got %v", got)
	}

	// And neither may invent a verdict for a run that crossed nothing.
	quiet := &stallWatch{limit: time.Minute, stop: make(chan struct{})}
	orig := errors.New("transport: connection reset")
	if got = noBudget.override(quiet.override(orig)); !errors.Is(got, orig) {
		t.Fatalf("an ordinary error must pass through unchanged, got %v", got)
	}
}

// TestStallWatchBeatKeepsItAlive covers the unit directly, without an engine, so
// a regression in the beat plumbing is distinguishable from a regression in the
// watchdog itself. Table-driven over the two behaviours that matter: silence
// fires, activity does not.
func TestStallWatchBeatKeepsItAlive(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      time.Duration
		beatEvery  time.Duration
		observeFor time.Duration
		wantFired  bool
	}{
		{name: "silent past the limit", limit: 100 * time.Millisecond, observeFor: 500 * time.Millisecond, wantFired: true},
		{name: "beaten well inside the limit", limit: 400 * time.Millisecond, beatEvery: 25 * time.Millisecond, observeFor: 600 * time.Millisecond},
		{name: "disabled never fires", limit: 0, observeFor: 300 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &stallWatch{limit: tc.limit, last: time.Now(), stop: make(chan struct{})}
			ctx, stop := s.watch(context.Background())
			defer stop()

			deadline := time.Now().Add(tc.observeFor)
			for time.Now().Before(deadline) {
				if tc.beatEvery > 0 {
					time.Sleep(tc.beatEvery)
					beat(ctx)
					continue
				}
				time.Sleep(10 * time.Millisecond)
			}

			err := s.stalled()
			if tc.wantFired {
				if err == nil {
					t.Fatal("expected the watchdog to fire")
				}
				if !errors.Is(err, ErrTurnStalled) {
					t.Errorf("the watchdog's error must wrap ErrTurnStalled, got %v", err)
				}
				if ctx.Err() == nil {
					t.Error("firing must cancel the run context, or a wedged call never returns")
				}
				return
			}
			if err != nil {
				t.Fatalf("the watchdog fired on a live run: %v", err)
			}
			if ctx.Err() != nil {
				t.Error("the run context must not be cancelled while activity continues")
			}
		})
	}
}
