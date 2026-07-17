package providerfactory

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// suggestedContextWindow is the provider.context_window the fix suggests: the
// value verified (P34.5) to make Ollama serve at a usable window instead of its
// 4096 default, while staying well inside a 16GB-VRAM budget.
const suggestedContextWindow = 32768

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

// isCertainOllama distinguishes the two halves of IsLegacyOllamaCompat's rule:
// an :11434 base_url is Ollama, a bare /v1 base only might be.
func isCertainOllama(p config.ProviderConfig) bool {
	return strings.Contains(p.BaseURL, ":11434")
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
func LegacyOllamaCompatFix(p config.ProviderConfig) string {
	if !IsLegacyOllamaCompat(p) {
		return ""
	}
	return fmt.Sprintf(
		"set provider.default: ollama / provider.base_url: %s / provider.context_window: %d — "+
			"note the ollama adapter defaults think: false, so add provider.think: true to keep a reasoning model "+
			"(qwen3, deepseek-r1) thinking",
		ollamainfo.NativeBase(p.BaseURL), suggestedContextWindow)
}
