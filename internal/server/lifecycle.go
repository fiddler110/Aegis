// The daemon's HTTP lifecycle: route table, listen-address validation, and
// ListenAndServe's startup/shutdown sequence. Extracted from server.go (L4).
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Handler exposes the HTTP routes for testing with httptest.
func (s *Server) Handler() http.Handler { return s.routes() }

// routes registers the daemon's HTTP API.
//
// Any handler that spends model tokens (calls engine.Run or otherwise drives
// an adapter) must call s.beginDailySpend before starting and defer
// guard.Finish immediately after, the same way handlePostMessage and
// handleDebate do (see spendGuard's doc comment) — this doesn't stop a new
// handler from skipping the call entirely (there is no compiler-enforced
// guarantee of that, unlike the P14.10 commandDefs table for the TUI's
// command surface), but it does close the narrower gap that actually bit
// once already: /debate shipped calling the daily-cap check/record pair
// as two independent free functions, and never called the second half at
// all, so its spend was invisible to every other cap check until the gap
// was found and fixed in the same review that caught P14.1's TUI
// command-surface drift. Folding both halves into one guarded type removes
// the "called one half but not the other" failure mode.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /status", s.handleStatusInfo)
	mux.HandleFunc("POST /sessions", s.handleCreateSession)
	mux.HandleFunc("GET /sessions", s.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	mux.HandleFunc("PATCH /sessions/{id}", s.handleUpdateSession)
	mux.HandleFunc("DELETE /sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /sessions/{id}/messages", s.handlePostMessage)
	mux.HandleFunc("POST /sessions/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /sessions/{id}/steer", s.handleSteer)
	mux.HandleFunc("POST /sessions/{id}/drive", s.handleDrive)      // P52.12: phased skill drive over the SSE seam
	mux.HandleFunc("GET /sessions/{id}/drive", s.handleDriveSkills) // P52.12: which skills this session can drive
	mux.HandleFunc("POST /sessions/{id}/stop", s.handleStopRun)     // P28.5: stop a resumable run out of band
	mux.HandleFunc("GET /sessions/{id}/checkpoints", s.handleListCheckpoints)
	mux.HandleFunc("POST /sessions/{id}/rewind", s.handleRewind)
	mux.HandleFunc("POST /sessions/{id}/fork", s.handleFork) // P22.3
	mux.HandleFunc("POST /sessions/{id}/compact", s.handleCompactSession)
	mux.HandleFunc("POST /sessions/{id}/skills/activate", s.handleActivateSkill)
	mux.HandleFunc("POST /sessions/{id}/background", s.handleSetBackground) // P3.2
	mux.HandleFunc("GET /sessions/{id}/events", s.handleGetBGEvents)        // P3.2
	mux.HandleFunc("POST /sessions/{id}/archive", s.handleArchiveSession)
	mux.HandleFunc("POST /sessions/{id}/unarchive", s.handleUnarchiveSession)
	mux.HandleFunc("POST /sessions/prune", s.handlePruneSessions)
	mux.HandleFunc("GET /runs", s.handleListRuns)
	mux.HandleFunc("GET /cron/jobs", s.handleListCronJobs)
	mux.HandleFunc("GET /teammates", s.handleListTeammates)
	mux.HandleFunc("GET /commands", s.handleListCommands)
	mux.HandleFunc("GET /memory", s.handleGetMemory)
	mux.HandleFunc("POST /memory", s.handleAppendMemory)
	mux.HandleFunc("GET /personas", s.handleListPersonas)
	mux.HandleFunc("POST /security/scan", s.handleScan)
	mux.HandleFunc("GET /security/status", s.handleSecurityStatus)
	mux.HandleFunc("GET /security/baseline", s.handleSecurityBaseline)
	mux.HandleFunc("POST /security/install", s.handleSecurityInstall)
	mux.HandleFunc("GET /config/sandbox", s.handleGetConfigSandbox)
	mux.HandleFunc("PATCH /config/sandbox", s.requireAdminToken(s.handlePatchConfigSandbox))
	mux.HandleFunc("GET /config/security", s.handleGetConfigSecurity)
	mux.HandleFunc("PATCH /config/security", s.requireAdminToken(s.handlePatchConfigSecurity))
	mux.HandleFunc("GET /config/skills", s.handleGetConfigSkills)
	mux.HandleFunc("GET /models/local", s.handleListLocalModels)
	mux.HandleFunc("PATCH /config/skills", s.requireAdminToken(s.handlePatchConfigSkills))
	mux.HandleFunc("GET /config/cost", s.handleGetConfigCost)
	mux.HandleFunc("PATCH /config/cost", s.requireAdminToken(s.handlePatchConfigCost))
	mux.HandleFunc("POST /config/harden", s.handleConfigHarden)
	mux.HandleFunc("POST /debate", s.handleDebate)
	mux.HandleFunc("POST /knowledge", s.handleKnowledge)
	mux.HandleFunc("POST /repomap/index", s.handleRepoMapIndex)
	mux.HandleFunc("GET /ui", s.handleWebUI)
	mux.HandleFunc("GET /ui/", s.handleWebUI)
	mux.HandleFunc("GET /ui/assets/", s.handleWebUIAssets)
	mux.HandleFunc("POST /auth/exchange", s.handleAuthExchange)
	// Not exempted from authMiddleware: a caller must already present a valid
	// credential (bearer token or browser session) to log out of it, so
	// nothing here is reachable without first being authenticated (P81.4).
	mux.HandleFunc("POST /auth/logout", s.handleAuthLogout)
	return s.authMiddleware(s.originMiddleware(mux))
}

