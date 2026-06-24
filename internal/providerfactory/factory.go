// Package providerfactory builds a provider.Adapter from configuration,
// centralizing provider selection so the daemon and CLI share one code path.
package providerfactory

import (
	"fmt"

	"github.com/scottymacleod/agentharness/internal/config"
	"github.com/scottymacleod/agentharness/internal/provider"
	"github.com/scottymacleod/agentharness/internal/provider/anthropic"
	"github.com/scottymacleod/agentharness/internal/provider/openai"
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
		base = anthropic.New(cfg.Provider.APIKey, anthropic.WithBaseURL(cfg.Provider.BaseURL))
	case "openai":
		if cfg.Provider.APIKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is not set")
		}
		base = openai.New(cfg.Provider.APIKey, openai.WithBaseURL(cfg.Provider.BaseURL))
	default:
		return nil, fmt.Errorf("unsupported provider %q (supported: anthropic, openai)", cfg.Provider.Default)
	}

	policy := provider.DefaultRetryPolicy()
	policy.MaxRetries = cfg.Provider.MaxRetries
	return provider.WithRetry(base, policy, nil), nil
}
