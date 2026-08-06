package provider

import (
	"context"
	"errors"
	"log/slog"
)

// FallbackTarget is one adapter in a failover chain, with an optional model
// override applied when this target is used (fallback providers often use a
// different model id than the primary).
type FallbackTarget struct {
	Adapter Adapter
	Model   string // "" keeps the request's original model
}

// failoverAdapter tries a primary adapter, then each fallback in order, on
// synchronous Stream failure. It only switches on errors surfaced before any
// tokens have streamed (the same boundary retryAdapter uses), so partial
// output is never replayed from mid-stream. Each target is expected to
// already carry its own retry policy (see providerfactory.Build) — failover
// only kicks in once a target's own retries are exhausted.
type failoverAdapter struct {
	targets []FallbackTarget // targets[0] is the primary
	logger  *slog.Logger
}

// WithFailover chains primary and fallbacks so that a Stream failure on one
// target is retried against the next. Returns primary unchanged if fallbacks
// is empty (P5.9).
func WithFailover(primary Adapter, primaryModel string, fallbacks []FallbackTarget, logger *slog.Logger) Adapter {
	if len(fallbacks) == 0 {
		return primary
	}
	if logger == nil {
		logger = slog.Default()
	}
	targets := make([]FallbackTarget, 0, len(fallbacks)+1)
	targets = append(targets, FallbackTarget{Adapter: primary, Model: primaryModel})
	targets = append(targets, fallbacks...)
	return &failoverAdapter{targets: targets, logger: logger}
}

// Name reports the primary adapter's name; the log line on an actual
// switch-over names the specific fallback used.
func (f *failoverAdapter) Name() string { return f.targets[0].Adapter.Name() }

// Unwrap exposes the primary adapter so capability probes (e.g.
// provider.RaiseContextWindow) reach the base adapter through this decorator.
// Escalating only the primary's window is the intended scope: the phased drive
// runs against the primary, and a fallback target (typically a cloud model with
// a fixed window) has no runtime-tunable num_ctx anyway.
func (f *failoverAdapter) Unwrap() Adapter { return f.targets[0].Adapter }

// Stream implements Adapter, trying each target in order until one succeeds.
func (f *failoverAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	var lastErr error
	for i, t := range f.targets {
		// P61.5: stop at the first target once the caller has gone away. Without
		// this a cancelled run walked the whole chain, taking a (guaranteed to
		// fail) request and a WARN per hop before surfacing the cancellation.
		// Reported as the context's error rather than the last target's, since
		// that is what actually ended the attempt.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return nil, errors.Join(err, lastErr)
			}
			return nil, err
		}
		r := req
		if t.Model != "" {
			r.Model = t.Model
		}
		ch, err := t.Adapter.Stream(ctx, r)
		if err == nil {
			if i > 0 {
				f.logger.Warn("provider failover: switched adapter",
					"from", f.targets[0].Adapter.Name(), "to", t.Adapter.Name(), "attempt", i+1)
			}
			return ch, nil
		}
		lastErr = err
		if i < len(f.targets)-1 {
			f.logger.Warn("provider failover: adapter exhausted retries, trying next",
				"adapter", t.Adapter.Name(), "next", f.targets[i+1].Adapter.Name(), "err", err)
		}
	}
	return nil, lastErr
}
