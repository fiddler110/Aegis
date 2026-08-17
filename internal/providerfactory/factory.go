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
	"github.com/fiddler110/aegis/internal/ollamainfo"
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

// Option configures a Build call.
type Option func(*options)

type options struct {
	caps ollama.CapabilityStore
}

// WithModelCaps backs the built adapter with the persistent per-model
// capability store (P53.5), so a discovered quirk — today, the `think`-
// rejection latch — survives process exit instead of being re-paid with a
// failed request on every start. Only the Ollama adapter consumes it; the
// cloud adapters declare their capabilities statically.
//
// Passing a typed-nil store is safe and reduces to the unwired behavior.
func WithModelCaps(s ollama.CapabilityStore) Option {
	return func(o *options) { o.caps = s }
}

// Build constructs the adapter selected by cfg.Provider.Default, wrapped with
// retry/backoff for transient failures, and — when cfg.Provider.Fallback is
// non-empty — chained with failover to those providers on exhausted retries
// (P5.9). Pass a non-nil logger so retry/failover WARN messages go there
// instead of slog.Default() (which writes to stderr and would corrupt the
// TUI display).
func Build(cfg *config.Config, logger *slog.Logger, opts ...Option) (provider.Adapter, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var o options
	for _, fn := range opts {
		fn(&o)
	}

	primaryBase, err := buildOne(cfg.Provider.Default, cfg.Provider.APIKey, cfg.Provider.BaseURL, cfg.Provider.Headers, cfg.Provider.Think, cfg.Provider.ReasoningEffort, cfg.Provider.MaxTokens, cfg.Provider.ContextWindow, cfg.Provider.KeepAlive, cfg.Provider.ResponseHeaderTimeout(), cfg.Provider.StreamIdleTimeout(), logger, o.caps)
	if err != nil {
		return nil, err
	}
	policy := provider.DefaultRetryPolicy()
	policy.MaxRetries = cfg.Provider.MaxRetries
	primary := provider.WithRetry(admit(cfg, cfg.Provider.Default, cfg.Provider.BaseURL, primaryBase, logger), policy, logger)

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
		fbBase, err := buildOne(fb.Provider, apiKey, fb.BaseURL, cfg.Provider.Headers, cfg.Provider.Think, cfg.Provider.ReasoningEffort, cfg.Provider.MaxTokens, cfg.Provider.ContextWindow, cfg.Provider.KeepAlive, cfg.Provider.ResponseHeaderTimeout(), cfg.Provider.StreamIdleTimeout(), logger, o.caps)
		if err != nil {
			logger.Warn("provider fallback: skipping misconfigured fallback", "provider", fb.Provider, "err", err)
			continue
		}
		targets = append(targets, provider.FallbackTarget{
			Adapter: provider.WithRetry(admit(cfg, fb.Provider, fb.BaseURL, fbBase, logger), policy, logger),
			Model:   fb.Model,
		})
	}
	return provider.WithFailover(primary, cfg.Provider.Model, targets, logger), nil
}

// admit wraps base in the P59.9 admission-control decorator when the resolved
// queue depth for this backend bounds anything.
//
// Two placement decisions are load-bearing. It goes *inside* WithRetry, so a
// retry's backoff sleep does not sit on a slot a queued caller could use. And it
// is applied per built adapter rather than once around the composed chain, so
// each backend carries its own bound: a local primary that fails over to a cloud
// target must not hand the cloud its single-GPU queue depth, and the reverse
// (cloud primary, local fallback) must still bound the local one.
//
// One consequence worth stating: the daemon builds one adapter and shares it, so
// this bounds everything in that process — sessions, in-process swarm agents,
// the drive, the guard and compaction passes. It does not bound a *separate*
// process (the subprocess swarm worker builds its own adapter), which is honest
// rather than complete: a cross-process bound would need a lock outside the
// harness, and the in-process case is where concurrency is actually generated.
func admit(cfg *config.Config, name, baseURL string, base provider.Adapter, logger *slog.Logger) provider.Adapter {
	n := cfg.Provider.AdmissionLimit(name, baseURL)
	if n <= 0 {
		return base
	}
	logger.Debug("provider admission control", "provider", name, "max_in_flight", n)
	return provider.WithAdmissionControl(base, n, logger)
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
func buildOne(name, apiKey, baseURL string, headers map[string]string, think *bool, reasoningEffort string, maxTokens, contextWindow int, keepAlive string, responseHeaderTimeout, streamIdleTimeout time.Duration, logger *slog.Logger, caps ollama.CapabilityStore) (provider.Adapter, error) {
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
			anthropic.WithStreamIdleTimeout(streamIdleTimeout),
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
			ollama.WithStreamIdleTimeout(streamIdleTimeout),
			ollama.WithLogger(logger),
			// Qwen3's stock Ollama chat template renders the assistant turn's
			// content and tool calls as mutually exclusive branches, so a turn
			// that narrates *and* calls loses the call from the rendered
			// history. This is the only construction site that talks to a real
			// Ollama, so it is the one that gets to ask.
			ollama.WithTemplateProbe(ollamainfo.TemplateDropsToolCalls),
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
		if caps != nil {
			opts = append(opts, ollama.WithCapabilityStore(caps))
		}
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
			openai.WithStreamIdleTimeout(streamIdleTimeout),
		}
		// Only send the Ollama-specific `think` field when targeting an
		// OpenAI-compatible local server, not the real openai.com API.
		if baseURL != "" && think != nil {
			opts = append(opts, openai.WithThink(think))
		}
		// P61.4: the compat endpoint cannot carry num_ctx and cannot report the
		// served window, so the adapter can only reconcile max_tokens against
		// the window the daemon resolved for it (Request.NumCtx) — and only
		// when the backend actually spends one budget on prompt+completion.
		// Gated on the :11434 half of the legacy-compat rule alone, because
		// that is the only half that is *certainly* Ollama: the bare-/v1 half
		// also matches LM Studio, liteLLM and any gateway fronting a cloud
		// model, where max_tokens is a separate output allowance and clamping
		// it would truncate a legitimate long generation. Missing a proxied
		// Ollama here costs the clamp, not correctness — `aegis doctor`'s
		// generation-budget row still names the unreconciled pair.
		if IsOllamaPortBaseURL(baseURL) {
			opts = append(opts, openai.WithSharedContextWindow(true))
		}
		if reasoningEffort != "" {
			opts = append(opts, openai.WithReasoningEffort(reasoningEffort))
		}
		return openai.New(apiKey, opts...), nil

	default:
		return nil, fmt.Errorf("unsupported provider %q (supported: anthropic, openai, ollama)", name)
	}
}
