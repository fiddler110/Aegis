// Package providerfactory builds a provider.Adapter from configuration,
// centralizing provider selection so the daemon and CLI share one code path.
package providerfactory

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/anthropic"
	"github.com/fiddler110/aegis/internal/provider/ollama"
	"github.com/fiddler110/aegis/internal/provider/openai"
)

// defaultOllamaKeepAlive is the native-adapter keep_alive substituted when the
// user leaves provider.keep_alive unset (P35.4). Ollama's native /api/chat
// reuses its KV-cache prefix across requests automatically, but only while the
// model stays resident; left at Ollama's own 5m idle default, a multi-turn
// agentic run whose per-turn cost outlasts that window sees the model unload
// between turns and every turn reprocess the whole conversation from scratch
// (measured at 3+ min prompt passes by turn 15 of a live threat-model run).
// A bounded resident window keeps the model loaded across a run's inter-turn
// gaps so the cache survives, while still letting Ollama unload once genuinely
// idle — RAM is only held during active work, not "-1"/pinned-forever (the
// limited-RAM concern that made P33.10 keep this opt-in). Any explicit config
// value, including "-1" or "0", overrides it.
const defaultOllamaKeepAlive = "30m"

// Build constructs the adapter selected by cfg.Provider.Default, wrapped with
// retry/backoff for transient failures, and — when cfg.Provider.Fallback is
// non-empty — chained with failover to those providers on exhausted retries
// (P5.9). Pass a non-nil logger so retry/failover WARN messages go there
// instead of slog.Default() (which writes to stderr and would corrupt the
// TUI display).
func Build(cfg *config.Config, logger *slog.Logger) (provider.Adapter, error) {
	if logger == nil {
		logger = slog.Default()
	}

	primaryBase, err := buildOne(cfg.Provider.Default, cfg.Provider.APIKey, cfg.Provider.BaseURL, cfg.Provider.Headers, cfg.Provider.Think, cfg.Provider.ReasoningEffort, cfg.Provider.MaxTokens, cfg.Provider.ContextWindow, cfg.Provider.KeepAlive, cfg.Provider.ResponseHeaderTimeout(), logger)
	if err != nil {
		return nil, err
	}
	policy := provider.DefaultRetryPolicy()
	policy.MaxRetries = cfg.Provider.MaxRetries
	primary := provider.WithRetry(primaryBase, policy, logger)

	if len(cfg.Provider.Fallback) == 0 {
		return primary, nil
	}

	primaryLocal := isLocalProvider(cfg.Provider.Default)
	targets := make([]provider.FallbackTarget, 0, len(cfg.Provider.Fallback))
	for _, fb := range cfg.Provider.Fallback {
		if primaryLocal && !isLocalProvider(fb.Provider) && !cfg.Provider.AllowCloudFallback {
			logger.Warn("provider fallback: skipping cloud fallback from a local primary without allow_cloud_fallback",
				"primary", cfg.Provider.Default, "fallback_provider", fb.Provider)
			continue
		}
		apiKey := config.ProviderAPIKey(fb.Provider)
		fbBase, err := buildOne(fb.Provider, apiKey, fb.BaseURL, cfg.Provider.Headers, cfg.Provider.Think, cfg.Provider.ReasoningEffort, cfg.Provider.MaxTokens, cfg.Provider.ContextWindow, cfg.Provider.KeepAlive, cfg.Provider.ResponseHeaderTimeout(), logger)
		if err != nil {
			logger.Warn("provider fallback: skipping misconfigured fallback", "provider", fb.Provider, "err", err)
			continue
		}
		targets = append(targets, provider.FallbackTarget{
			Adapter: provider.WithRetry(fbBase, policy, logger),
			Model:   fb.Model,
		})
	}
	return provider.WithFailover(primary, cfg.Provider.Model, targets, logger), nil
}

// isLocalProvider reports whether provider name keeps data on the local
// machine. Used to gate local->cloud failover behind an explicit opt-in.
func isLocalProvider(name string) bool {
	return name == "ollama"
}

// defaultProviderHost is the hostname a cloud provider's requests go to when
// base_url is left unset. Empty for "ollama"/other OpenAI-compatible local
// servers, which have no single fixed default host to compare against.
func defaultProviderHost(name string) string {
	switch name {
	case "anthropic":
		return "api.anthropic.com"
	case "openai":
		return "api.openai.com"
	default:
		return ""
	}
}

// isRealAPIKey reports whether apiKey is an actual credential worth
// protecting, as opposed to Ollama's non-secret "ollama" placeholder used
// when no real key is configured (config.ProviderAPIKey's ollama fallback).
func isRealAPIKey(name, apiKey string) bool {
	if apiKey == "" {
		return false
	}
	return !(name == "ollama" && apiKey == "ollama")
}

