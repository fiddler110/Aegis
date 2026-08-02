package server

import (
	"context"
	"log/slog"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/modelcaps"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// reconcileModelCaps refreshes the P53.5 capability cache at daemon start:
// records whose model weights have changed since they were measured are
// dropped, and the configured model's manifest-claimed tool support is
// recorded. See modelcaps.ReconcileOllama for why digest-keyed invalidation is
// what makes persisting a probe verdict safe.
//
// Non-Ollama providers are skipped entirely — there is nothing to fingerprint
// and no discovered quirk to persist, since the cloud adapters declare their
// capabilities statically rather than learning them.
func reconcileModelCaps(cfg *config.Config, store *modelcaps.Store, logger *slog.Logger) {
	if store == nil || cfg == nil {
		return
	}
	p := cfg.Provider
	if !modelcaps.IsOllamaProvider(p.Default, p.BaseURL) {
		return
	}
	modelcaps.ReconcileOllama(context.Background(), store, ollamainfo.NativeBase(p.BaseURL), p.Model, logger)
}
