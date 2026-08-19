package enginecfg

import (
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

// TestBuiltinOptionsWiresSearch is the regression for the bug found live
// 2026-08-19: cfg.Search was set only in server.go's daemon path, as a
// manual overlay applied after this same BuiltinOptions call — so every
// other call site (aegis chat, aegis debate, aegis dryrun, worker.go) built
// its registry with a zero-value Search, silently ignoring a configured
// `search.provider`/`api_key`/`base_url` and falling back to the
// zero-config DuckDuckGo scrape no matter what the operator set. Search is
// a straight config reading, not host wiring, so it belongs in this
// function per its own doc comment — this test pins that it stays there.
func TestBuiltinOptionsWiresSearch(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider:   "searxng",
			APIKey:     "test-key",
			BaseURL:    "http://searxng.internal:8080",
			ScanOutput: true,
		},
	}
	opts := BuiltinOptions(cfg, "/ws")
	if opts.Search.Provider != "searxng" {
		t.Errorf("Search.Provider = %q, want searxng", opts.Search.Provider)
	}
	if opts.Search.APIKey != "test-key" {
		t.Errorf("Search.APIKey = %q, want test-key", opts.Search.APIKey)
	}
	if opts.Search.BaseURL != "http://searxng.internal:8080" {
		t.Errorf("Search.BaseURL = %q, want http://searxng.internal:8080", opts.Search.BaseURL)
	}
	if !opts.Search.ScanOutput {
		t.Error("Search.ScanOutput = false, want true")
	}
}
