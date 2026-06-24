package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeAdapter returns a scripted error (or success) on each Stream call.
type fakeAdapter struct {
	calls int
	errs  []error // errs[i] returned on call i; nil -> success
}

func (f *fakeAdapter) Name() string { return "fake" }

func (f *fakeAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

// newTestRetry wraps inner and replaces the sleep with a recorder so tests run
// instantly.
func newTestRetry(inner Adapter, policy RetryPolicy, delays *[]time.Duration) *retryAdapter {
	r := WithRetry(inner, policy, nil).(*retryAdapter)
	r.sleep = func(ctx context.Context, d time.Duration) error {
		if delays != nil {
			*delays = append(*delays, d)
		}
		return ctx.Err()
	}
	return r
}

func TestRetry_SucceedsAfterRateLimit(t *testing.T) {
	f := &fakeAdapter{errs: []error{
		NewHTTPError("fake", 429, "", "rate limited"),
		nil,
	}}
	r := newTestRetry(f, RetryPolicy{MaxRetries: 4, BaseDelay: time.Millisecond, MaxDelay: time.Second}, nil)

	ch, err := r.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
	if f.calls != 2 {
		t.Fatalf("expected 2 calls (1 retry), got %d", f.calls)
	}
}

func TestRetry_NonRetryableFailsFast(t *testing.T) {
	f := &fakeAdapter{errs: []error{NewHTTPError("fake", 400, "", "bad request")}}
	r := newTestRetry(f, RetryPolicy{MaxRetries: 4, BaseDelay: time.Millisecond, MaxDelay: time.Second}, nil)

	if _, err := r.Stream(context.Background(), Request{}); err == nil {
		t.Fatal("expected error for 400")
	}
	if f.calls != 1 {
		t.Fatalf("expected exactly 1 call (no retry on 400), got %d", f.calls)
	}
}

func TestRetry_ExhaustsRetries(t *testing.T) {
	f := &fakeAdapter{errs: []error{
		NewHTTPError("fake", 503, "", "x"),
		NewHTTPError("fake", 503, "", "x"),
		NewHTTPError("fake", 503, "", "x"),
	}}
	r := newTestRetry(f, RetryPolicy{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Second}, nil)

	if _, err := r.Stream(context.Background(), Request{}); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if f.calls != 3 { // initial + 2 retries
		t.Fatalf("expected 3 calls, got %d", f.calls)
	}
}

func TestRetry_HonorsRetryAfter(t *testing.T) {
	f := &fakeAdapter{errs: []error{
		NewHTTPError("fake", 429, "2", "slow down"),
		nil,
	}}
	var delays []time.Duration
	r := newTestRetry(f, RetryPolicy{MaxRetries: 4, BaseDelay: time.Millisecond, MaxDelay: time.Hour}, &delays)

	if _, err := r.Stream(context.Background(), Request{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Fatalf("expected a single 2s Retry-After delay, got %v", delays)
	}
}

func TestRetry_ContextCanceledNotRetried(t *testing.T) {
	f := &fakeAdapter{errs: []error{NewTransportError("fake", context.Canceled)}}
	r := newTestRetry(f, RetryPolicy{MaxRetries: 4, BaseDelay: time.Millisecond, MaxDelay: time.Second}, nil)

	if _, err := r.Stream(context.Background(), Request{}); err == nil {
		t.Fatal("expected error")
	}
	if f.calls != 1 {
		t.Fatalf("context cancellation must not retry, got %d calls", f.calls)
	}
}

func TestAPIError_Retryable(t *testing.T) {
	cases := []struct {
		err  *APIError
		want bool
	}{
		{NewHTTPError("p", 429, "", ""), true},
		{NewHTTPError("p", 408, "", ""), true},
		{NewHTTPError("p", 500, "", ""), true},
		{NewHTTPError("p", 503, "", ""), true},
		{NewHTTPError("p", 400, "", ""), false},
		{NewHTTPError("p", 401, "", ""), false},
		{NewHTTPError("p", 404, "", ""), false},
		{NewTransportError("p", errors.New("connection refused")), true},
		{NewTransportError("p", context.Canceled), false},
		{NewTransportError("p", context.DeadlineExceeded), false},
	}
	for _, c := range cases {
		if got := c.err.Retryable(); got != c.want {
			t.Errorf("status=%d err=%v: Retryable()=%v want %v", c.err.StatusCode, c.err.Err, got, c.want)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("5"); d != 5*time.Second {
		t.Errorf("numeric: got %v", d)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("empty: got %v", d)
	}
	if d := parseRetryAfter("garbage"); d != 0 {
		t.Errorf("garbage: got %v", d)
	}
}
