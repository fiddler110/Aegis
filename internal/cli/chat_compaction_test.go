package cli

import (
	"context"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

// TestDriveCompaction_EnabledForCloudProvider is the P47.1 regression guard:
// the CLI `chat --skill` drive engine must be built with proactive per-turn
// compaction — a non-zero context window AND a non-nil compactor — so a
// multi-turn drive can't grow context unbounded until the model server
// hard-rejects the request. The CLI path once set neither, diverging silently
// from the daemon (which wires both) and hard-aborting an unattended run.
//
// A cloud provider with an explicit context_window is used so the helper
// resolves the window from config without probing a (possibly running) local
// Ollama server, keeping the test deterministic and network-free.
func TestDriveCompaction_EnabledForCloudProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Model = "claude-sonnet-5"
	cfg.Provider.ContextWindow = 200_000

	compactor, ctxWin := driveCompaction(context.Background(), cfg, nil, discardLoggerCLI())

	if ctxWin != 200_000 {
		t.Fatalf("effective context window = %d, want 200000 (the configured window)", ctxWin)
	}
	if compactor == nil {
		t.Fatal("driveCompaction returned a nil compactor; engine.New would then disable proactive compaction")
	}
}

// TestDriveCompaction_SkipsWhenLocalWindowUnknown verifies the local-model
// safety valve: when the provider is Ollama and no window is configured or
// detectable (0), the compactor is still constructed but with auto-compaction
// disabled (MaxBudget == 0) rather than falling back to the 120k cloud budget,
// which on a small local server would never fire before the prompt is
// front-truncated. The compactor is non-nil either way; the engine gates on
// ctxWin > 0, which is 0 here.
func TestDriveCompaction_SkipsWhenLocalWindowUnknown(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Default = "ollama"
	// No ContextWindow and a base URL pointed at an almost-certainly-dead port
	// so ollamainfo.Detect returns !ok and the helper yields ctxWin == 0.
	cfg.Provider.BaseURL = "http://127.0.0.1:1/v1"
	cfg.Provider.Model = "does-not-matter"

	compactor, ctxWin := driveCompaction(context.Background(), cfg, nil, discardLoggerCLI())

	if ctxWin != 0 {
		t.Fatalf("effective context window = %d, want 0 (undetectable local window)", ctxWin)
	}
	if compactor == nil {
		t.Fatal("driveCompaction returned a nil compactor even in the skip case")
	}
}
