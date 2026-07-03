// Package providerfactory builds a provider.Adapter from configuration,
// centralizing provider selection so the daemon and CLI share one code path.
package providerfactory

import (
	"fmt"
	"log/slog"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/provider/anthropic"
	"github.com/scottymacleod/aegis/internal/provider/openai"
)

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

	primaryBase, err := buildOne(cfg.Provider.Default, cfg.Provider.APIKey, cfg.Provider.BaseURL, cfg.Provider.Headers, cfg.Provider.Think, cfg.Provider.ReasoningEffort, cfg.Provider.MaxTokens)
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
		fbBase, err := buildOne(fb.Provider, apiKey, fb.BaseURL, cfg.Provider.Headers, cfg.Provider.Think, cfg.Provider.ReasoningEffort, cfg.Provider.MaxTokens)
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

// buildOne constructs a single unwrapped adapter for provider name. Shared by
// the primary and every fallback target so their construction rules
// (base URL defaults, thinking/reasoning options, key requirements) stay
// identical.
func buildOne(name, apiKey, baseURL string, headers map[string]string, think *bool, reasoningEffort string, maxTokens int) (provider.Adapter, error) {
	switch name {
	case "anthropic":
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
		}
		opts := []anthropic.Option{
			anthropic.WithBaseURL(baseURL),
			anthropic.WithHeaders(headers),
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
		// Ollama uses an OpenAI-compatible API. Default to localhost:11434 when
		// no base URL is configured. Thinking is disabled by default to prevent
		// reasoning preambles in non-thinking tasks.
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		falseVal := false
		if think == nil {
			think = &falseVal // suppress Ollama thinking unless explicitly enabled
		}
		return openai.New(apiKey,
			openai.WithBaseURL(baseURL),
			openai.WithHeaders(headers),
			openai.WithThink(think),
		), nil

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
