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

import (
	"fmt"
	"maps"
)

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
	// PromptSuffix (P74.21) is appended to the system prompt on every request
	// this model answers, for a quirk that needs a line of instruction rather
	// than a repair — e.g. a model trained on a different tool-call vocabulary
	// that benefits from being told so explicitly. Applied by
	// provider.WithHarness, which resolves it fresh per request from
	// Request.Model, so it never leaks onto a request another model answers.
	//
	// Unlike the two repair bools, nothing before this item made the choice
	// "local vs default" for this field — NewResolver leaves it empty for
	// every model until an Override sets it. A configured suffix is validated
	// against sysprompt's local-profile budget at provider-build time (see
	// internal/providerfactory), not here: this package has no business
	// estimating tokens, and the same config-file-is-not-a-call-site reasoning
	// that motivates RequiredExposedTools applies to the budget too.
	PromptSuffix string
	// ToolDescriptionOverrides (P74.21) renames a tool's Description as sent
	// to this model, keyed by tool name. Applied by provider.WithHarness to
	// Request.Tools right before the request goes out — no registry surgery,
	// so it costs nothing for a model with no override and never touches what
	// tool_search or another model sees. A name with no matching tool in the
	// request is silently ignored: the harness describes what to change *if*
	// the tool is present, not a claim that it always will be.
	ToolDescriptionOverrides map[string]string
	// DeferredTools (P74.21) names tools this model should never receive in
	// Request.Tools; provider.WithHarness strips them from the request and
	// folds their name+description into a short note appended to the system
	// prompt instead, so the model still knows they exist without paying for
	// their schema on every turn.
	//
	// This is a strictly weaker trade than builtin.Options.LocalProfile's
	// registration-time deferral: that mechanism removes a tool from what is
	// *exposed* while leaving it registered, so tool_search can load it back
	// mid-conversation. This field operates only at the request layer — the
	// registry never learns the tool is "deferred" for this model — so there
	// is no recovery path inside a session: a name listed here is unavailable
	// to this model for the life of the session, full stop. Reach for
	// LocalProfile's mechanism when a tool should be loadable on demand;
	// reach for this only when a model should never see it at all.
	//
	// RequiredExposedTools may never appear here; ValidateOverrides enforces
	// it at config-load time because a config file is not a call site
	// TestEveryRegisterCallSiteDecidesTheLocalProfile's build-time scan can see.
	DeferredTools []string
}

// RequiredExposedTools names tools no Harness.DeferredTools may ever include.
//
// tool_search is the one entry today: hiding it from a model would be
// silently self-defeating — a session that also relies on
// builtin.Options.LocalProfile's registration-time deferral needs it to
// reach anything LocalProfile deferred, and no Harness field can substitute
// for it once it's gone. ValidateOverrides rejects it before a session ever
// starts rather than discovering it turn by turn.
var RequiredExposedTools = []string{"tool_search"}

// ValidateOverrides rejects a configured per-model override that would strand
// a RequiredExposedTools entry in DeferredTools. Called once at config-load
// time (internal/providerfactory), because — unlike the fixed call sites
// TestEveryRegisterCallSiteDecidesTheLocalProfile can scan at build time — a
// user-authored config.Provider.ModelHarness entry is only checkable at
// runtime.
func ValidateOverrides(overrides map[string]Override) error {
	for model, o := range overrides {
		for _, deferred := range o.DeferredTools {
			for _, required := range RequiredExposedTools {
				if deferred == required {
					return fmt.Errorf("model_harness[%q]: %q may not be deferred: it is the only mechanism for loading a deferred tool", model, required)
				}
			}
		}
	}
	return nil
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
	// PromptSuffix sets Harness.PromptSuffix for this model. Unlike the two
	// bool fields there is no provider-level default to inherit — every model
	// starts at "" — so a nil pointer here just means "no suffix", not "use
	// the default one".
	PromptSuffix *string `koanf:"prompt_suffix" json:"prompt_suffix,omitempty"`
	// ToolDescriptionOverrides sets Harness.ToolDescriptionOverrides for this
	// model. Entries are merged onto the (always empty) default per key, so
	// naming one tool here does not require repeating every other override.
	ToolDescriptionOverrides map[string]string `koanf:"tool_description_overrides" json:"tool_description_overrides,omitempty"`
	// DeferredTools sets Harness.DeferredTools for this model. Entries are
	// appended to the (always empty) default; see RequiredExposedTools for
	// the one name this may never contain.
	DeferredTools []string `koanf:"deferred_tools" json:"deferred_tools,omitempty"`
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
			if o.PromptSuffix != nil {
				h.PromptSuffix = *o.PromptSuffix
			}
			if len(o.ToolDescriptionOverrides) > 0 {
				h.ToolDescriptionOverrides = make(map[string]string, len(o.ToolDescriptionOverrides))
				maps.Copy(h.ToolDescriptionOverrides, o.ToolDescriptionOverrides)
			}
			if len(o.DeferredTools) > 0 {
				h.DeferredTools = append([]string(nil), o.DeferredTools...)
			}
		}
		return h
	}
}