// validateBaseURL is the P27.2 (FIND-03, CVSS 7.1) guard against
// provider.base_url redirecting API-key-bearing requests to an attacker
// host: base_url is a config value with no allowlist or scheme/host
// validation today, and it's reachable from project-level config (the same
// untrusted-by-default layer P27.1 gates hooks/permission/sandbox/mcp/
// notify through — base_url isn't folded into that gate since a narrower,
// independent check fully addresses this specific finding without a trust
// prompt).
//
// An empty baseURL (the common case — provider default) always passes
// through untouched. Otherwise: plaintext HTTP to a non-loopback host is
// refused outright when a real API key would be attached (the credential-
// exposure case CWE-522 describes) — a loopback HTTP endpoint (e.g. a local
// Ollama/LiteLLM proxy) is unaffected, matching how such setups already work
// today. A non-default host for a cloud provider that isn't refused still
// gets a prominent startup WARN, since many legitimate uses (corporate
// gateways, self-hosted OpenAI-compatible proxies) exist and a hard refusal
// there would be a real regression.
func validateBaseURL(name, apiKey, baseURL string, logger *slog.Logger) error {
	if baseURL == "" {
		return nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("provider.base_url %q: %w", baseURL, err)
	}
	loopback := config.IsLoopbackBaseURL(baseURL)

	if u.Scheme == "http" && !loopback && isRealAPIKey(name, apiKey) {
		return fmt.Errorf(
			"provider.base_url %q sends the %s API key over plaintext HTTP to a non-loopback host; "+
				"refusing to attach the key — use https, or point base_url at a loopback address if this "+
				"is a trusted local/LAN endpoint with no real credential involved",
			baseURL, name,
		)
	}

	if defaultHost := defaultProviderHost(name); defaultHost != "" && !loopback && !strings.EqualFold(u.Hostname(), defaultHost) {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("provider.base_url overrides the default API host; every request (including the API key) goes to this host instead",
			"provider", name, "base_url", baseURL, "default_host", defaultHost)
	}

	return nil
}

// buildOne constructs a single unwrapped adapter for provider name. Shared by
// the primary and every fallback target so their construction rules
// (base URL defaults, thinking/reasoning options, key requirements) stay
// identical.
func buildOne(name, apiKey, baseURL string, headers map[string]string, think *bool, reasoningEffort string, maxTokens, contextWindow int, keepAlive string, responseHeaderTimeout time.Duration, logger *slog.Logger) (provider.Adapter, error) {
	if err := validateBaseURL(name, apiKey, baseURL, logger); err != nil {
		return nil, err
	}
	switch name {
	case "anthropic":
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
		}
		opts := []anthropic.Option{
			anthropic.WithBaseURL(baseURL),
			anthropic.WithHeaders(headers),
			anthropic.WithResponseHeaderTimeout(responseHeaderTimeout),
		}
		// Enable extended thinking when explicitly requested, budgeting half of
		// max_tokens for reasoning (clamped to the API's 1024 minimum).
		if think != nil && *think {
			budget := maxTokens / 2
			if budget < 1024 {
				budget = 1024
			}
			opts = append(opts, anthropic.WithThinking(budget))
		}
		return anthropic.New(apiKey, opts...), nil

	case "ollama":
		// Native /api/chat (P33.9), not the OpenAI-compat endpoint: it needs
		// no API key, and unlocks per-request num_ctx, keep_alive, and real
		// token/load telemetry the compat path can't offer. A lingering "/v1"
		// suffix from an older config is stripped by ollama.WithBaseURL.
		// Thinking is disabled by default to prevent reasoning preambles in
		// non-thinking tasks.
		falseVal := false
		if think == nil {
			think = &falseVal // suppress Ollama thinking unless explicitly enabled
		}
		opts := []ollama.Option{
			ollama.WithBaseURL(baseURL),
			ollama.WithHeaders(headers),
			ollama.WithThink(think),
			ollama.WithResponseHeaderTimeout(responseHeaderTimeout),
			ollama.WithLogger(logger),
		}
		if contextWindow > 0 {
			opts = append(opts, ollama.WithNumCtx(contextWindow))
		}
		// keep_alive keeps the model resident so Ollama's automatic prefix
		// KV-cache reuse survives the gaps between turns instead of the whole
		// conversation reprocessing each turn (P35.4). An unset config value
		// substitutes a bounded resident default (defaultOllamaKeepAlive), not
		// "-1"; an explicit value — including "-1" to pin forever or "0" to
		// unload immediately (P33.10) — always wins.
		if keepAlive == "" {
			keepAlive = defaultOllamaKeepAlive
		}
		opts = append(opts, ollama.WithKeepAlive(keepAlive))
		return ollama.New(opts...), nil

	case "openai":
		// Require an API key only when using the real OpenAI endpoint. Local
		// servers (LM Studio, liteLLM) have no auth requirement when base_url
		// is configured.
		if apiKey == "" && baseURL == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is not set")
		}
		opts := []openai.Option{
			openai.WithBaseURL(baseURL),
			openai.WithHeaders(headers),
			openai.WithResponseHeaderTimeout(responseHeaderTimeout),
		}
		// Only send the Ollama-specific `think` field when targeting an
		// OpenAI-compatible local server, not the real openai.com API.
		if baseURL != "" && think != nil {
			opts = append(opts, openai.WithThink(think))
		}
		if reasoningEffort != "" {
			opts = append(opts, openai.WithReasoningEffort(reasoningEffort))
		}
		return openai.New(apiKey, opts...), nil

	default:
		return nil, fmt.Errorf("unsupported provider %q (supported: anthropic, openai, ollama)", name)
	}
}
