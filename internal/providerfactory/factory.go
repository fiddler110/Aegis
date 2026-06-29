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
// retry/backoff for transient failures. Pass a non-nil logger so retry WARN
// messages go there instead of slog.Default() (which writes to stderr and would
// corrupt the TUI display).
func Build(cfg *config.Config, logger *slog.Logger) (provider.Adapter, error) {
	var base provider.Adapter
	switch cfg.Provider.Default {
	case "anthropic":
		if cfg.Provider.APIKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
		}
		opts := []anthropic.Option{
			anthropic.WithBaseURL(cfg.Provider.BaseURL),
			anthropic.WithHeaders(cfg.Provider.Headers),
		}
		// Enable extended thinking when explicitly requested, budgeting half of
		// max_tokens for reasoning (clamped to the API's 1024 minimum).
		if cfg.Provider.Think != nil && *cfg.Provider.Think {
			budget := cfg.Provider.MaxTokens / 2
			if budget < 1024 {
				budget = 1024
			}
			opts = append(opts, anthropic.WithThinking(budget))
		}
		base = anthropic.New(cfg.Provider.APIKey, opts...)

	case "ollama":
		// Ollama uses an OpenAI-compatible API. Default to localhost:11434 when
		// no base URL is configured. Thinking is disabled by default to prevent
		// reasoning preambles in non-thinking tasks.
		baseURL := cfg.Provider.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		falseVal := false
		think := cfg.Provider.Think
		if think == nil {
			think = &falseVal // suppress Ollama thinking unless explicitly enabled
		}
		base = openai.New(cfg.Provider.APIKey,
			openai.WithBaseURL(baseURL),
			openai.WithHeaders(cfg.Provider.Headers),
			openai.WithThink(think),
		)

	case "openai":
		// Require an API key only when using the real OpenAI endpoint. Local
		// servers (LM Studio, liteLLM) have no auth requirement when base_url
		// is configured.
		if cfg.Provider.APIKey == "" && cfg.Provider.BaseURL == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is not set")
		}
		opts := []openai.Option{
			openai.WithBaseURL(cfg.Provider.BaseURL),
			openai.WithHeaders(cfg.Provider.Headers),
		}
		// Only send the Ollama-specific `think` field when targeting an
		// OpenAI-compatible local server, not the real openai.com API.
		if cfg.Provider.BaseURL != "" && cfg.Provider.Think != nil {
			opts = append(opts, openai.WithThink(cfg.Provider.Think))
		}
		if cfg.Provider.ReasoningEffort != "" {
			opts = append(opts, openai.WithReasoningEffort(cfg.Provider.ReasoningEffort))
		}
		base = openai.New(cfg.Provider.APIKey, opts...)

	default:
		return nil, fmt.Errorf("unsupported provider %q (supported: anthropic, openai, ollama)", cfg.Provider.Default)
	}

	policy := provider.DefaultRetryPolicy()
	policy.MaxRetries = cfg.Provider.MaxRetries
	return provider.WithRetry(base, policy, logger), nil
}
