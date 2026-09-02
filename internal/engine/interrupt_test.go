package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// preferFinishTool declares tool.InterruptPreference statically.
type preferFinishTool struct {
	namedFakeTool
	prefer bool
}

func (p *preferFinishTool) PreferFinish(json.RawMessage) bool { return p.prefer }

func newTestEngine(t *testing.T, tools ...tool.Tool) *Engine {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	eng, err := New(Options{Adapter: &scriptedAdapter{}, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

// TestRequestStopHardCancelsImmediately covers the unchanged default path:
// hard=true always calls hardCancel and never arms the graceful flag.
func TestRequestStopHardCancelsImmediately(t *testing.T) {
	eng := newTestEngine(t)
	called := false
	if soft := eng.RequestStop(true, func() { called = true }); soft {
		t.Error("RequestStop(hard=true) reported soft, want immediate")
	}
	if !called {
		t.Error("RequestStop(hard=true) did not call hardCancel")
	}
	if eng.consumeGracefulStop() {
		t.Error("a hard stop must not leave the graceful flag armed")
	}
}

// TestRequestStopSoftFallsBackWithNothingInFlight covers the "nothing to
// wait for" case: a soft stop with no in-flight calls is indistinguishable
// from a hard one and must not silently do nothing.
func TestRequestStopSoftFallsBackWithNothingInFlight(t *testing.T) {
	eng := newTestEngine(t)
	called := false
	if soft := eng.RequestStop(false, func() { called = true }); soft {
		t.Error("RequestStop(soft) with nothing in flight reported soft, want immediate fallback")
	}
	if !called {
		t.Error("RequestStop(soft) with nothing in flight did not fall back to hardCancel")
	}
}

// TestRequestStopSoftFallsBackForNonPreferringCall: a soft stop with an
// in-flight call whose tool does not prefer to finish must fall back
// immediately, same as "nothing in flight" — a stop request is never
// silently ignored.
func TestRequestStopSoftFallsBackForNonPreferringCall(t *testing.T) {
	eng := newTestEngine(t, &preferFinishTool{namedFakeTool: namedFakeTool{name: "slow"}, prefer: false})
	eng.markInFlight("tu_1", "slow", json.RawMessage(`{}`))

	called := false
	if soft := eng.RequestStop(false, func() { called = true }); soft {
		t.Error("RequestStop(soft) with a non-preferring in-flight call reported soft, want immediate fallback")
	}
	if !called {
		t.Error("RequestStop(soft) with a non-preferring in-flight call did not fall back to hardCancel")
	}
}

// TestRequestStopSoftArmsWithoutCancellingWhenAllPrefer is the mechanism's
// whole point: every in-flight call prefers to finish, so the stop is
// deferred (armed) rather than taking effect immediately, and
// consumeGracefulStop reports it exactly once.
func TestRequestStopSoftArmsWithoutCancellingWhenAllPrefer(t *testing.T) {
	eng := newTestEngine(t, &preferFinishTool{namedFakeTool: namedFakeTool{name: "fast"}, prefer: true})
	eng.markInFlight("tu_1", "fast", json.RawMessage(`{}`))

	// Long enough that the test's own assertions run well before it could
	// fire and mask a bug as a false pass.
	var mu sync.Mutex
	fired := false
	restore := stubTimeAfterFunc(t, func(d time.Duration, f func()) *time.Timer {
		return time.AfterFunc(time.Hour, func() { mu.Lock(); fired = true; mu.Unlock(); f() })
	})
	defer restore()

	called := false
	if soft := eng.RequestStop(false, func() { called = true }); !soft {
		t.Fatal("RequestStop(soft) with only preferring calls in flight should arm rather than cancel immediately")
	}
	if called {
		t.Error("RequestStop(soft) must not call hardCancel while the graceful window is open")
	}
	mu.Lock()
	got := fired
	mu.Unlock()
	if got {
		t.Fatal("safety-net timer fired before the test could check the armed state")
	}

	if !eng.consumeGracefulStop() {
		t.Error("consumeGracefulStop() = false, want true after a soft stop was armed")
	}
	if eng.consumeGracefulStop() {
		t.Error("consumeGracefulStop() must only report the request once")
	}
}

// TestRequestStopGraceTimerFallsBackToHardCancel is the safety net: if the
// round never finishes (consumeGracefulStop is never called), the grace timer
// itself calls hardCancel so a soft stop can never hang a run.
func TestRequestStopGraceTimerFallsBackToHardCancel(t *testing.T) {
	eng := newTestEngine(t, &preferFinishTool{namedFakeTool: namedFakeTool{name: "fast"}, prefer: true})
	eng.markInFlight("tu_1", "fast", json.RawMessage(`{}`))

	fired := make(chan struct{})
	restore := stubTimeAfterFunc(t, func(d time.Duration, f func()) *time.Timer {
		// Fire on the next tick instead of waiting out the real grace period.
		return time.AfterFunc(time.Millisecond, func() { f(); close(fired) })
	})
	defer restore()

	called := make(chan struct{})
	if soft := eng.RequestStop(false, func() { close(called) }); !soft {
		t.Fatal("expected the soft path to arm")
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("grace-timer fallback never called hardCancel")
	}
	<-fired
}

// stubTimeAfterFunc swaps the package-level timeAfterFunc seam for the
// duration of a test, restoring it afterward.
func stubTimeAfterFunc(t *testing.T, fn func(time.Duration, func()) *time.Timer) func() {
	t.Helper()
	orig := timeAfterFunc
	timeAfterFunc = fn
	return func() { timeAfterFunc = orig }
}

// blockingPreferTool blocks in Execute until told to proceed, so a test can
// call RequestStop while it is genuinely in flight. It always prefers to
// finish.
type blockingPreferTool struct {
	namedFakeTool
	started chan struct{}
	proceed chan struct{}
}

func (b *blockingPreferTool) PreferFinish(json.RawMessage) bool { return true }
func (b *blockingPreferTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	close(b.started)
	<-b.proceed
	return tool.Result{Content: "done"}, nil
}

// TestRunEndsGracefullyAfterSoftStopAtRoundBoundary is the end-to-end version
// of the unit tests above: a soft RequestStop while a PreferFinish-true call
// is in flight lets that call finish and its result reach the conversation,
// then ends the run at the next safe point instead of starting the model's
// second turn — and never invokes the hard cancel at all.
func TestRunEndsGracefullyAfterSoftStopAtRoundBoundary(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "block", Input: json.RawMessage(`{}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{ // must never run
			{Type: provider.EventTextDelta, Text: "should not reach"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	bt := &blockingPreferTool{
		namedFakeTool: namedFakeTool{name: "block"},
		started:       make(chan struct{}),
		proceed:       make(chan struct{}),
	}
	reg := tool.NewRegistry()
	if err := reg.Register(bt); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hardCalled := false
	hardCancel := func() { hardCalled = true; cancel() }

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- eng.Run(ctx, conv, func(Event) {})
	}()

	select {
	case <-bt.started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool call never started")
	}

	if soft := eng.RequestStop(false, hardCancel); !soft {
		t.Fatal("RequestStop(soft) should arm while the preferring call is in flight")
	}
	close(bt.proceed)

	select {
	case runErr := <-runErrCh:
		if runErr == nil || !errors.Is(runErr, ErrInterrupted) {
			t.Errorf("Run() error = %v, want ErrInterrupted", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the round finished")
	}

	if hardCalled {
		t.Error("hardCancel was called even though the in-flight call preferred to finish")
	}
	if adapter.calls != 1 {
		t.Errorf("adapter.calls = %d, want 1: the second model turn must never have started", adapter.calls)
	}
}
