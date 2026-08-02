package modelcaps

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// ReconcileTimeout bounds a whole ReconcileOllama call. Short on purpose: this
// is a cache refresh, and a slow or wedged model server must delay a daemon
// start (or an `aegis doctor` run) by seconds at most. Failing it costs
// nothing — records keep their existing fingerprints and get another chance
// next time.
const ReconcileTimeout = 5 * time.Second

// IsOllamaProvider reports whether a (provider, base_url) pair names a local
// Ollama-style server — the same test `aegis doctor` and the daemon both use,
// kept here so every caller of ReconcileOllama gates identically.
func IsOllamaProvider(providerName, baseURL string) bool {
	return strings.EqualFold(providerName, "ollama") || strings.Contains(baseURL, ":11434")
}

// ReconcileOllama invalidates persisted records whose model weights changed
// since they were measured, and records model's manifest-claimed tool support.
//
// This is what makes persistence safe at all. An Ollama tag is mutable —
// `ollama pull qwen3:14b` can replace the weights without the name changing —
// which is exactly why toolcallprobe.Gate refused to write verdicts to disk.
// Keying invalidation on the content digest from /api/tags rather than on the
// name closes that hole: a re-pulled model loses its record and gets re-probed,
// a stable one keeps it.
//
// Every writer should call this before it writes, not only the daemon: a
// process that writes a verdict without a digest snapshot produces an
// unfingerprinted record, which the next reconcile can only adopt (it has no
// way to tell whether the weights moved in between).
//
// An unreachable server is a no-op rather than a mass invalidation — "cannot
// tell" must never be read as "everything is stale". logger may be nil.
func ReconcileOllama(ctx context.Context, s *Store, nativeBase, model string, logger *slog.Logger) {
	if s == nil || nativeBase == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, ReconcileTimeout)
	defer cancel()

	digests, ok := ollamainfo.Digests(ctx, nativeBase)
	if !ok {
		return
	}
	if dropped := s.Reconcile(digests); dropped > 0 && logger != nil {
		logger.Info("model capability cache: dropped records for models whose weights changed", "count", dropped)
	}

	// The manifest's own tool-support claim, recorded alongside the probe's
	// measured verdict rather than in place of it — the two can disagree, and
	// that disagreement is the informative part.
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		return
	}
	if native, known := ollamainfo.NativeToolSupport(ctx, nativeBase, model); known {
		s.SetNativeToolSupport(model, native)
	}
}
