package provider

import (
	"context"

	"github.com/fiddler110/aegis/internal/profile"
)

// harnessAdapter routes each request to whichever combination of repair
// decorators its model's resolved profile.Harness engages (P74.17), decided
// from Request.Model rather than fixed at construction time — see the
// package doc on internal/profile for why that has to be per-request.
//
// The four possible chains are built once, at wrap time, so a request that
// needs none of them (every cloud call, and any local call for a model with
// no repair behavior declared) pays only the resolve call and a type switch,
// never a decorator it isn't using.
type harnessAdapter struct {
	base             Adapter
	salvage          Adapter // ProseToolCallSalvage only
	repair           Adapter // ArgumentShapeRepair only
	salvageAndRepair Adapter // both
	resolve          profile.Resolver
}

// WithHarness wraps base so the P74.17 per-model repair decorators
// (prose-tool-call salvage, argument-shape repair) engage exactly for the
// models resolve says should have them, decided fresh for every request from
// Request.Model. Returns base unchanged when base or resolve is nil.
func WithHarness(base Adapter, resolve profile.Resolver) Adapter {
	if base == nil || resolve == nil {
		return base
	}
	return &harnessAdapter{
		base:             base,
		salvage:          WithProseToolCallSalvage(base),
		repair:           WithArgumentShapeRepair(base),
		salvageAndRepair: WithArgumentShapeRepair(WithProseToolCallSalvage(base)),
		resolve:          resolve,
	}
}

func (a *harnessAdapter) Name() string    { return a.base.Name() }
func (a *harnessAdapter) Unwrap() Adapter { return a.base }

func (a *harnessAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	h := a.resolve(req.Model)
	switch {
	case h.ProseToolCallSalvage && h.ArgumentShapeRepair:
		return a.salvageAndRepair.Stream(ctx, req)
	case h.ProseToolCallSalvage:
		return a.salvage.Stream(ctx, req)
	case h.ArgumentShapeRepair:
		return a.repair.Stream(ctx, req)
	default:
		return a.base.Stream(ctx, req)
	}
}
