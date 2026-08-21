// Package profile resolves the per-model harness (P74.17) that decides which
// response-repair behaviors engage for a given model.
//
// Before this package existed, every local-model repair Aegis had — P74.8's
// prose-tool-call salvage, and the argument-shape repair this item adds as
// its own first cargo — was gated on one blanket boolean,
// config.Provider.LocalPromptProfile(). That boolean answers "is the active
// provider a local one", not "does this specific model need this specific
// repair": qwen3:14b and a barely-conformant 1.5B model both got the exact
// same treatment, and a cloud model routed as a debate seat's fallback got
// none even where it might have helped.
//
// A Harness is resolved per Request.Model, not once at adapter-construction
// time, for the same reason internal/provider's NumCtx field is per-request
// (P52.4): one daemon-wide adapter serves the primary model, a persona-pinned
// model and whatever internal/config.Provider.SmallModel task routing picks,
// and baking a decision in at Build() time would apply it to all of them
// regardless of which one is actually answering.
package profile

// Harness is what Aegis does differently when talking to one model. The
// zero value engages nothing — a cloud model, or a local one with no
// declared quirks, gets exactly today's unmodified behavior.
type Harness struct {
	// ProseToolCallSalvage engages provider.WithProseToolCallSalvage: a turn
	// that requested tools but answered in prose is scanned for a tool call
	// written as text (P74.8).
	ProseToolCallSalvage bool
	// ArgumentShapeRepair engages provider.WithArgumentShapeRepair: a
	// structured tool call whose arguments are shaped wrong — double-encoded
	// JSON, wrapped under a redundant key, a bare scalar where an object was
	// expected — is repaired before it reaches tool dispatch (P74.9's second
	// half).
	ArgumentShapeRepair bool
}

// Resolver resolves the Harness that applies to model. It is a function
// rather than a lookup method so callers can close over whatever inputs
// they have (config, a modelcaps store) without this package needing to
// know about either.
type Resolver func(model string) Harness

// Override is a per-model correction layered additively on top of the
// provider-level default (config.Provider.model_harness). Every field is a
// pointer: unset means "inherit the default", not "declare false" — the same
// convention internal/modelcaps.Declared uses, and for the same reason: a
// user who wants to turn argument-shape repair off for one model must not
// also have to say what the default was.
type Override struct {
	ProseToolCallSalvage *bool `koanf:"prose_tool_call_salvage" json:"prose_tool_call_salvage,omitempty"`
	ArgumentShapeRepair  *bool `koanf:"argument_shape_repair" json:"argument_shape_repair,omitempty"`
}

// NewResolver builds a Resolver from the provider-level local/cloud default
// and per-model overrides declared in config.
//
// local mirrors config.Provider.LocalPromptProfile() — the same signal
// P74.8 and P74.9 gated on directly before this package existed — kept as
// the base every model starts from. That preserves the existing behavior for
// every model with no override: nothing regresses the day this ships. A
// model named in overrides layers its corrections on top, additively: naming
// a model only to flip one field leaves the other at the provider default,
// it does not reset the whole harness to zero value.
func NewResolver(local bool, overrides map[string]Override) Resolver {
	base := Harness{}
	if local {
		base = Harness{ProseToolCallSalvage: true, ArgumentShapeRepair: true}
	}
	return func(model string) Harness {
		h := base
		if o, ok := overrides[model]; ok {
			if o.ProseToolCallSalvage != nil {
				h.ProseToolCallSalvage = *o.ProseToolCallSalvage
			}
			if o.ArgumentShapeRepair != nil {
				h.ArgumentShapeRepair = *o.ArgumentShapeRepair
			}
		}
		return h
	}
}
