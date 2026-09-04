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
	"github.com/fiddler110/aegis/internal/profile"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/anthropic"
	"github.com/fiddler110/aegis/internal/provider/ollama"
	"github.com/fiddler110/aegis/internal/provider/openai"
	"github.com/fiddler110/aegis/internal/sysprompt"
	"github.com/fiddler110/aegis/internal/tokenest"
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

	if err := validateModelHarness(cfg); err != nil {
		return nil, err
	}

	primaryBase, err := buildOne(buildOneConfig{
		name:                  cfg.Provider.Default,
		apiKey:                cfg.Provider.APIKey,
		baseURL:               cfg.Provider.BaseURL,
		headers:               cfg.Provider.HeadersFor(cfg.Provider.Default, cfg.Provider.BaseURL),
		think:                 cfg.Provider.Think,
		reasoningEffort:       cfg.Provider.ReasoningEffort,
		maxTokens:             cfg.Provider.MaxTokens,
		contextWindow:         cfg.Provider.ContextWindow,
		keepAlive:             cfg.Provider.KeepAlive,
		responseHeaderTimeout: cfg.Provider.ResponseHeaderTimeout(),
		streamIdleTimeout:     cfg.Provider.StreamIdleTimeout(),
		logger:                logger,
		caps:                  o.caps,
	})
	if err != nil {
		return nil, err
	}
	primary := decorate(primaryBase, cfg, cfg.Provider.Default, cfg.Provider.BaseURL, logger)

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
		fbBase, err := buildOne(buildOneConfig{
			name:            fb.Provider,
			apiKey:          apiKey,
			baseURL:         fb.BaseURL,
			headers:         cfg.Provider.HeadersFor(fb.Provider, fb.BaseURL),
			think:           cfg.Provider.Think,
			reasoningEffort: cfg.Provider.ReasoningEffort,
			maxTokens:       cfg.Provider.MaxTokens,
			// LLM-11: this target's own window, never the primary's
			// cfg.Provider.ContextWindow — see ProviderFallbackConfig.ContextWindow.
			contextWindow:         fb.ContextWindow,
			keepAlive:             cfg.Provider.KeepAlive,
			responseHeaderTimeout: cfg.Provider.ResponseHeaderTimeout(),
			streamIdleTimeout:     cfg.Provider.StreamIdleTimeout(),
			logger:                logger,
			caps:                  o.caps,
		})
		if err != nil {
			logger.Warn("provider fallback: skipping misconfigured fallback", "provider", fb.Provider, "err", err)
			continue
		}
		targets = append(targets, provider.FallbackTarget{
			Adapter: decorate(fbBase, cfg, fb.Provider, fb.BaseURL, logger),
			Model:   fb.Model,
		})
	}
	return provider.WithFailover(primary, targets, logger), nil
}

// Decorate applies the shipping decorator chain to a caller-supplied base
// adapter, exactly as Build applies it to the adapter it constructs.
//
// It exists so a test can exercise the composition that ships rather than a
// hand-assembled approximation of it (M10/EXEC-6). An eval scenario handed its
// scripted adapter straight to engine.New, so every decorator between the
// engine and the backend — retry, admission control, the per-model harness
// profile and the prose-tool-call salvage inside it — was untested in
// composition with the engine, which is the only place their behavior is
// observable. Wrapping a providertest.Adapter with this closes that.
//
// The failover layer is deliberately not part of it: failover is about a
// *second* backend and belongs to Build, which knows the fallback list.
func Decorate(base provider.Adapter, cfg *config.Config, logger *slog.Logger) provider.Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return decorate(base, cfg, cfg.Provider.Default, cfg.Provider.BaseURL, logger)
}

