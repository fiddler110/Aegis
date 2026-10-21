package anthropic

import (
	"strconv"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
)

// The Messages API stopped being one request shape across models. From the
// 4.6 generation on, extended thinking is `{type:"adaptive"}` with depth set
// by output_config.effort, and `{type:"enabled", budget_tokens}` is rejected
// with a 400 on everything from 4.7 up. Several current models think whether
// or not the request asks (and some refuse to be told not to), and most no
// longer accept a non-default temperature. A request built the 2025 way —
// which is what this adapter sent until it learned these traits — fails on
// every current model the moment `think: true` is set.
//
// The traits below are keyed on the model id the request names, because the
// model is per request (P52.4): one adapter serves the primary model, a
// persona-pinned model and the small model, and they need not share a
// generation.

// thinkingStyle is how a model is asked to think.
type thinkingStyle int

const (
	// thinkingBudget is the pre-4.6 shape: {type:"enabled", budget_tokens:N}.
	thinkingBudget thinkingStyle = iota
	// thinkingAdaptive is {type:"adaptive"}, with depth set by effort.
	thinkingAdaptive
)

// offSwitch is how (and whether) a model's thinking can be turned off.
type offSwitch int

const (
	offDisabled     offSwitch = iota // {type:"disabled"} is accepted
	offBetweenTools                  // only {type:"between_tools"} (Sonnet 5.5), at effort <= high
	offNone                          // thinking cannot be turned off; lower effort instead
)

// modelTraits is what the request builder needs to know about one model.
type modelTraits struct {
	known bool // false: an id this table cannot parse (a gateway alias, say)

	thinking thinkingStyle
	// thinksByDefault: the model runs adaptive thinking when the request
	// carries no thinking parameter at all. On these models "omit it" does not
	// mean "off" — which matters for the guard's small max_tokens (EXEC-1).
	thinksByDefault bool
	off             offSwitch
	// sampling: a non-default temperature is accepted. False on the 4.7+
	// generation, where sending one is a 400.
	sampling bool
	// displayOmitted: thinking text streams back empty unless the request
	// asks for display:"summarized" (the default from 4.7 on).
	displayOmitted bool
	// effort lists the output_config.effort levels the model accepts, lowest
	// first; nil means the parameter is not accepted at all.
	effort []string

	contextWindow int // input tokens
}

var (
	effortAll   = []string{"low", "medium", "high", "xhigh", "max"}
	effort46    = []string{"low", "medium", "high", "max"} // xhigh arrived with Opus 4.7
	effort45    = []string{"low", "medium", "high"}
	legacyTrait = modelTraits{thinking: thinkingBudget, off: offDisabled, sampling: true, contextWindow: 200_000}
)

// traitsFor classifies a model id. An id it cannot parse gets the legacy
// shape with known=false: that is exactly what the adapter sent before this
// table existed, so a proxy alias behaves as it always did.
func traitsFor(model string) modelTraits {
	family, major, minor, ok := parseModelID(model)
	if !ok {
		return legacyTrait
	}
	t := modelTraits{known: true, contextWindow: 1_000_000}
	ver := major*10 + minor
	switch family {
	case "fable", "mythos":
		t.thinking, t.thinksByDefault, t.off = thinkingAdaptive, true, offNone
		t.displayOmitted, t.effort = true, effortAll
	case "opus":
		switch {
		case ver >= 55:
			t.thinking, t.thinksByDefault, t.off = thinkingAdaptive, true, offNone
			t.displayOmitted, t.effort = true, effortAll
		case ver >= 50:
			t.thinking, t.thinksByDefault, t.off = thinkingAdaptive, true, offDisabled
			t.displayOmitted, t.effort = true, effortAll
		case ver >= 47:
			t.thinking, t.off = thinkingAdaptive, offDisabled
			t.displayOmitted, t.effort = true, effortAll
		case ver == 46:
			t.thinking, t.off, t.sampling, t.effort = thinkingAdaptive, offDisabled, true, effort46
		case ver == 45:
			t = legacyTrait
			t.known, t.effort = true, effort45
		default:
			t = legacyTrait
			t.known = true
		}
	case "sonnet":
		switch {
		case ver >= 55:
			t.thinking, t.thinksByDefault, t.off = thinkingAdaptive, true, offBetweenTools
			t.displayOmitted, t.effort = true, effortAll
		case ver >= 50:
			t.thinking, t.thinksByDefault, t.off = thinkingAdaptive, true, offDisabled
			t.displayOmitted, t.effort = true, effortAll
		case ver == 46:
			t.thinking, t.off, t.sampling, t.effort = thinkingAdaptive, offDisabled, true, effort46
		default:
			t = legacyTrait
			t.known = true
		}
	case "haiku":
		if ver >= 55 {
			t.thinking, t.thinksByDefault, t.off = thinkingAdaptive, true, offDisabled
			t.displayOmitted, t.effort = true, effortAll
		} else {
			t = legacyTrait
			t.known = true
		}
	default:
		return legacyTrait
	}
	return t
}

