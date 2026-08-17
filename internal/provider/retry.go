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

// foregroundExtraRetries is how many attempts beyond the configured baseline a
// foreground call — the user's own turn — is allowed (P67.3). Two, not more:
// with the shipped defaults (500ms base, 30s cap) the added sleeps are ~8s and
// ~16s, so the worst-case retry sequence stays around half a minute and remains
// comfortably inside the 900s MaxTurnStall bound that backstops the turn. The
// number is expressed as a delta rather than an absolute so an operator who
// lowers provider retries still gets a lower foreground count.
const foregroundExtraRetries = 2

// backgroundMaxRetries and backgroundMaxDelay are the fail-fast settings for a
// call nobody is waiting on (P67.3). One retry covers the genuinely transient
// case — a dropped connection, a single 503 — and stops there, because the
// second and third attempts of a title generation against a rate-limited
// backend are pure added load during exactly the window the backend is
// struggling. The delay cap comes down with it for the same reason: a
// background call holding a slot for 30s to retry is the cost being avoided.
//
// Both are floors-not-ceilings: a baseline configured *below* these keeps its
// own smaller numbers (see policyFor), because an operator who turned retries
// down meant it.
const (
	backgroundMaxRetries = 1
	backgroundMaxDelay   = 5 * time.Second
)

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
// disabled it returns inner unchanged — including for foreground calls, whose
// P67.3 bonus attempts are a delta on a configured baseline and so are nothing
// when the baseline is nothing. An operator who turned retries off meant it.
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

// Unwrap exposes the wrapped adapter so capability probes (e.g.
// provider.RaiseContextWindow) can reach the base adapter through this
// decorator.
func (r *retryAdapter) Unwrap() Adapter { return r.inner }

// policyFor derives the policy governing one call from the configured baseline
// and the call's purpose (P67.3). The baseline is what an operator configured,
// so it stays the reference point in every direction: foreground adds attempts
// to it, background takes them away, and attended — the default, including
// every untagged call — is the baseline itself, unchanged.
func policyFor(base RetryPolicy, p Purpose) RetryPolicy {
	switch urgencyOf(p) {
	case urgencyForeground:
		base.MaxRetries += foregroundExtraRetries
	case urgencyBackground:
		if base.MaxRetries > backgroundMaxRetries {
			base.MaxRetries = backgroundMaxRetries
		}
		if base.MaxDelay > backgroundMaxDelay {
			base.MaxDelay = backgroundMaxDelay
		}
	}
	return base
}

// Stream implements Adapter, retrying transient errors.
func (r *retryAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	purpose := EffectivePurpose(ctx, req)
	policy := policyFor(r.policy, purpose)
	for attempt := 0; ; attempt++ {
		ch, err := r.inner.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
		if attempt >= policy.MaxRetries || !retryable(err) {
			return nil, err
		}
		delay := backoff(policy, attempt, err)
		r.logger.Warn("provider request failed; retrying",
			"provider", r.inner.Name(), "purpose", string(purpose),
			"attempt", attempt+1, "max", policy.MaxRetries,
			"delay", delay, "err", err)
		if serr := r.sleep(ctx, delay); serr != nil {
			return nil, serr
		}
	}
}

// backoff computes the delay before the next attempt under policy. A
// server-provided Retry-After takes precedence; otherwise exponential backoff
// with equal jitter.
//
// The Retry-After clamp at MaxDelay is deliberate and stays (P67.3 says so
// explicitly): it is what keeps provider backoff inside the 900s MaxTurnStall
// bound without the retry path needing heartbeats at all. Honoring the header
// unbounded would turn a backend's "come back in an hour" into a fatal stalled
// turn. It now clamps to the *purpose's* MaxDelay, which only ever tightens it.
func backoff(policy RetryPolicy, attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		if apiErr.RetryAfter > policy.MaxDelay {
			return policy.MaxDelay
		}
		return apiErr.RetryAfter
	}
	d := policy.BaseDelay << attempt // BaseDelay * 2^attempt
	if d <= 0 || d > policy.MaxDelay {
		d = policy.MaxDelay
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
