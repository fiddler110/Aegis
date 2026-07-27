package provider

import (
	"context"
	"log/slog"
	"testing"
)

// raiserAdapter is a minimal Adapter that records the last num_ctx it was raised
// to, so the unwrap-chain test can assert the escalation reached the base.
type raiserAdapter struct {
	numCtx int
}

func (raiserAdapter) Name() string { return "raiser" }
func (raiserAdapter) Stream(context.Context, Request) (<-chan Event, error) {
	return nil, nil
}
func (r *raiserAdapter) RaiseContextWindow(n int) bool {
	if n > r.numCtx {
		r.numCtx = n
		return true
	}
	return false
}

// plainAdapter implements Adapter but not ContextWindowRaiser and cannot be
// unwrapped — RaiseContextWindow must report false for it.
type plainAdapter struct{}

func (plainAdapter) Name() string { return "plain" }
func (plainAdapter) Stream(context.Context, Request) (<-chan Event, error) {
	return nil, nil
}

// TestRaiseContextWindow_UnwrapsDecorators is the P47.5(b) plumbing guard: the
// phased drive escalates num_ctx through provider.RaiseContextWindow, but the
// adapter it holds is the retry- (and sometimes failover-) wrapped one, not the
// raw Ollama adapter. RaiseContextWindow must unwrap those decorators to reach a
// base that implements ContextWindowRaiser, and report false for an adapter that
// can't raise its window at all.
func TestRaiseContextWindow_UnwrapsDecorators(t *testing.T) {
	logger := slog.Default()

	// Bare raiser: raised when n grows, refused when it doesn't.
	base := &raiserAdapter{}
	if !RaiseContextWindow(base, 8192) {
		t.Fatal("bare raiser must grow from 0 to 8192")
	}
	if base.numCtx != 8192 {
		t.Fatalf("base num_ctx = %d, want 8192", base.numCtx)
	}
	if RaiseContextWindow(base, 4096) {
		t.Error("must not grow to a smaller window")
	}

	// Through the retry decorator.
	wrapped := WithRetry(&raiserAdapter{}, DefaultRetryPolicy(), logger)
	if !RaiseContextWindow(wrapped, 16384) {
		t.Error("must unwrap the retry decorator to reach the base raiser")
	}

	// Through retry + failover (targets[0] is the primary the drive runs on).
	inner := WithRetry(&raiserAdapter{}, DefaultRetryPolicy(), logger)
	fo := WithFailover(inner, "primary-model",
		[]FallbackTarget{{Adapter: plainAdapter{}, Model: "fallback"}}, logger)
	if !RaiseContextWindow(fo, 32768) {
		t.Error("must unwrap failover→retry→base to escalate the primary's window")
	}

	// A plain adapter with no raiser anywhere in the chain reports false.
	if RaiseContextWindow(plainAdapter{}, 65536) {
		t.Error("an adapter that can't raise its window must report false")
	}
	if RaiseContextWindow(WithRetry(plainAdapter{}, DefaultRetryPolicy(), logger), 65536) {
		t.Error("wrapping a non-raiser must still report false")
	}
}
