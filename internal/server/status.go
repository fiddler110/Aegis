// /healthz and /status — the two read-only handlers every client polls.
// Extracted from server.go (L4).
package server

import (
	"net/http"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// Readiness only, and nothing else — see api.HealthStatus' doc comment for
	// why the model name and the sandbox-fallback state moved to the
	// authenticated /status. This route needs no credential, so anything added
	// here is readable by any local process.
	writeJSON(w, http.StatusOK, api.HealthStatus{Status: "ok"})
}

// handleStatusInfo serves the P14.5 /status TUI surface: daemon/provider
// identity, sandbox fallback state (which /healthz deliberately does not
// carry — see api.HealthStatus), the
// cross-session daily spend the P9.5/P10.5 caps already track in the session
// store, and (P28.7) a lightweight provider-reachability/latency probe.
// Kept as a separate endpoint from /healthz rather than growing that one,
// since /healthz is polled frequently (waitForDaemon) and shouldn't pay for
// two extra DB reads plus a network probe per call.
func (s *Server) handleStatusInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dailyCost, err := s.store.TodayCost(ctx)
	if err != nil {
		s.logger.Warn("read daily cost for /status", "err", err)
		dailyCost = 0
	}
	dailyTokens, err := s.store.TodayTokens(ctx)
	if err != nil {
		s.logger.Warn("read daily tokens for /status", "err", err)
		dailyTokens = 0
	}
	ctxWin, ctxWinSrc := s.effectiveContextWindow()
	reachable, latencyMS := s.probeProviderReachability(ctx)
	resp := api.StatusInfo{
		Provider:              s.cfg.Provider.Default,
		Model:                 s.cfg.Provider.Model,
		SandboxFallback:       s.sandboxFallback,
		SandboxFallbackReason: s.sandboxFallbackReason,
		DailyCostUSD:          dailyCost,
		DailyCapUSD:           s.cfg.Cost.DailyCapUSD,
		DailyTokens:           dailyTokens,
		DailyTokenCap:         s.cfg.Cost.DailyTokenCap,
		AgentConcurrency:      s.agentLimiter.Cap(),
		AgentConcurrencyMax:   builtin.MaxParallelAgents,
		ContextWindow:         ctxWin,
		ContextWindowSource:   ctxWinSrc,
		Workspace:             s.workspace,
		WorkdirAllowlist:      s.cfg.Server.SessionWorkdirAllowlist,
		ProviderReachable:     reachable,
		ProviderLatencyMS:     latencyMS,
	}
	writeJSON(w, http.StatusOK, resp)
}
