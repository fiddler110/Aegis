// Package providerfactory builds a provider.Adapter from configuration,
// centralizing provider selection so the daemon and CLI share one code path.
package providerfactory

import (
	"fmt"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/provider/anthropic"
	"github.com/scottymacleod/aegis/internal/provider/openai"
)

// Build constructs the adapter selected by cfg.Provider.Default, wrapped with
// retry/backoff for transient failures.
func Build(cfg *config.Config) (provider.Adapter, error) {
	var base provider.Adapter
	switch cfg.Provider.Default {
	case "anthropic":
		if cfg.Provider.APIKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
		}
		base = anthropic.New(cfg.Provider.APIKey,
			anthropic.WithBaseURL(cfg.Provider.BaseURL),
			anthropic.WithHeaders(cfg.Provider.Headers),
		)
	case "openai":
		// Require an API key only when using the real OpenAI endpoint. Local
		// servers (Ollama, LM Studio) have no auth requirement, so we skip the
		// check when a custom base_url is configured.
		if cfg.Provider.APIKey == "" && cfg.Provider.BaseURL == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is not set")
		}
		base = openai.New(cfg.Provider.APIKey,
			openai.WithBaseURL(cfg.Provider.BaseURL),
			openai.WithHeaders(cfg.Provider.Headers),
			openai.WithThink(cfg.Provider.Think),
		)
	default:
		return nil, fmt.Errorf("unsupported provider %q (supported: anthropic, openai)", cfg.Provider.Default)
	}

	policy := provider.DefaultRetryPolicy()
	policy.MaxRetries = cfg.Provider.MaxRetries
	return provider.WithRetry(base, policy, nil), nil
}