// validateListenAddr enforces FIND-08: server.addr defaults to loopback, but
// nothing previously stopped an operator from pointing it at a non-loopback
// address (e.g. "0.0.0.0:4127") and silently exposing the bearer-token-
// protected, unrate-limited API to the network. Fail closed unless the
// operator has explicitly acknowledged the tradeoff via server.allow_remote.
func (s *Server) validateListenAddr() error {
	if isLoopbackAddr(s.cfg.Server.Addr) {
		return nil
	}
	if !s.cfg.Server.AllowRemote {
		return fmt.Errorf("server: refusing to bind non-loopback address %q: set server.allow_remote: true to acknowledge that this exposes the daemon's API to the network (see docs/configuration.md)", s.cfg.Server.Addr)
	}
	s.logger.Warn("daemon is binding to a non-loopback address; its bearer-token-protected API will be reachable from the network", "addr", s.cfg.Server.Addr)
	return nil
}

// ListenAndServe runs the daemon until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	s.daemonCtx = ctx
	// One teardown for every exit (P79.2), registered before the first of them.
	// New has already acquired everything by the time this is called, so a
	// refusal to start needs the same release as a graceful shutdown. Before
	// this, the second half of teardown (cron, swarm, tasks, sandbox, language
	// servers) ran only in the ctx.Done branch: a daemon that died because its
	// address was refused, or because the port was taken, left LSP child
	// processes and a persistent sandbox container behind.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = s.Close(closeCtx)
	}()
	if s.authToken == "" {
		return fmt.Errorf("server: refusing to start: auth token was not generated")
	}
	if s.adminToken == "" {
		return fmt.Errorf("server: refusing to start: admin token was not generated")
	}
	if err := s.validateListenAddr(); err != nil {
		return err
	}
	// P81.17: the committed dist/ has no PR-time drift check that runs
	// (P81.11 confirmed that disablement is deliberate and permanent), so
	// this is the only thing left that verifies the embedded web UI bundle
	// against the digest of the bundle that was actually reviewed.
	if drift, digest, err := checkDistDrift(); err != nil {
		s.logger.Warn("web UI bundle drift check failed", "err", err)
	} else if drift {
		s.logger.Warn("web UI bundle does not match its pinned manifest — dist/ may have been modified since the last reviewed build", "digest", digest)
	} else {
		s.logger.Info("web UI bundle matches its pinned manifest", "digest", digest)
	}
	// Start the cron scheduler in the background.
	if s.cronSched != nil {
		cronCtx, cronCancel := context.WithCancel(context.Background())
		s.cronCancel = cronCancel
		go s.cronSched.Run(cronCtx)
	}

	// Start the auto-pruner ticker when any of the three retention horizons
	// is configured (P81.24 added ArchivedSessionTTLDays and
	// CheckpointTTLDays alongside the original SessionTTLDays) — one ticker,
	// each horizon gated independently so leaving one at its 0/disabled
	// default never blocks the others.
	if s.cfg.Cleanup.SessionTTLDays > 0 || s.cfg.Cleanup.ArchivedSessionTTLDays > 0 || s.cfg.Cleanup.CheckpointTTLDays > 0 {
		interval := 24 * time.Hour
		if h := s.cfg.Cleanup.IntervalHours; h > 0 {
			interval = time.Duration(h) * time.Hour
		}
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.runRetentionPrune()
				}
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		if s.tlsCert != nil {
			s.logger.Info("daemon listening (TLS)", "addr", s.cfg.Server.Addr)
			s.http.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*s.tlsCert}}
			// Cert/key are already loaded into TLSConfig above; passing empty
			// paths here tells ListenAndServeTLS to use that config instead of
			// reading files itself (see the method's doc comment).
			errCh <- s.http.ListenAndServeTLS("", "")
		} else {
			s.logger.Info("daemon listening", "addr", s.cfg.Server.Addr)
			errCh <- s.http.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		// Resource teardown is the deferred s.Close above; this only drains
		// in-flight HTTP requests. Ordering is deliberate: Shutdown returns
		// once handlers are done, so Close runs after the last handler that
		// could still be touching a store.
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// runRetentionPrune runs one pass of every configured retention horizon
// (P81.24): idle non-archived sessions, archived sessions past their own
// clock, and checkpoints past theirs — independent of whether their owning
// session still exists. Each is gated on its own TTL being non-zero, so
// leaving one at the disabled default never blocks the others sharing this
// ticker.
//
// Both session sweeps are followed by forgetPrunedSessions (ARCH-10): a row
// deleted here takes its in-process maps with it, the same way the delete
// handler's does. It runs once at the end rather than after each sweep because
// the reconciliation is against the store's whole live set either way.
func (s *Server) runRetentionPrune() {
	pruned := false
	if s.cfg.Cleanup.SessionTTLDays > 0 {
		ttl := time.Duration(s.cfg.Cleanup.SessionTTLDays) * 24 * time.Hour
		if n, err := s.store.Prune(context.Background(), ttl); err != nil {
			s.logger.Error("auto-prune sessions", "err", err)
		} else if n > 0 {
			pruned = true
			s.logger.Info("auto-pruned old sessions", "deleted", n, "ttl_days", s.cfg.Cleanup.SessionTTLDays)
		}
	}
	if s.cfg.Cleanup.ArchivedSessionTTLDays > 0 {
		ttl := time.Duration(s.cfg.Cleanup.ArchivedSessionTTLDays) * 24 * time.Hour
		if n, err := s.store.PruneArchived(context.Background(), ttl); err != nil {
			s.logger.Error("auto-prune archived sessions", "err", err)
		} else if n > 0 {
			pruned = true
			s.logger.Info("auto-pruned archived sessions", "deleted", n, "ttl_days", s.cfg.Cleanup.ArchivedSessionTTLDays)
		}
	}
	if pruned {
		s.forgetPrunedSessions(context.Background())
	}
	if s.cfg.Cleanup.CheckpointTTLDays > 0 && s.checkpoints != nil {
		cutoff := time.Now().Add(-time.Duration(s.cfg.Cleanup.CheckpointTTLDays) * 24 * time.Hour)
		if n, err := s.checkpoints.PruneOlderThan(context.Background(), cutoff); err != nil {
			s.logger.Error("auto-prune checkpoints", "err", err)
		} else if n > 0 {
			s.logger.Info("auto-pruned old checkpoints", "deleted", n, "ttl_days", s.cfg.Cleanup.CheckpointTTLDays)
		}
	}
}

// shutdownGrace bounds both the HTTP drain and Close's own background-worker
// waits. It is deliberately short: everything it waits on is either already
// finished or wedged, and a daemon that will not exit is worse than one that
// abandons a stuck sub-agent.
const shutdownGrace = 5 * time.Second

// Close releases everything New acquired: background workers first (they can
// still write to a store), then the child processes, then the open files and
// databases. It is safe to call more than once and safe on a Server that never
// served — which is the point of exporting it. New opens an audit log, up to
// three SQLite databases, a set of MCP server subprocesses and possibly a
// persistent sandbox container; before this existed the only code that let go
// of any of it was ListenAndServe, so every embedder that drives a Server
// through Handler() (the eval harness, tests, anything embedding the daemon)
// leaked the lot for the life of the process. On Windows that is not merely
// untidy: the still-open handles make the data directory undeletable, which is
// how this was found (C2, the first live_workflow run).
//
// ctx bounds the workers that take one; a nil ctx is treated as
// context.Background(). The returned error is the first failure encountered,
// with the rest logged — teardown always continues past a failing step.
func (s *Server) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var firstErr error
	fail := func(what string, err error) {
		if err == nil {
			return
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", what, err)
		}
		if s.logger != nil {
			s.logger.Warn("daemon shutdown: releasing resource failed", "resource", what, "err", err)
		}
	}

	s.closeOnce.Do(func() {
		// Producers first: each of these can still be writing to a store.
		if s.cronCancel != nil {
			s.cronCancel()
		}
		if s.toolCalling != nil {
			// Cancels any background conformance refinement (P53.4) so probe
			// goroutines die with the daemon rather than generating against a
			// model server nobody is listening to.
			s.toolCalling.Close()
		}
		if s.swarm != nil {
			s.swarm.Shutdown(ctx)
		}
		if s.tasks != nil {
			s.tasks.Shutdown(ctx)
		}

		// Child processes.
		if s.sandbox != nil {
			s.sandbox.Close()
		}
		if s.lspMgr != nil {
			s.lspMgr.Close()
		}
		for _, c := range s.mcpClients {
			fail("mcp client", c.Close())
		}

		// Files and databases last.
		if s.knowledge != nil {
			fail("knowledge store", s.knowledge.Close())
		}
		if s.longMem != nil {
			fail("long-term memory store", s.longMem.Close())
		}
		// Session-scoped knowledge stores opened for a non-default Workdir
		// (P25.9) — s.knowledge above only covers the daemon's own workspace.
		for _, ks := range s.knowledgeStores.values() {
			fail("session knowledge store", ks.Close())
		}
		if s.audit != nil {
			fail("audit log", s.audit.Close())
		}
		if s.store != nil {
			fail("session store", s.store.Close())
		}
	})
	return firstErr
}
