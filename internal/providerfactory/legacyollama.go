package providerfactory

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// IsLegacyOllamaCompat reports whether cfg drives an Ollama server through the
// OpenAI-compat adapter (P34.5) — the shape every pre-P33.9 config has, since
// `openai` + a loopback base_url was the only way to reach Ollama before the
// native adapter existed.
//
// It matters because buildOne only wires ollama.WithNumCtx/WithKeepAlive and
// the native load/token telemetry on the "ollama" branch: the compat path
// cannot send num_ctx (so Ollama serves every request at its 4096 default, no
// matter what context_window says) and cannot see load_duration (so P33.9's
// cold-load notice never fires). Nothing about that is visible from the outside
// — hence the doctor check and the startup WARN.
//
// The rule is the roadmap's: provider.default == "openai" and a base_url that
// is neither empty nor an OpenAI endpoint — an :11434 host, or any /v1 base
// that isn't api.openai.com. The :11434 half is unambiguous; the /v1 half also
// matches non-Ollama OpenAI-compatible servers (LM Studio, liteLLM), which the
// openai branch supports deliberately and which have no native /api/chat to
// switch to. Rather than narrow the rule and miss an Ollama server on a
// non-default port behind a proxy, the message for that case is worded as a
// conditional (see LegacyOllamaCompatDetail) — a false positive there costs a
// line of advice the reader can dismiss, not a broken config.
func IsLegacyOllamaCompat(p config.ProviderConfig) bool {
	if !strings.EqualFold(p.Default, "openai") || p.BaseURL == "" {
		return false
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Hostname(), defaultProviderHost("openai")) {
		return false
	}
	if strings.Contains(p.BaseURL, ":11434") {
		return true
	}
	return strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1")
}

// IsOllamaPortBaseURL distinguishes the two halves of IsLegacyOllamaCompat's
// rule: an :11434 base_url is Ollama, a bare /v1 base only might be. It is the
// one predicate in this package that is allowed to mean "certainly Ollama",
// which is why P61.4's max_tokens clamp on the compat adapter is gated on it
// and not on IsLegacyOllamaCompat — the latter deliberately over-matches so its
// *advice* reaches a proxied server, and advice can be dismissed where a clamp
// cannot.
func IsOllamaPortBaseURL(baseURL string) bool {
	return strings.Contains(baseURL, ":11434")
}

// isCertainOllama is IsOllamaPortBaseURL for a whole provider config.
func isCertainOllama(p config.ProviderConfig) bool {
	return IsOllamaPortBaseURL(p.BaseURL)
}

// CertainlyOllama reports whether p names a backend positively identified as
// Ollama, on either adapter: the native one (provider.default: ollama), or the
// OpenAI-compat one pointed at an :11434 base_url.
//
// It is the same standard IsOllamaPortBaseURL sets and for the same reason — a
// caller uses this where being wrong is not merely bad advice. P66.14 (LLM-03)
// is the second such caller: the engine's token-estimate calibration was gated
// on `PromptEvalDurationMS > 0`, a field only the native adapter sets, so every
// user following docs/providers.md's own recommendation (`provider.default:
// openai` with a `:11434/v1` base_url) ran the whole session on the uncorrected
// 20-33% undercount. What that gate was reaching for was "this backend reports
// the full prompt every turn, and truncates in silence when it is exceeded",
// which is a property of the *server*, not of one adapter's telemetry.
//
// Deliberately narrower than config.LocalBackend, which answers "one GPU, not a
// fleet" and includes LM Studio, llama.cpp and any loopback proxy. Those report
// prompt tokens on their own terms; missing one here costs a correction, while
// wrongly including a cloud gateway would calibrate against a cache-adjusted
// number.
func CertainlyOllama(p config.ProviderConfig) bool {
	if strings.EqualFold(strings.TrimSpace(p.Default), "ollama") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(p.Default), "openai") && IsOllamaPortBaseURL(p.BaseURL)
}

