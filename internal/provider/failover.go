package provider

import (
	"context"
	"errors"
	"log/slog"
	"sync"
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
	targets []FallbackTarget // targets[0] is the primary; its Model field is never applied (see Stream)
	logger  *slog.Logger

	// lastFallbackMu guards lastFallbackModel/lastFallbackActive (P52.6:
	// s.adapter, and therefore this decorator, is shared across concurrent
	// sessions/turns). Set at the end of every successful Stream — see
	// LastServedFallback. active is a separate bool rather than inferred from
	// a non-empty model, since a fallback target with no Model override
	// (FallbackTarget.Model == "") can legitimately serve under an empty or
	// passed-through model string.
	lastFallbackMu     sync.Mutex
	lastFallbackModel  string
	lastFallbackActive bool
}

// WithFailover chains primary and fallbacks so that a Stream failure on one
// target is retried against the next. Returns primary unchanged if fallbacks
// is empty (P5.9).
func WithFailover(primary Adapter, fallbacks []FallbackTarget, logger *slog.Logger) Adapter {
	if len(fallbacks) == 0 {
		return primary
	}
	if logger == nil {
		logger = slog.Default()
	}
	targets := make([]FallbackTarget, 0, len(fallbacks)+1)
	targets = append(targets, FallbackTarget{Adapter: primary})
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

// LastServedFallback implements provider.FailoverObserver: the model name a
// fallback target actually served the most recent successful Stream call
// with, and whether that was a fallback at all (false when the primary
// served it, or before any call has completed). Read by
// provider.ActiveFailoverModel, which server/engine_build.go consults so the
// compaction trigger can be sized against the model actually generating
// output rather than the primary's window for the rest of a run spent on a
// fallback (LLM-11's second half).
func (f *failoverAdapter) LastServedFallback() (model string, active bool) {
	f.lastFallbackMu.Lock()
	defer f.lastFallbackMu.Unlock()
	return f.lastFallbackModel, f.lastFallbackActive
}

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
		if i > 0 && t.Model != "" {
			// A fallback provider commonly serves a different model id than
			// whatever the caller asked for (the primary's default, a routed
			// small model, a persona pin) — that override belongs here.
			//
			// The primary (i==0) must NEVER get this treatment: it used to
			// carry its own Model override (providerfactory.Build passed
			// cfg.Provider.Model as the primary's FallbackTarget.Model), which
			// silently overwrote every caller-chosen model — task routing's
			// small_model, the guard's model, a persona's model pin — back to
			// the primary's default the instant any fallback was configured,
			// with no error and no log line. A request to the primary must
			// reach it exactly as the caller built it, identical to what
			// happens when no fallback is configured at all (WithFailover
			// returns primary unwrapped in that case).
			r.Model = t.Model
		}
		if i > 0 {
			// LLM-11: NumCtx was resolved (by numCtxAdapter, upstream of this
			// decorator) for the *primary* model. Riding it to a fallback serves
			// that model with a window sized for a different model entirely —
			// live-confirmed 2026-09-04: a fallback pinned to num_ctx 32768
			// received num_ctx 16384, the primary's window, in 4/4 requests.
			// Clearing it here lets the fallback's own adapter fall through to
			// its own configured/detected default instead.
			r.NumCtx = 0
		}
		ch, err := t.Adapter.Stream(ctx, r)
		if err == nil {
			f.lastFallbackMu.Lock()
			f.lastFallbackModel = r.Model
			f.lastFallbackActive = i > 0
			f.lastFallbackMu.Unlock()
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
