package provider

import "context"

// numCtxAdapter stamps a serving context window (Request.NumCtx) onto every
// request that doesn't already carry one, then delegates to the base adapter.
//
// It exists because the daemon builds one adapter at startup but resolves the
// model per turn (P52.4): the run's model — and therefore the window that model
// should be served with — is only known when the engine for that turn is
// assembled, and the engine itself has no reason to know about Ollama's
// num_ctx. Wrapping the shared adapter for the duration of one run keeps that
// knowledge at the layer that has it (the server, which resolved the window per
// model in P52.1) without making the window mutable adapter state again.
type numCtxAdapter struct {
	base   Adapter
	numCtx int
}

// WithNumCtx returns base wrapped so every request it streams carries n as its
// serving context window. n <= 0 (or a nil base) returns base unchanged, so a
// caller with nothing to say about the window adds no layer at all.
func WithNumCtx(base Adapter, n int) Adapter {
	if base == nil || n <= 0 {
		return base
	}
	return &numCtxAdapter{base: base, numCtx: n}
}

// Name reports the base adapter's name — the wrapper is not a distinct backend.
func (a *numCtxAdapter) Name() string { return a.base.Name() }

// Unwrap exposes the base adapter so the capability probes (RaiseContextWindow,
// CheckBackendHealth) reach it through this decorator, exactly as they do
// through the retry and failover decorators.
func (a *numCtxAdapter) Unwrap() Adapter { return a.base }

// Stream implements Adapter, filling in NumCtx when the caller left it unset.
// An explicit per-request value always wins: this decorator supplies a default
// for a run, it does not override a caller that knows better.
func (a *numCtxAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if req.NumCtx <= 0 {
		req.NumCtx = a.numCtx
	}
	return a.base.Stream(ctx, req)
}
