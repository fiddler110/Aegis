// Package server is the harness daemon. It owns the session store, the model
// adapter, the tool registry, and runs the agent engine, exposing everything
// over a local HTTP API (with server-sent events for streaming runs).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scottymacleod/agentharness/internal/api"
	"github.com/scottymacleod/agentharness/internal/compaction"
	"github.com/scottymacleod/agentharness/internal/config"
	"github.com/scottymacleod/agentharness/internal/cron"
	"github.com/scottymacleod/agentharness/internal/cost"
	"github.com/scottymacleod/agentharness/internal/engine"
	"github.com/scottymacleod/agentharness/internal/hooks"
	"github.com/scottymacleod/agentharness/internal/mcp"
	"github.com/scottymacleod/agentharness/internal/memory"
	"github.com/scottymacleod/agentharness/internal/permission"
	"github.com/scottymacleod/agentharness/internal/persona"
	"github.com/scottymacleod/agentharness/internal/sandbox"
	"github.com/scottymacleod/agentharness/internal/provider"
	"github.com/scottymacleod/agentharness/internal/providerfactory"
	"github.com/scottymacleod/agentharness/internal/session"
	"github.com/scottymacleod/agentharness/internal/swarm"
	"github.com/scottymacleod/agentharness/internal/task"
	"github.com/scottymacleod/agentharness/internal/tool"
	"github.com/scottymacleod/agentharness/internal/tool/builtin"
)

// Server holds the daemon's shared state.
type Server struct {
	cfg        *config.Config
	store      *session.Store
	adapter    provider.Adapter
	tools      *tool.Registry
	memory     memory.Sources
	compactor  engine.Compactor
	hooks      engine.Hooks
	mcpClients []*mcp.Client
	swarm      swarm.Backend
	swarmReg   *swarm.Registry
	tasks      *task.Manager
	cronSched  *cron.Scheduler
	cronCancel context.CancelFunc
	sandbox    sandbox.Backend
	audit      *hooks.Audit
	workspace  string
	logger     *slog.Logger
	http       *http.Server
}

// New constructs a daemon from config. The workspace root for tools is the
// process working directory.
func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.EnsureDataDir(); err != nil {
		return nil, err
	}
	store, err := session.Open(cfg.SessionDBPath())
	if err != nil {
		return nil, err
	}

	// A missing API key is not fatal: the daemon still serves session
	// management and reports the error only when a turn is actually run.
	adapter, err := providerfactory.Build(cfg)
	if err != nil {
		logger.Warn("provider not ready; message runs will fail until configured", "err", err)
		adapter = nil
	}

	// Background-task manager shares the session database's single connection.
	taskStore, err := task.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, err
	}
	taskMgr := task.NewManager(taskStore, logger)

	cwd, _ := os.Getwd()

	// Sandbox backend: try container runtime if configured, else local.
	var sb sandbox.Backend
	if cfg.Sandbox.Backend == "container" {
		csb, err := sandbox.NewContainerBackend(sandbox.ContainerOpts{
			Image:   cfg.Sandbox.Image,
			Network: cfg.Sandbox.Network,
			Prefer:  sandbox.ContainerRuntime(cfg.Sandbox.Runtime),
		})
		if err != nil {
			logger.Warn("container sandbox unavailable, falling back to local", "err", err)
			sb = sandbox.NewLocalBackend()
		} else {
			logger.Info("sandbox backend", "runtime", csb.DetectedRuntime(), "image", cfg.Sandbox.Image)
			sb = csb
		}
	} else {
		sb = sandbox.NewLocalBackend()
	}

	// Cron scheduler: fires due jobs as background tasks.
	cronStore, err := cron.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, err
	}
	runCronCmd := cronShellRunner(sb, cwd)
	cronRun := func(j cron.Job) {
		title := j.Title
		if title == "" {
			title = "cron: " + j.Command
		}
		_, _ = taskMgr.Start(task.Spec{Kind: "cron", Title: title}, func(ctx context.Context, emit func(string)) (string, error) {
			return "", runCronCmd(ctx, j.Command, emit)
		})
	}
	cronSched := cron.NewScheduler(cronStore, cronRun, logger)

	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL, Tasks: taskMgr, Cron: cronSched, Sandbox: sb}); err != nil {
		store.Close()
		return nil, err
	}

	s := newWithDeps(cfg, logger, store, adapter, reg)
	s.tasks = taskMgr
	s.cronSched = cronSched
	s.sandbox = sb
	s.workspace = cwd
	s.memory = memory.Sources{ProjectRoot: cwd, DataDir: cfg.DataDir}
	s.audit = hooks.NewAudit(filepath.Join(cfg.DataDir, "audit.jsonl"))
	s.hooks = hooks.NewMulti(s.audit)
	if adapter != nil {
		s.compactor = compaction.New(compaction.Options{Adapter: adapter, Model: cfg.Provider.Model})
	}

	// Connect configured MCP servers and register their tools.
	mcpServers := make([]mcp.ServerConfig, 0, len(cfg.MCP))
	for _, m := range cfg.MCP {
		mcpServers = append(mcpServers, mcp.ServerConfig{Name: m.Name, Command: m.Command, Args: m.Args, Env: m.Env})
	}
	s.mcpClients = mcp.RegisterServers(context.Background(), reg, mcpServers, logger)

	// Multi-agent: choose a sub-agent backend and register the `agent` tool.
	s.swarmReg = swarm.NewRegistry()
	s.swarm = s.buildSwarmBackend(swarm.MailboxRoot(cfg.DataDir))
	s.swarm.OnStop(s.onSubagentStop)
	if err := reg.Register(builtin.NewAgentTool(s.swarm, s.tasks)); err != nil {
		store.Close()
		return nil, err
	}

	return s, nil
}

