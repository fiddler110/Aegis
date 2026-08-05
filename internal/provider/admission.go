package provider

import (
	"context"
	"log/slog"
	"time"
)

// admissionAdapter bounds how many requests are in flight against one backend
// at a time (P59.9), queueing the rest rather than letting them pile onto the
// server.
//
// It exists because nothing else in the harness owns that number for a *local*
// backend. The daemon serves concurrent sessions, swarm spawns sub-agents, and
// the guard/compaction/title passes are requests of their own — each built
// believing it owns the full detected serving window (WithNumCtx /
// engine_build.go). Against a cloud endpoint that belief is harmless: the
// provider fans the requests out across its own fleet. Against one Ollama
// server it is a claim on one GPU that N other requests are making at the
// same time.
//
// **The default of 1 is a latency policy, not a correctness one.** This
// distinction was measured (P59.10 batch, 2026-08-05) rather than assumed, and
// the measurement corrected what this comment used to assert. The original
// reasoning was that concurrency splits the KV cache across
// OLLAMA_NUM_PARALLEL slots and so silently truncates a request sized against
// the full window (the P59.1 failure). On Ollama 0.30.10 / qwen3:14b /
// num_ctx 16384, that does not reproduce: four concurrent ~12k-token requests,
// each carrying a passphrase in its *first* tokens — the region truncation
// drops first — all returned it verbatim, with identical
// prompt_eval_count (12034) and zero failures. Nothing was truncated and
// nothing was evicted.
//
// What concurrency does cost is per-request latency, and the aggregate gain is
// far short of linear (same host, 4 distinct 1.5k-token prompts):
//
//	K=1  11.18 tok/s aggregate   p50 latency  5.72s
//	K=2  15.60 tok/s (1.40x)     p50 latency  6.73s
//	K=4  17.85 tok/s (1.60x)     p50 latency  9.82s, max 14.34s
//
// So a second in-flight request buys ~40% aggregate throughput and pays for it
// with ~70% worse turn latency at K=4. For an interactive agent — where one
// turn's latency is the thing a user feels — that trade is bad, which is why
// the local default stays 1. It is *not* bad for a batch of independent
// sub-agents, so an operator running swarm work on a local backend has a real
// reason to raise provider.max_concurrent_requests, and no correctness reason
// not to. Prefix-cache reuse survives concurrency intact (continuations of a
// shared conversation kept 29-47ms prefills at K=1/2/4), so raising the depth
// does not forfeit P35.4.
//
// swarm.AdaptiveLimiter is deliberately not the instrument for this: it bounds
// *spawns*, reactively, by observing whether measured speedup tracks n. That is
// the right question for "is this host CPU-bound" and the wrong one for a fixed
// VRAM budget, which is not something you discover after the fact. This is a
// hard bound applied at the adapter layer, so every caller — sessions, swarm,
// drive, guard, compaction — passes through it without any of them needing to
// know it exists.
//
// It deliberately does not detect VRAM or infer a capacity; P20.3 and P17.5
// both rejected that and their reasons still hold. A queue depth is a policy the
// operator sets.
type admissionAdapter struct {
	base   Adapter
	sem    chan struct{}
	logger *slog.Logger
}

// slowAdmissionWait is how long a request may queue before the wait is worth
// telling the operator about at WARN. Below it, a wait is the mechanism working
// as intended and is logged at DEBUG; above it, the configured depth is
// plausibly too low (or something upstream is spawning more concurrent work
// than a single local server can absorb) and a silent multi-minute stall would
// otherwise look like a hung model.
const slowAdmissionWait = 60 * time.Second

// WithAdmissionControl returns base wrapped so at most maxInFlight of its
// streams run concurrently; the rest block in Stream until a slot frees.
// maxInFlight <= 0 (or a nil base) returns base unchanged, so "unbounded" adds
// no layer at all.
//
// The slot is held for the whole life of the stream, not just until Stream
// returns — the request occupies the backend until its last token, which is the
// thing being bounded. It is released when the returned channel is closed or
// ctx is cancelled, whichever comes first.
//
// Place this *inside* the retry decorator (WithRetry(WithAdmissionControl(...)))
// so a retry's backoff sleep does not sit on a slot the next queued caller could
// be using.
func WithAdmissionControl(base Adapter, maxInFlight int, logger *slog.Logger) Adapter {
	if base == nil || maxInFlight <= 0 {
		return base
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &admissionAdapter{
		base:   base,
		sem:    make(chan struct{}, maxInFlight),
		logger: logger,
	}
}

// AdmissionDepth reports the in-flight request bound applied to a — or to an
// adapter it wraps — unwrapping the retry / failover / num_ctx decorators the
// same way RaiseContextWindow does. 0 means no bound is in force.
//
// It exists so a diagnostic (`aegis doctor`, a startup line) can state the
// depth that is actually wired rather than re-deriving it from config and
// hoping the two agree.
func AdmissionDepth(a Adapter) int {
	for a != nil {
		if ad, ok := a.(*admissionAdapter); ok {
			return cap(ad.sem)
		}
		u, ok := a.(interface{ Unwrap() Adapter })
		if !ok {
			return 0
		}
		a = u.Unwrap()
	}
	return 0
}

// Name reports the base adapter's name — the wrapper is not a distinct backend.
func (a *admissionAdapter) Name() string { return a.base.Name() }

// Unwrap exposes the base adapter so the capability probes (RaiseContextWindow,
// RaisedContextWindow, CheckBackendHealth) reach it through this decorator,
// exactly as they do through the retry, failover and num_ctx decorators.
func (a *admissionAdapter) Unwrap() Adapter { return a.base }

// Stream implements Adapter: acquire a slot, delegate, and forward events until
// the base stream ends, releasing the slot exactly once when it does.
func (a *admissionAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	start := time.Now()
	select {
	case a.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Full: log before blocking, so a queued request is visible while it
		// waits rather than only in hindsight.
		a.logger.Debug("provider admission: queued behind in-flight request(s)",
			"provider", a.base.Name(), "depth", cap(a.sem))
		select {
		case a.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if waited := time.Since(start); waited >= slowAdmissionWait {
		a.logger.Warn("provider admission: request waited a long time for a slot; consider raising provider.max_concurrent_requests or reducing concurrent agents",
			"provider", a.base.Name(), "waited", waited.Round(time.Second), "depth", cap(a.sem))
	}

	ch, err := a.base.Stream(ctx, req)
	if err != nil {
		<-a.sem
		return nil, err
	}

	out := make(chan Event, 1)
	go func() {
		defer close(out)
		defer func() { <-a.sem }()
		for ev := range ch {
			select {
			case out <- ev:
			case <-ctx.Done():
				// The consumer has gone away (cancelled run). Drain the base
				// stream in the background so the adapter's own sender can
				// finish and close, and free the slot now rather than holding
				// it behind an abandoned reader.
				go func() {
					for range ch {
					}
				}()
				return
			}
		}
	}()
	return out, nil
}
