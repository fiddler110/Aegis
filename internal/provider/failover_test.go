package provider

import (
	"context"
	"errors"
	"testing"
)

// namedFakeAdapter is fakeAdapter (see retry_test.go) with a configurable
// name and a record of the last model it was asked to stream.
type namedFakeAdapter struct {
	name      string
	err       error // returned on every Stream call when set
	lastModel string
	callCount int
}

func (f *namedFakeAdapter) Name() string { return f.name }

func (f *namedFakeAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	f.callCount++
	f.lastModel = req.Model
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

func TestFailover_NoFallbacksReturnsPrimaryUnchanged(t *testing.T) {
	primary := &namedFakeAdapter{name: "primary"}
	got := WithFailover(primary, "", nil, nil)
	if got != Adapter(primary) {
		t.Fatalf("expected WithFailover with no fallbacks to return primary unchanged")
	}
}

func TestFailover_PrimarySucceedsNoSwitch(t *testing.T) {
	primary := &namedFakeAdapter{name: "primary"}
	fallback := &namedFakeAdapter{name: "fallback"}
	a := WithFailover(primary, "", []FallbackTarget{{Adapter: fallback}}, nil)

	if _, err := a.Stream(context.Background(), Request{Model: "m1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary.callCount != 1 || fallback.callCount != 0 {
		t.Fatalf("expected only primary called, got primary=%d fallback=%d", primary.callCount, fallback.callCount)
	}
}

func TestFailover_SwitchesOnPrimaryFailure(t *testing.T) {
	primary := &namedFakeAdapter{name: "primary", err: NewHTTPError("primary", 500, "", "down")}
	fallback := &namedFakeAdapter{name: "fallback"}
	a := WithFailover(primary, "primary-model", []FallbackTarget{{Adapter: fallback, Model: "fallback-model"}}, nil)

	ch, err := a.Stream(context.Background(), Request{Model: "primary-model"})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel from fallback")
	}
	if primary.callCount != 1 || fallback.callCount != 1 {
		t.Fatalf("expected both called once, got primary=%d fallback=%d", primary.callCount, fallback.callCount)
	}
	if fallback.lastModel != "fallback-model" {
		t.Fatalf("expected fallback to use its model override, got %q", fallback.lastModel)
	}
}

func TestFailover_AllTargetsFailReturnsLastError(t *testing.T) {
	primary := &namedFakeAdapter{name: "primary", err: NewHTTPError("primary", 500, "", "down")}
	fallback := &namedFakeAdapter{name: "fallback", err: NewHTTPError("fallback", 503, "", "also down")}
	a := WithFailover(primary, "", []FallbackTarget{{Adapter: fallback}}, nil)

	_, err := a.Stream(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error when every target fails")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Provider != "fallback" {
		t.Fatalf("expected the last target's error, got %v", err)
	}
}

// TestFailover_CancelledContextStopsAtFirstTarget is the P61.5 regression: a
// cancelled context used to walk the entire chain, sending a request and
// logging a WARN per hop before surfacing the cancellation. Failover must
// notice the caller has gone away and stop.
func TestFailover_CancelledContextStopsAtFirstTarget(t *testing.T) {
	primary := &namedFakeAdapter{name: "primary", err: NewHTTPError("primary", 500, "", "down")}
	fb1 := &namedFakeAdapter{name: "fb1"}
	fb2 := &namedFakeAdapter{name: "fb2"}
	a := WithFailover(primary, "", []FallbackTarget{{Adapter: fb1}, {Adapter: fb2}}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Stream(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if primary.callCount != 0 || fb1.callCount != 0 || fb2.callCount != 0 {
		t.Fatalf("a cancelled context still reached the chain: primary=%d fb1=%d fb2=%d",
			primary.callCount, fb1.callCount, fb2.callCount)
	}
}

// TestFailover_CancelledMidChainKeepsTheTargetError covers cancellation arriving
// after a target has already failed: the caller gets the cancellation (that is
// what ended the attempt) without losing the backend failure that preceded it.
func TestFailover_CancelledMidChainKeepsTheTargetError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	primary := &cancellingAdapter{name: "primary", cancel: cancel, err: NewHTTPError("primary", 500, "", "down")}
	fallback := &namedFakeAdapter{name: "fallback"}
	a := WithFailover(primary, "", []FallbackTarget{{Adapter: fallback}}, nil)
	defer cancel()

	_, err := a.Stream(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Provider != "primary" {
		t.Errorf("the failed target's error was lost: %v", err)
	}
	if fallback.callCount != 0 {
		t.Errorf("fallback was tried after cancellation, callCount=%d", fallback.callCount)
	}
}

// cancellingAdapter fails like namedFakeAdapter but cancels the run's context on
// the way out, standing in for a caller that aborts mid-chain.
type cancellingAdapter struct {
	name      string
	cancel    context.CancelFunc
	err       error
	callCount int
}

func (c *cancellingAdapter) Name() string { return c.name }

func (c *cancellingAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	c.callCount++
	c.cancel()
	return nil, c.err
}

func TestFailover_ChainsThroughMultipleFallbacks(t *testing.T) {
	primary := &namedFakeAdapter{name: "primary", err: NewHTTPError("primary", 500, "", "down")}
	fb1 := &namedFakeAdapter{name: "fb1", err: NewHTTPError("fb1", 500, "", "also down")}
	fb2 := &namedFakeAdapter{name: "fb2"}
	a := WithFailover(primary, "", []FallbackTarget{{Adapter: fb1}, {Adapter: fb2}}, nil)

	if _, err := a.Stream(context.Background(), Request{}); err != nil {
		t.Fatalf("expected second fallback to succeed, got %v", err)
	}
	if primary.callCount != 1 || fb1.callCount != 1 || fb2.callCount != 1 {
		t.Fatalf("expected each target tried once in order, got primary=%d fb1=%d fb2=%d",
			primary.callCount, fb1.callCount, fb2.callCount)
	}
}