// LegacyOllamaCompatDetail describes what the config is costing, or "" when
// IsLegacyOllamaCompat doesn't fire. Shared by `aegis doctor` and the daemon's
// startup warning so both say the same thing.
func LegacyOllamaCompatDetail(p config.ProviderConfig) string {
	if !IsLegacyOllamaCompat(p) {
		return ""
	}
	if isCertainOllama(p) {
		return fmt.Sprintf(
			"provider.default: openai with base_url %q drives Ollama through the legacy OpenAI-compat adapter: "+
				"context_window is never sent (Ollama serves at its 4096 default) and the cold-load notice never fires",
			p.BaseURL)
	}
	return fmt.Sprintf(
		"provider.default: openai with a non-OpenAI base_url %q — if that is an Ollama server it is on the legacy "+
			"OpenAI-compat adapter, which never sends context_window (Ollama serves at its 4096 default) and cannot "+
			"report cold loads",
		p.BaseURL)
}

// LegacyOllamaCompatFix is the exact config change, spelled out rather than
// described, plus the one real behavior difference so the fix isn't a silent
// downgrade: buildOne's ollama branch defaults think to false, while the compat
// path passes provider.think through untouched (and omits it entirely when nil),
// leaving the model's own default alone — so a qwen3-style reasoning model stops
// thinking after the switch unless think: true is set explicitly.
//
// modelMax is the model's detected training-context maximum
// (ollamainfo.Result.ModelMax), or 0 when it could not be detected. The
// recommended context_window is calibrated against it (P35.3): a fixed
// 16GB-VRAM-safe number is overrun by a skill-driven session before it writes
// any output, and the model's real max is far larger. 0 falls back to the
// baseline recommendation unchanged.
func LegacyOllamaCompatFix(p config.ProviderConfig, modelMax int) string {
	if !IsLegacyOllamaCompat(p) {
		return ""
	}
	win := ollamainfo.RecommendContextWindow(modelMax)
	sizing := "skill-driven/long-running sessions can build a >40k-token prompt before producing " +
		"output, so raise it toward the model's max if a run fails on context (P35.3)"
	if modelMax > 0 {
		sizing = fmt.Sprintf("sized for skill-driven headroom from this model's %d-token max — "+
			"lower it to trade context for GPU memory (P35.3)", modelMax)
	}
	return fmt.Sprintf(
		"set provider.default: ollama / provider.base_url: %s / provider.context_window: %d (%s) — "+
			"note the ollama adapter defaults think: false, so add provider.think: true to keep a reasoning model "+
			"(qwen3, deepseek-r1) thinking",
		ollamainfo.NativeBase(p.BaseURL), win, sizing)
}

// LegacyOllamaModelfileRecipe returns the exact commands to bake a num_ctx
// derivative of model that serves want tokens — the fallback for a run that
// must stay on the OpenAI-compat (/v1) adapter, which cannot send num_ctx, so
// Ollama serves the model's modelfile default (16384 for stock
// qwen3.6:35b-a3b-fast) and a skill-driven prompt overflows it
// ("request (34774 tokens) exceeds the available context size (16384)", P39.9).
// The switch-to-native path (LegacyOllamaCompatFix) is the primary fix; this is
// for when the native adapter can't be used. Returns "" for a blank model or a
// non-positive window.
func LegacyOllamaModelfileRecipe(model string, want int) string {
	if model == "" || want <= 0 {
		return ""
	}
	derived := deriveCtxModelName(model, want)
	return fmt.Sprintf(
		"if you must stay on the /v1 compat adapter, bake a num_ctx derivative: "+
			"`printf 'FROM %s\\nPARAMETER num_ctx %d\\n' | ollama create %s -f -`, then set provider.model: %s",
		model, want, derived, derived)
}

// deriveCtxModelName builds a valid Ollama model name for a num_ctx derivative
// of base serving want tokens (e.g. qwen3.6:35b-a3b-fast → qwen3.6-35b-a3b-fast-ctx32768).
// The ':' tag separator is replaced with '-' so the derived name is a single
// bare tag, matching the reference wrapper's convention.
func deriveCtxModelName(base string, want int) string {
	return fmt.Sprintf("%s-ctx%d", strings.ReplaceAll(base, ":", "-"), want)
}
