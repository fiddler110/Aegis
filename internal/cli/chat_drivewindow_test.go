package cli

import (
	"context"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

// TestRecommendPhasedDriveWindow_NonOllamaGate: the up-front window sizing
// (P47.5a) only applies to an Ollama-backed provider. A cloud provider (no
// base_url) must return ok=false so the caller leaves the configured window
// untouched — no probe, no override.
//
// Stayed in internal/cli when the drive moved to internal/drive (P52.12): the
// escalation *policy* (drive.NextWindow) is the drive's, but resolving a
// provider's model max is config/ollamainfo probing the drive has no business
// doing — the host builds the EscalateWindow closure and hands it in.
func TestRecommendPhasedDriveWindow_NonOllamaGate(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Default = "anthropic"
	cfg.Provider.BaseURL = ""
	if _, _, ok := recommendPhasedDriveWindow(context.Background(), cfg); ok {
		t.Error("a non-Ollama provider must not yield a phased-drive window recommendation")
	}
}