// onSubagentStop records the SUBAGENT_STOP lifecycle event in the audit trail.
func (s *Server) onSubagentStop(id swarm.Identity, res swarm.Result) {
	status := "done"
	summary := res.Output
	if res.Failed() {
		status, summary = "failed", res.Err
	}
	if s.audit != nil {
		s.audit.SubagentStop(id.AgentID, status, truncateSummary(summary, 200), res.Failed())
	}
	s.logger.Info("subagent stopped", "agent", id.AgentID, "status", status)
}

func truncateSummary(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// buildSwarmBackend selects the sub-agent backend from config. The subprocess
// backend gives OS-level isolation by launching the harness binary in a headless
// worker mode; the default in-process backend runs teammates as goroutines.
func (s *Server) buildSwarmBackend(mailboxRoot string) swarm.Backend {
	if s.cfg.Swarm.Backend == "subprocess" {
		if exe, err := os.Executable(); err == nil {
			s.logger.Info("swarm backend: subprocess", "exe", exe)
			return swarm.NewSubprocessBackend(exe, "__worker", s.swarmReg, mailboxRoot)
		}
		s.logger.Warn("cannot resolve executable path; falling back to in-process swarm backend")
	}
	return swarm.NewInProcessBackend(s.subAgentRunner(), s.swarmReg, mailboxRoot)
}

// subAgentRunner returns a swarm.RunFunc that executes a teammate by building a
// sub-engine over the daemon's shared adapter and tools. The child runs with its
// own (clamped) permission mode and a fresh cost tracker.
func (s *Server) subAgentRunner() swarm.RunFunc {
	return func(ctx context.Context, cfg swarm.SpawnConfig) (string, error) {
		if s.adapter == nil {
			return "", fmt.Errorf("no model provider configured")
		}
		model := cfg.Model
		if model == "" {
			model = s.cfg.Provider.Model
		}
		gate := permission.New(permission.ParseMode(cfg.Mode), s.approver())
		eng, err := engine.New(engine.Options{
			Adapter:   s.adapter,
			Tools:     s.tools,
			Gate:      gate,
			Compactor: s.compactor,
			Hooks:     s.hooks,
			Cost:      cost.NewTracker(),
			BudgetUSD: s.cfg.Cost.BudgetUSD,
			Model:     model,
			MaxTokens: s.cfg.Provider.MaxTokens,
			Logger:    s.logger,
		})
		if err != nil {
			return "", err
		}

		// Grandchildren clamp against this child's mode.
		ctx = swarm.WithParentMode(ctx, cfg.Mode)
		conv := &engine.Conversation{System: cfg.SystemPrompt}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: cfg.Prompt}}})

		var sb strings.Builder
		runErr := eng.Run(ctx, conv, func(ev engine.Event) {
			if ev.Kind == engine.KindText {
				sb.WriteString(ev.Text)
			}
		})
		return strings.TrimSpace(sb.String()), runErr
	}
}

// newWithDeps assembles a Server from explicit dependencies. It is the seam
// used by tests to inject a mock adapter and an in-memory store.
func newWithDeps(cfg *config.Config, logger *slog.Logger, store *session.Store, adapter provider.Adapter, tools *tool.Registry) *Server {
	s := &Server{cfg: cfg, store: store, adapter: adapter, tools: tools, logger: logger}
	s.http = &http.Server{Addr: cfg.Server.Addr, Handler: s.routes()}
	return s
}

// Handler exposes the HTTP routes for testing with httptest.
func (s *Server) Handler() http.Handler { return s.routes() }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /sessions", s.handleCreateSession)
	mux.HandleFunc("GET /sessions", s.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	mux.HandleFunc("DELETE /sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /sessions/{id}/messages", s.handlePostMessage)
	mux.HandleFunc("GET /teammates", s.handleListTeammates)
	return mux
}