// decorate is the one place the per-backend decorator stack is spelled. Build
// applies it to the primary adapter and to every fallback target, and Decorate
// exposes it for tests — three callers, one spelling, so a decorator added here
// reaches all of them.
// validateModelHarness rejects a config.Provider.ModelHarness that P74.21's
// runtime invariants can't let through: a per-model prompt suffix too large
// for the local prompt profile's budget, or a DeferredTools entry naming a
// profile.RequiredExposedTools tool. Both are checked once here, at Build
// time, rather than left to fail — or worse, silently degrade — on whichever
// request first resolves the offending model's Harness.
func validateModelHarness(cfg *config.Config) error {
	if err := profile.ValidateOverrides(cfg.Provider.ModelHarness); err != nil {
		return err
	}
	local := cfg.Provider.LocalPromptProfile()
	for model, o := range cfg.Provider.ModelHarness {
		if o.PromptSuffix == nil {
			continue
		}
		if !sysprompt.FitsLocalBudget(*o.PromptSuffix, local) {
			return fmt.Errorf("model_harness[%q].prompt_suffix: %d estimated tokens exceeds the %d-token local-profile budget (sysprompt.LocalPromptSuffixMaxTokens)",
				model, tokenest.Estimate(*o.PromptSuffix), sysprompt.LocalPromptSuffixMaxTokens)
		}
	}
	return nil
}

func decorate(base provider.Adapter, cfg *config.Config, providerName, baseURL string, logger *slog.Logger) provider.Adapter {
	policy := provider.DefaultRetryPolicy()
	policy.MaxRetries = cfg.Provider.MaxRetries
	resolve := profile.NewResolver(cfg.Provider.LocalPromptProfile(), cfg.Provider.ModelHarness)
	adapted := provider.WithHarness(
		provider.WithRetry(admit(cfg, providerName, baseURL, base, logger), policy, logger),
		resolve,
	)
	return redactOutbound(cfg, providerName, baseURL, adapted, logger)
}

// redactOutbound wraps adapted with provider.WithRedaction (P81.5/FIND-05)
// when the resolved endpoint is not loopback and
// security.redact_outbound_payloads (on by default) hasn't been turned off —
// the same config.IsLoopbackBaseURL gate MeteredCloudEndpoint (P81.15) uses
// to draw the local/remote line. Applied outermost, once per built adapter,
// so provider.WithRetry's own retries replay the same already-redacted
// request rather than re-running the pass per attempt.
func redactOutbound(cfg *config.Config, providerName, baseURL string, adapted provider.Adapter, logger *slog.Logger) provider.Adapter {
	if !cfg.Security.RedactOutboundPayloads || config.IsLoopbackBaseURL(baseURL) {
		return adapted
	}
	return provider.WithRedaction(adapted, providerName, logger)
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

// buildOneConfig bundles buildOne's parameters. The primary and every
// fallback target build from the same field set (P78.6) — as 12 positional
// arguments identical at both call sites, a same-typed swap (e.g.
// maxTokens/contextWindow, both int) compiled cleanly and neither call site
// was diffable against the signature at a glance.
type buildOneConfig struct {
	name                  string
	apiKey                string
	baseURL               string
	headers               map[string]string
	think                 *bool
	reasoningEffort       string
	maxTokens             int
	contextWindow         int
	keepAlive             string
	responseHeaderTimeout time.Duration
	streamIdleTimeout     time.Duration
	logger                *slog.Logger
	caps                  ollama.CapabilityStore
}

// buildOne constructs a single unwrapped adapter for cfg.name. Shared by the
// primary and every fallback target so their construction rules (base URL
// defaults, thinking/reasoning options, key requirements) stay identical.
func buildOne(cfg buildOneConfig) (provider.Adapter, error) {
	name, apiKey, baseURL := cfg.name, cfg.apiKey, cfg.baseURL
	headers, think, reasoningEffort := cfg.headers, cfg.think, cfg.reasoningEffort
	maxTokens, contextWindow, keepAlive := cfg.maxTokens, cfg.contextWindow, cfg.keepAlive
	responseHeaderTimeout, streamIdleTimeout := cfg.responseHeaderTimeout, cfg.streamIdleTimeout
	logger, caps := cfg.logger, cfg.caps
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
		// Thinking defaults on (P77.1): local reasoning is free (no per-token
		// billing, unlike Anthropic's thinking budget above), and the TUI
		// already renders it as a collapsible block — so the honest default is
		// "show it when the model produces it." A model that doesn't support
		// `think` 400s once, and the P38.5 retry/latch machinery in the ollama
		// adapter (thinkRejected) omits the field for it from then on, so this
		// costs at most one failed request per unsupported model per process,
		// never a repeat one. Set `provider.think: false` to opt back out.
		trueVal := true
		if think == nil {
			think = &trueVal
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
