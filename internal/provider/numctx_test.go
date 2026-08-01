package provider

import (
	"context"
	"testing"
)

// recordingAdapter captures the last request it was asked to stream.
type recordingAdapter struct {
	last Request
}

func (recordingAdapter) Name() string { return "recording" }
func (a *recordingAdapter) Stream(_ context.Context, req Request) (<-chan Event, error) {
	a.last = req
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

// TestWithNumCtxStampsTheWindow is the P52.4 plumbing guard: the daemon builds
// one adapter but resolves the model — and therefore the serving window — per
// turn, so the per-run wrapper is what carries the window onto the request.
func TestWithNumCtxStampsTheWindow(t *testing.T) {
	base := &recordingAdapter{}
	a := WithNumCtx(base, 4096)
	if _, err := a.Stream(context.Background(), Request{Model: "small:1b"}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if base.last.NumCtx != 4096 {
		t.Errorf("num_ctx = %d, want 4096", base.last.NumCtx)
	}
	if a.Name() != "recording" {
		t.Errorf("Name() = %q, want the base adapter's name", a.Name())
	}
}

// TestWithNumCtxKeepsAnExplicitRequestValue: the wrapper supplies a default for
// a run, it never overrides a caller that set the field itself.
func TestWithNumCtxKeepsAnExplicitRequestValue(t *testing.T) {
	base := &recordingAdapter{}
	a := WithNumCtx(base, 4096)
	if _, err := a.Stream(context.Background(), Request{Model: "m", NumCtx: 65536}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if base.last.NumCtx != 65536 {
		t.Errorf("num_ctx = %d, want the request's own 65536", base.last.NumCtx)
	}
}

// TestWithNumCtxNoWindowAddsNoLayer: with nothing to say about the window the
// caller gets the base adapter back untouched, so a cloud provider (or a daemon
// that detected nothing) behaves exactly as before.
func TestWithNumCtxNoWindowAddsNoLayer(t *testing.T) {
	base := &recordingAdapter{}
	if got := WithNumCtx(base, 0); got != Adapter(base) {
		t.Errorf("WithNumCtx(base, 0) = %#v, want the base adapter unchanged", got)
	}
	if got := WithNumCtx(nil, 8192); got != nil {
		t.Errorf("WithNumCtx(nil, n) = %#v, want nil", got)
	}
}

// TestWithNumCtxUnwrapsToBase: the capability probes (RaiseContextWindow,
// CheckBackendHealth) must still reach the base adapter through this decorator,
// exactly as they do through retry and failover — the phased drive's escalation
// path depends on it.
func TestWithNumCtxUnwrapsToBase(t *testing.T) {
	base := &raiserAdapter{}
	a := WithNumCtx(base, 4096)
	if !RaiseContextWindow(a, 32768) {
		t.Fatal("must unwrap the num_ctx decorator to reach the base raiser")
	}
	if base.numCtx != 32768 {
		t.Errorf("base num_ctx = %d, want 32768", base.numCtx)
	}
}