// ListenAndServe runs the daemon until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	defer s.store.Close()
	defer func() {
		for _, c := range s.mcpClients {
			_ = c.Close()
		}
	}()
	// Start the cron scheduler in the background.
	if s.cronSched != nil {
		cronCtx, cronCancel := context.WithCancel(context.Background())
		s.cronCancel = cronCancel
		go s.cronSched.Run(cronCtx)
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("daemon listening", "addr", s.cfg.Server.Addr)
		errCh <- s.http.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.cronCancel != nil {
			s.cronCancel()
		}
		if s.swarm != nil {
			s.swarm.Shutdown(shutdownCtx)
		}
		if s.tasks != nil {
			s.tasks.Shutdown(shutdownCtx)
		}
		if s.sandbox != nil {
			s.sandbox.Close()
		}
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// approver returns the daemon's approval policy for Ask decisions.
func (s *Server) approver() permission.Approver {
	if s.cfg.Permission.AutoApproveExec {
		return permission.AutoApprove{}
	}
	return permission.AutoDeny{}
}

func (s *Server) newEngine(mode string) (*engine.Engine, error) {
	if s.adapter == nil {
		return nil, fmt.Errorf("no model provider configured (set ANTHROPIC_API_KEY and restart the daemon)")
	}
	gate := permission.New(permission.ParseMode(mode), s.approver())
	return engine.New(engine.Options{
		Adapter:   s.adapter,
		Tools:     s.tools,
		Gate:      gate,
		Compactor: s.compactor,
		Hooks:     s.hooks,
		Cost:      cost.NewTracker(),
		BudgetUSD: s.cfg.Cost.BudgetUSD,
		Model:     s.cfg.Provider.Model,
		MaxTokens: s.cfg.Provider.MaxTokens,
		Logger:    s.logger,
	})
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "model": s.cfg.Provider.Model})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req api.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = s.cfg.Permission.Mode
	}
	system := req.System
	if system == "" {
		p, _ := persona.Get(req.Persona)
		system = p.System
	}
	sess, err := s.store.Create(r.Context(), req.Title, system, mode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toMeta(sess.ID, sess.Title, sess.Mode, sess.CreatedAt, sess.UpdatedAt))
}

func (s *Server) handleListTeammates(w http.ResponseWriter, _ *http.Request) {
	out := []api.Teammate{}
	if s.swarmReg != nil {
		for _, m := range s.swarmReg.List() {
			out = append(out, api.Teammate{
				AgentID:   m.Identity.AgentID,
				Name:      m.Identity.Name,
				Team:      m.Identity.Team,
				Status:    string(m.Status),
				Summary:   m.Summary,
				StartedAt: m.StartedAt,
				EndedAt:   m.EndedAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	metas, err := s.store.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]api.SessionMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, toMeta(m.ID, m.Title, m.Mode, m.CreatedAt, m.UpdatedAt))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req api.PostMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	sess, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	send := func(ev api.Event) {
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data)
		flusher.Flush()
	}

	eng, err := s.newEngine(sess.Mode)
	if err != nil {
		send(api.Event{Kind: api.KindError, Error: err.Error()})
		return
	}

	conv := &engine.Conversation{System: s.effectiveSystem(sess.System), Messages: sess.Messages}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: req.Text}}})

	// Carry the session's permission mode so the `agent` tool can clamp any
	// sub-agents it spawns to no more than this posture.
	runCtx := swarm.WithParentMode(r.Context(), sess.Mode)
	runErr := eng.Run(runCtx, conv, func(ev engine.Event) {
		send(toAPIEvent(ev))
	})

	// Persist whatever was produced, even on partial failure.
	if err := s.store.SaveMessages(context.Background(), id, conv.Messages); err != nil {
		s.logger.Error("save messages", "session", id, "err", err)
	}
	if sess.Title == "" {
		_ = s.store.SetTitle(context.Background(), id, deriveTitle(req.Text))
	}
	if runErr != nil {
		s.logger.Warn("run ended with error", "session", id, "err", runErr)
	}
}

// effectiveSystem combines the session's base system prompt with loaded
// project/user memory and skills.
func (s *Server) effectiveSystem(base string) string {
	mem := s.memory.Load()
	switch {
	case base == "" && mem == "":
		return ""
	case mem == "":
		return base
	case base == "":
		return mem
	default:
		return base + "\n\n" + mem
	}
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// --- helpers ---

func toAPIEvent(ev engine.Event) api.Event {
	out := api.Event{
		Kind:        api.EventKind(ev.Kind),
		Text:        ev.Text,
		Tool:        ev.ToolName,
		ToolInput:   ev.ToolInput,
		ToolResult:  ev.ToolResult,
		ToolIsError: ev.ToolIsError,
	}
	if ev.Err != nil {
		out.Error = ev.Err.Error()
	}
	if ev.Usage != nil {
		out.InputTokens = ev.Usage.InputTokens
		out.OutputTokens = ev.Usage.OutputTokens
	}
	out.CostUSD = ev.CostUSD
	return out
}

func toMeta(id, title, mode string, created, updated time.Time) api.SessionMeta {
	return api.SessionMeta{ID: id, Title: title, Mode: mode, CreatedAt: created, UpdatedAt: updated}
}

func deriveTitle(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) > 60 {
		text = text[:60] + "…"
	}
	return text
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}

// cronShellRunner returns a function that runs a cron job's command using the
// given sandbox backend, streaming output to the task buffer via emit.
func cronShellRunner(sb sandbox.Backend, cwd string) func(ctx context.Context, command string, emit func(string)) error {
	return func(ctx context.Context, command string, emit func(string)) error {
		return sb.ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: cwd}, emit)
	}
}
