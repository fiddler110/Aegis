package provider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// gatedAdapter is a base adapter whose streams stay open until the test
// releases them, so a test can observe how many are concurrently in flight.
type gatedAdapter struct {
	mu      sync.Mutex
	release map[int]chan struct{} // per-stream release signal
	next    int

	inFlight atomic.Int32
	peak     atomic.Int32
	started  atomic.Int32

	err error // returned synchronously from Stream when non-nil
}

func newGatedAdapter() *gatedAdapter {
	return &gatedAdapter{release: map[int]chan struct{}{}}
}

func (*gatedAdapter) Name() string { return "gated" }

func (g *gatedAdapter) Stream(ctx context.Context, _ Request) (<-chan Event, error) {
	if g.err != nil {
		return nil, g.err
	}
	g.started.Add(1)
	now := g.inFlight.Add(1)
	for {
		peak := g.peak.Load()
		if now <= peak || g.peak.CompareAndSwap(peak, now) {
			break
		}
	}

	g.mu.Lock()
	id := g.next
	g.next++
	gate := make(chan struct{})
	g.release[id] = gate
	g.mu.Unlock()

	out := make(chan Event)
	go func() {
		defer close(out)
		defer g.inFlight.Add(-1)
		select {
		case <-gate:
		case <-ctx.Done():
		}
		select {
		case out <- Event{Type: EventDone}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

// releaseAll lets every stream started so far finish.
func (g *gatedAdapter) releaseAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, gate := range g.release {
		close(gate)
		delete(g.release, id)
	}
}

// drain reads a stream to completion, which is what releases the admission slot.
func drain(ch <-chan Event) {
	for range ch {
	}
}

// TestAdmissionBoundsConcurrentStreams is the P59.9 guarantee itself: with a
// depth of 1, a second caller does not reach the backend until the first
// stream has finished — not merely until Stream returned, since the request
// occupies the model for as long as it is generating.
func TestAdmissionBoundsConcurrentStreams(t *testing.T) {
	base := newGatedAdapter()
	a := WithAdmissionControl(base, 1, quietLogger())

	first, err := a.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	secondStarted := make(chan struct{})
	go func() {
		ch, err := a.Stream(context.Background(), Request{})
		if err != nil {
			t.Errorf("second Stream: %v", err)
			return
		}
		close(secondStarted)
		drain(ch)
	}()

	// The second call must still be queued while the first stream is open.
	select {
	case <-secondStarted:
		t.Fatal("second Stream was admitted while the first was still streaming")
	case <-time.After(100 * time.Millisecond):
	}
	if got := base.started.Load(); got != 1 {
		t.Fatalf("backend saw %d request(s) while depth=1 and one was in flight, want 1", got)
	}

	base.releaseAll()
	drain(first)

	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("second Stream never admitted after the first finished")
	}
	base.releaseAll()

	if peak := base.peak.Load(); peak > 1 {
		t.Errorf("peak concurrent backend streams = %d, want <= 1", peak)
	}
}

// TestAdmissionAllowsConfiguredDepth: the bound is the configured number, not
// serialization — an operator who raises it gets the parallelism they asked for.
func TestAdmissionAllowsConfiguredDepth(t *testing.T) {
	base := newGatedAdapter()
	a := WithAdmissionControl(base, 2, quietLogger())

	var streams []<-chan Event
	for i := 0; i < 2; i++ {
		ch, err := a.Stream(context.Background(), Request{})
		if err != nil {
			t.Fatalf("Stream %d: %v", i, err)
		}
		streams = append(streams, ch)
	}
	if got := base.started.Load(); got != 2 {
		t.Fatalf("backend saw %d request(s), want 2 admitted concurrently at depth 2", got)
	}
	base.releaseAll()
	for _, ch := range streams {
		drain(ch)
	}
}

// TestAdmissionReleasesOnSynchronousError: a backend that fails before any
// stream exists must not leak the slot, or one transport error would wedge
// every later request behind a permanently-held semaphore.
func TestAdmissionReleasesOnSynchronousError(t *testing.T) {
	base := newGatedAdapter()
	base.err = errors.New("connection refused")
	a := WithAdmissionControl(base, 1, quietLogger())

	if _, err := a.Stream(context.Background(), Request{}); err == nil {
		t.Fatal("expected the base adapter's error")
	}

	base.err = nil
	done := make(chan struct{})
	go func() {
		defer close(done)
		ch, err := a.Stream(context.Background(), Request{})
		if err != nil {
			t.Errorf("Stream after error: %v", err)
			return
		}
		base.releaseAll()
		drain(ch)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("slot was not released after a synchronous Stream error")
	}
}

// TestAdmissionReleasesOnAbandonedStream: a run cancelled mid-stream leaves a
// consumer that never drains. The slot must come back anyway — otherwise a
// cancelled turn (Ctrl-C, a budget abort) permanently costs one unit of the
// depth, and at the local default of 1 that is the whole daemon.
func TestAdmissionReleasesOnAbandonedStream(t *testing.T) {
	base := newGatedAdapter()
	a := WithAdmissionControl(base, 1, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := a.Stream(ctx, Request{}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Never read from the returned channel; just cancel, as an aborted run does.
	cancel()

	admitted := make(chan struct{})
	go func() {
		ch, err := a.Stream(context.Background(), Request{})
		if err != nil {
			t.Errorf("Stream after abandon: %v", err)
			return
		}
		close(admitted)
		base.releaseAll()
		drain(ch)
	}()
	select {
	case <-admitted:
	case <-time.After(2 * time.Second):
		t.Fatal("slot was not released after the caller abandoned a cancelled stream")
	}
}

// TestAdmissionQueueRespectsContext: a caller whose own context dies while
// queued gets its error back rather than waiting for a slot it no longer wants.
func TestAdmissionQueueRespectsContext(t *testing.T) {
	base := newGatedAdapter()
	a := WithAdmissionControl(base, 1, quietLogger())

	first, err := a.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := a.Stream(ctx, Request{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("queued Stream error = %v, want context.DeadlineExceeded", err)
	}

	base.releaseAll()
	drain(first)
}

// TestAdmissionCancelledContextNeverAcquiresASlot is the P61.5 regression. The
// acquire used to be a three-way select — sem-send, ctx.Done(), default — and a
// select picks *uniformly at random* among its ready cases, so with a free slot
// and an already-cancelled context the dead request won a slot (and a full
// backend turn) roughly half the time. It read as a cancellation check and was
// not one.
//
// The loop is what makes the test deterministic: a single iteration would pass
// against the buggy code half the time, and 200 consecutive wins is not a thing
// a uniform choice does.
func TestAdmissionCancelledContextNeverAcquiresASlot(t *testing.T) {
	base := newGatedAdapter()
	a := WithAdmissionControl(base, 4, quietLogger()) // depth > 1: a slot is always free

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for i := 0; i < 200; i++ {
		if _, err := a.Stream(ctx, Request{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("iteration %d: Stream error = %v, want context.Canceled", i, err)
		}
	}
	if n := base.started.Load(); n != 0 {
		t.Errorf("a cancelled request reached the backend %d time(s)", n)
	}
}

// TestAdmissionUnboundedAddsNoLayer: "unbounded" must be the literal base
// adapter, so a cloud provider pays nothing — not even a forwarding goroutine
// per stream — for a knob that only means something locally.
func TestAdmissionUnboundedAddsNoLayer(t *testing.T) {
	base := newGatedAdapter()
	if got := WithAdmissionControl(base, 0, quietLogger()); got != Adapter(base) {
		t.Errorf("WithAdmissionControl(base, 0) = %#v, want the base adapter unchanged", got)
	}
	if got := WithAdmissionControl(nil, 4, quietLogger()); got != nil {
		t.Errorf("WithAdmissionControl(nil, n) = %#v, want nil", got)
	}
}

// TestAdmissionUnwrapsToBase: the capability probes (RaiseContextWindow,
// RaisedContextWindow, CheckBackendHealth) walk Unwrap() chains, so a decorator
// that did not implement it would silently disable context escalation and the
// local-server health probe on exactly the setups this decorator targets.
func TestAdmissionUnwrapsToBase(t *testing.T) {
	base := newGatedAdapter()
	a := WithAdmissionControl(base, 1, quietLogger())
	u, ok := a.(interface{ Unwrap() Adapter })
	if !ok {
		t.Fatal("admission adapter does not implement Unwrap() Adapter")
	}
	if u.Unwrap() != Adapter(base) {
		t.Error("Unwrap() did not return the base adapter")
	}
	if a.Name() != "gated" {
		t.Errorf("Name() = %q, want the base adapter's name", a.Name())
	}
}
