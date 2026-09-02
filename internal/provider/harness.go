package provider

import (
	"context"
	"fmt"
	"strings"

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
	req = applyPromptAndToolHarness(req, h)
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

// applyPromptAndToolHarness applies the P74.21 half of Harness — PromptSuffix,
// ToolDescriptionOverrides and DeferredTools — that has nothing to do with
// response repair and so needs no separate decorator chain: it only ever
// rewrites the outgoing Request, never the reply.
//
// req.Tools is never mutated in place; a caller may reuse the same backing
// slice across requests for other models, and this only touches the copy
// built for req.Model.
func applyPromptAndToolHarness(req Request, h profile.Harness) Request {
	if h.PromptSuffix == "" && len(h.ToolDescriptionOverrides) == 0 && len(h.DeferredTools) == 0 {
		return req
	}

	if len(h.ToolDescriptionOverrides) > 0 || len(h.DeferredTools) > 0 {
		kept := make([]ToolSchema, 0, len(req.Tools))
		var deferredNote strings.Builder
		for _, ts := range req.Tools {
			if isDeferredForHarness(ts.Name, h.DeferredTools) {
				if deferredNote.Len() == 0 {
					deferredNote.WriteString("The following tools exist in this workspace but are not available to you in this session:\n")
				}
				fmt.Fprintf(&deferredNote, "- %s: %s\n", ts.Name, ts.Description)
				continue
			}
			if override, ok := h.ToolDescriptionOverrides[ts.Name]; ok {
				ts.Description = override
			}
			kept = append(kept, ts)
		}
		req.Tools = kept
		if deferredNote.Len() > 0 {
			req.System = appendPromptText(req.System, strings.TrimRight(deferredNote.String(), "\n"))
		}
	}

	if h.PromptSuffix != "" {
		req.System = appendPromptText(req.System, h.PromptSuffix)
	}
	return req
}

func isDeferredForHarness(name string, deferred []string) bool {
	for _, d := range deferred {
		if d == name {
			return true
		}
	}
	return false
}

func appendPromptText(system, addition string) string {
	if system == "" {
		return addition
	}
	return strings.TrimRight(system, "\n") + "\n\n" + addition
}
