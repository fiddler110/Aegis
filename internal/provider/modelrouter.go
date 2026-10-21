package provider

import "context"

// WithModelRouter sends every request whose Model is smallModel to small and
// everything else to primary. It is what lets provider.small_model live on a
// different backend from provider.model — a cloud main model with titles,
// compaction and guard verdicts on a local model, or the reverse — without
// touching any of the call sites: each of them already names the small model
// on the request and calls the one daemon adapter.
//
// Unwrap reaches primary, so the capability helpers (health, context-window
// escalation, failover observation) keep answering for the main backend,
// which is what every caller of them means.
func WithModelRouter(primary Adapter, smallModel string, small Adapter) Adapter {
	if small == nil || smallModel == "" {
		return primary
	}
	return &modelRouter{primary: primary, small: small, smallModel: smallModel}
}

type modelRouter struct {
	primary, small Adapter
	smallModel     string
}

func (r *modelRouter) Name() string { return r.primary.Name() }

func (r *modelRouter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if req.Model == r.smallModel {
		return r.small.Stream(ctx, req)
	}
	return r.primary.Stream(ctx, req)
}

// Unwrap exposes the primary adapter to the capability helpers.
func (r *modelRouter) Unwrap() Adapter { return r.primary }