// parseModelID reads "claude-<family>-<major>[-<minor>][-<yyyymmdd>]", plus
// the older "claude-<major>[-<minor>]-<family>" form (claude-3-5-sonnet). A
// vendor prefix (OpenRouter's "anthropic/", Bedrock's "anthropic.") is
// stripped first. Any other shape returns ok=false.
func parseModelID(model string) (family string, major, minor int, ok bool) {
	id := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndexAny(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	id = strings.TrimPrefix(id, "anthropic.")
	if i := strings.IndexByte(id, '@'); i >= 0 { // Vertex snapshot separator
		id = id[:i]
	}
	rest, found := strings.CutPrefix(id, "claude-")
	if !found {
		return "", 0, 0, false
	}
	parts := strings.Split(rest, "-")
	var nums []int
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err == nil && len(p) < 8 { // a date suffix is not a version
			nums = append(nums, n)
		} else if err != nil && family == "" {
			family = p
		}
	}
	if family == "" || len(nums) == 0 {
		return "", 0, 0, false
	}
	major = nums[0]
	if len(nums) > 1 {
		minor = nums[1]
	}
	return family, major, minor, true
}

// clampEffort maps a configured effort onto one the model accepts: an exact
// match is kept, an unsupported level falls to the highest accepted level not
// above it (xhigh → high on Opus 4.6), and a model that takes no effort at all
// gets "" (omit). An unrecognized level is dropped rather than sent: the API
// would 400 on it, and the config validator is the place to complain.
func clampEffort(t modelTraits, want string) string {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" || len(t.effort) == 0 {
		return ""
	}
	rank := map[string]int{}
	for i, l := range effortAll {
		rank[l] = i
	}
	w, ok := rank[want]
	if !ok {
		return ""
	}
	best := ""
	for _, l := range t.effort {
		if rank[l] <= w {
			best = l
		}
	}
	return best
}

// effortAtMostHigh reports whether e permits the off switches that the API
// refuses above "high" (disabled on Opus 5 / Haiku 5.5, between_tools on
// Sonnet 5.5). An unset effort counts as permitted only on models whose
// default is high or below — every current one.
func effortAtMostHigh(e string) bool {
	return e != "xhigh" && e != "max"
}

// ContextWindowFor reports the input context window of a Claude model id, or
// 0 when the id is not one this package recognizes. Callers that size
// compaction or a usage meter use it in place of a name-fragment guess.
func ContextWindowFor(model string) int {
	t := traitsFor(model)
	if !t.known {
		return 0
	}
	return t.contextWindow
}

// reasoningFor builds the thinking, output_config and temperature fields of
// one request from the adapter's configuration and the request model's traits.
//
// suppress is the caller's "this call must not spend its budget thinking"
// (provider.SuppressesExtendedThinking for the guard, Request.SuppressThinking
// for compaction's retry and the empty-answer nudge). On a model that thinks
// by default it is the only thing standing between a 1,024-token guard call
// and an empty reply — EXEC-1, on a cloud backend this time. Where the model
// has an off switch it is used; where it has none, effort "low" is.
func (a *Adapter) reasoningFor(t modelTraits, req provider.Request, suppress bool) (*wireThinking, *wireOutputConfig, *float64) {
	temperature := req.Temperature
	if !t.sampling {
		temperature = nil
	}
	effort := clampEffort(t, a.effort)
	var thinking *wireThinking

	if t.thinking == thinkingBudget {
		if a.thinking != nil && !suppress {
			budget := a.thinking.BudgetTokens
			if budget >= req.MaxTokens {
				budget = req.MaxTokens - 1
			}
			if budget >= 1024 {
				// The API only permits the default temperature while thinking.
				thinking = &wireThinking{Type: "enabled", BudgetTokens: budget}
				temperature = nil
			}
		}
		return thinking, outputConfig(effort), temperature
	}

	switch {
	case suppress:
		if len(t.effort) > 0 {
			effort = "low"
		}
		thinking = offBlock(t.off)
	case a.thinking != nil:
		thinking = &wireThinking{Type: "adaptive"}
		if t.displayOmitted {
			// Without it the TUI shows a long silent pause: the thinking
			// blocks arrive, but with empty text.
			thinking.Display = "summarized"
		}
		temperature = nil
	case a.thinkingOff && effortAtMostHigh(effort):
		thinking = offBlock(t.off)
	}
	if t.thinksByDefault && (thinking == nil || thinking.Type == "adaptive") {
		temperature = nil
	}
	return thinking, outputConfig(effort), temperature
}

// offBlock is the thinking parameter that turns thinking off, or nil when the
// model has no off switch.
func offBlock(o offSwitch) *wireThinking {
	switch o {
	case offDisabled:
		return &wireThinking{Type: "disabled"}
	case offBetweenTools:
		return &wireThinking{Type: "between_tools"}
	}
	return nil
}

func outputConfig(effort string) *wireOutputConfig {
	if effort == "" {
		return nil
	}
	return &wireOutputConfig{Effort: effort}
}
