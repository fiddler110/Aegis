package provider

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"
)

// RetryPolicy configures the retry decorator.
type RetryPolicy struct {
	MaxRetries int           // retries after the initial attempt; <=0 disables
	BaseDelay  time.Duration // first backoff step
	MaxDelay   time.Duration // cap on a single backoff step
}

// DefaultRetryPolicy returns sensible defaults: up to 4 retries with
// exponential backoff from 500ms, capped at 30s.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxRetries: 4, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
}

// retryAdapter wraps an Adapter, retrying transient failures returned from
// Stream. It only retries errors surfaced synchronously (non-2xx status or
// transport failure) before any tokens have streamed, so partial output is
// never replayed.
type retryAdapter struct {
	inner  Adapter
	policy RetryPolicy
	logger *slog.Logger
	sleep  func(context.Context, time.Duration) error // overridable for tests
}

// WithRetry decorates inner with retry/backoff per policy. If retries are
// disabled it returns inner unchanged.
func WithRetry(inner Adapter, policy RetryPolicy, logger *slog.Logger) Adapter {
	if policy.MaxRetries <= 0 {
		return inner
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = 500 * time.Millisecond
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &retryAdapter{inner: inner, policy: policy, logger: logger, sleep: sleepCtx}
}

// Name implements Adapter.
func (r *retryAdapter) Name() string { return r.inner.Name() }

// Stream implements Adapter, retrying transient errors.
func (r *retryAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	for attempt := 0; ; attempt++ {
		ch, err := r.inner.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
		if attempt >= r.policy.MaxRetries || !retryable(err) {
			return nil, err
		}
		delay := r.backoff(attempt, err)
		r.logger.Warn("provider request failed; retrying",
			"provider", r.inner.Name(), "attempt", attempt+1, "max", r.policy.MaxRetries,
			"delay", delay, "err", err)
		if serr := r.sleep(ctx, delay); serr != nil {
			return nil, serr
		}
	}
}

// backoff computes the delay before the next attempt. A server-provided
// Retry-After takes precedence; otherwise exponential backoff with equal jitter.
func (r *retryAdapter) backoff(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		if apiErr.RetryAfter > r.policy.MaxDelay {
			return r.policy.MaxDelay
		}
		return apiErr.RetryAfter
	}
	d := r.policy.BaseDelay << attempt // BaseDelay * 2^attempt
	if d <= 0 || d > r.policy.MaxDelay {
		d = r.policy.MaxDelay
	}
	// Equal jitter: half fixed, half random in [0, d/2).
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	return false
}

// sleepCtx waits for d or until ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
