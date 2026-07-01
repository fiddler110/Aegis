// Package server is the Aegis daemon. It owns the session store, the model
// adapter, the tool registry, and runs the agent engine, exposing everything
// over a local HTTP API (with server-sent events for streaming runs).
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/scottymacleod/aegis/internal/agentdef"
	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/checkpoint"
	"github.com/scottymacleod/aegis/internal/commands"
	"github.com/scottymacleod/aegis/internal/compaction"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/cost"
	"github.com/scottymacleod/aegis/internal/cron"
	"github.com/scottymacleod/aegis/internal/engine"
	"github.com/scottymacleod/aegis/internal/filetracker"
	"github.com/scottymacleod/aegis/internal/guard"
	"github.com/scottymacleod/aegis/internal/hooks"
	"github.com/scottymacleod/aegis/internal/lsp"
	"github.com/scottymacleod/aegis/internal/mcp"
	"github.com/scottymacleod/aegis/internal/memory"
	"github.com/scottymacleod/aegis/internal/permission"
	"github.com/scottymacleod/aegis/internal/persona"
	"github.com/scottymacleod/aegis/internal/plugins"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/providerfactory"
	"github.com/scottymacleod/aegis/internal/repomap"
	"github.com/scottymacleod/aegis/internal/sandbox"
	"github.com/scottymacleod/aegis/internal/session"
	"github.com/scottymacleod/aegis/internal/skills"
	"github.com/scottymacleod/aegis/internal/swarm"
	"github.com/scottymacleod/aegis/internal/task"
	"github.com/scottymacleod/aegis/internal/tool"
	"github.com/scottymacleod/aegis/internal/tool/builtin"
	"github.com/scottymacleod/aegis/internal/trace"
)

const maxRequestBody = 10 << 20 // 10 MiB

// Server holds the daemon's shared state.
type Server struct {
	cfg         *config.Config
	store       *session.Store
	adapter     provider.Adapter
	tools       *tool.Registry
	memory      memory.Sources
	compactor   engine.Compactor
	hooks       engine.Hooks
	mcpClients  []*mcp.Client
	swarm       swarm.Backend
	swarmReg    *swarm.Registry
	tasks       *task.Manager
	cronSched   *cron.Scheduler
	cronCancel  context.CancelFunc
	checkpoints *checkpoint.Store
	fileTracker *filetracker.Tracker
	runs        *runRegistry
	sandbox     sandbox.Backend
	lspMgr      *lsp.Manager
	audit       *hooks.Audit
	cmdReg      *commands.Registry
	permRules   []permission.Rule // parsed text-based allow/deny rules
	repoMap     string            // cached repository map block for the system prompt (empty when not indexed)
	workspace   string
	logger      *slog.Logger
	http        *http.Server
	authToken   string // shared secret for API authentication

	// pendingApprovals maps run ID → chan approvalDecision for interactive approval.
	// The channel is written by handleApprove and read by sseApprover.Approve.
	pendingApprovals sync.Map

	// sessionPermCache maps "sessionID\x00toolName" → struct{} for tools the
	// user has approved with "allow always" during the current daemon lifetime.
	sessionPermCache sync.Map

	// pendingSteers maps session ID → chan string for mid-run steering.
	// The channel is written by handleSteer and drained by the engine between tool rounds.
	pendingSteers sync.Map

	// sessionSems serializes runs within a session. Each session maps to a
	// buffered channel of size 1; acquiring it blocks until the prior run finishes.
	sessionSems sync.Map // string → chan struct{}
}

// approvalDecision carries the client's answer to an interactive approval prompt.
type approvalDecision struct {
	Approved    bool
	AllowAlways bool
}

// sseApprover implements permission.Approver by sending a KindApprovalRequest
// SSE event and blocking until the client POSTs a /sessions/{id}/approve answer.
// The runID is echoed to the client so the approval reply is matched to this
// specific run, preventing a concurrent run on the same session from consuming
// the answer. AllowAlways decisions are stored in permCache so future calls to
// the same tool within the session are auto-approved without prompting.
type sseApprover struct {
	send      func(api.Event)
	ch        <-chan approvalDecision
	runID     string
	sessionID string
	permCache *sync.Map // key: sessionID+"\x00"+toolName → struct{}
}

func (a *sseApprover) Approve(ctx context.Context, toolName, reason string, input json.RawMessage) bool {
	// Check session-scoped allow-always cache before prompting.
	cacheKey := a.sessionID + "\x00" + toolName
	if _, ok := a.permCache.Load(cacheKey); ok {
		return true
	}
	a.send(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           toolName,
		ToolInput:      input,
		ApprovalReason: reason,
		ApprovalID:     a.runID,
	})
	select {
	case d := <-a.ch:
		if d.AllowAlways && d.Approved {
			a.permCache.Store(cacheKey, struct{}{})
		}
		return d.Approved
	case <-ctx.Done():
		return false
	}
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
	adapter, err := providerfactory.Build(cfg, logger)
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

	// Checkpoint store shares the session database connection.
	checkpointStore, err := checkpoint.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("getwd: %w", err)
	}

	// Sandbox backend: "container" forces a runtime (or auto-detects one);
	// "auto" detects and picks the best available, falling back to local;
	// anything else (default) runs commands directly on the host.
	var sb sandbox.Backend
	switch cfg.Sandbox.Backend {
	case "container", "auto":
		opts := sandbox.ContainerOpts{
			Image:    cfg.Sandbox.Image,
			Network:  cfg.Sandbox.Network,
			Priority: sandbox.ParseRuntimes(cfg.Sandbox.Priority),
		}
		// Only "container" honors an explicit forced runtime; "auto" always detects.
		if cfg.Sandbox.Backend == "container" {
			opts.Prefer = sandbox.ContainerRuntime(cfg.Sandbox.Runtime)
		}
		csb, err := sandbox.NewContainerBackend(opts)
		if err != nil {
			logger.Warn("sandbox: no container runtime available, falling back to local",
				"backend", cfg.Sandbox.Backend, "err", err)
			sb = sandbox.NewLocalBackend()
		} else {
			logger.Info("sandbox backend", "runtime", csb.DetectedRuntime(), "image", cfg.Sandbox.Image)
			sb = csb
		}
	default:
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

	// LSP manager: start configured language servers.
	var lspMgr *lsp.Manager
	if len(cfg.LSP) > 0 {
		lspMgr = lsp.NewManager(cwd, logger)
		for _, lc := range cfg.LSP {
			if err := lspMgr.Start(context.Background(), lsp.ServerConfig{
				Name: lc.Name, Command: lc.Command, Args: lc.Args, Extensions: lc.Extensions,
			}); err != nil {
				logger.Warn("lsp server start failed", "name", lc.Name, "err", err)
			}
		}
	}

	reg := tool.NewRegistry()
	ft := filetracker.New()
	todoList := builtin.NewTodoList()
	if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL, Tasks: taskMgr, Cron: cronSched, Sandbox: sb, FileTracker: ft, LSP: lspMgr, TodoList: todoList}); err != nil {
		store.Close()
		return nil, err
	}

	// Register external process tools (plugins).
	if len(cfg.Plugins) > 0 {
		var pluginConfigs []plugins.ProcessToolConfig
		for _, pc := range cfg.Plugins {
			pluginConfigs = append(pluginConfigs, plugins.ProcessToolConfig{
				Name:        pc.Name,
				Description: pc.Description,
				Command:     pc.Command,
				Args:        pc.Args,
				InputSchema: json.RawMessage(pc.InputSchema),
				Capability:  pc.Capability,
				TimeoutSec:  pc.TimeoutSec,
			})
		}
		plugins.RegisterProcessTools(reg, pluginConfigs, logger)
	}

	// Security posture warnings. These are easy to misconfigure in ways that
	// silently weaken isolation, so surface them loudly at startup.
	if _, isLocal := sb.(*sandbox.LocalBackend); isLocal {
		if cfg.Permission.Mode == string(permission.ModeAuto) && !cfg.Permission.AutoApproveExec {
			logger.Warn("permission mode 'auto' with the local sandbox runs model-issued shell commands directly on the host with no approval; use the container sandbox backend or 'build' mode for untrusted work")
		}
		if cfg.Permission.AutoApproveExec {
			logger.Warn("auto_approve_exec is enabled with the local sandbox: every shell command runs on the host without prompting")
		}
	}
	if cfg.Security.EgressThenWrite || len(cfg.Security.NetworkAllowList) > 0 {
		if _, ok := reg.Get("shell"); ok {
			logger.Warn("network security policy (egress_then_write / network_allowlist) does not constrain the shell tool; commands such as curl/wget/nc bypass it — enforce egress with the container sandbox for a hard guarantee")
		}
	}

	s := newWithDeps(cfg, logger, store, adapter, reg)
	// Parse text-based permission rules once at startup. A malformed rule is
	// logged and skipped rather than aborting the daemon.
	if len(cfg.Permission.Rules) > 0 {
		rules, err := permission.ParseRules(cfg.Permission.Rules)
		if err != nil {
			logger.Warn("ignoring invalid permission rules", "err", err)
		} else {
			s.permRules = rules
			logger.Info("loaded permission rules", "count", len(rules))
		}
	}
	s.tasks = taskMgr
	s.cronSched = cronSched
	s.checkpoints = checkpointStore
	s.fileTracker = ft
	s.sandbox = sb
	s.lspMgr = lspMgr
	s.workspace = cwd
	s.memory = memory.NewSources(cwd, cfg.DataDir)
	s.repoMap = loadRepoMap(cwd, logger)

	// Load custom agent definitions from user/project directories.
	if n := agentdef.LoadFromDirs(agentdef.DiscoverDirs(cfg.DataDir, cwd)...); n > 0 {
		logger.Info("loaded custom agent definitions", "count", n)
	}

	// Load custom persona templates from user/project directories.
	if n := persona.LoadFromDirs(persona.DiscoverDirs(cfg.DataDir, cwd)...); n > 0 {
		logger.Info("loaded custom personas", "count", n)
	}

	s.cmdReg = commands.Discover(commands.CommandDirs(cfg.DataDir, cwd)...)

	token, err := generateAndWriteToken(cfg.AuthTokenPath())
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("auth token: %w", err)
	}
	s.authToken = token

	s.audit = hooks.NewAudit(filepath.Join(cfg.DataDir, "audit.jsonl"))
	s.hooks = hooks.NewMulti(s.audit)
	if adapter != nil {
		compModel := cfg.Provider.Model
		if cfg.Provider.SmallModel != "" {
			compModel = cfg.Provider.SmallModel // prefer a fast small model for compaction
		}
		compOpts := compaction.Options{
			Adapter:       adapter,
			Model:         compModel,
			ContextWindow: cfg.Provider.ContextWindow,
		}
		// For local providers without a known context window, skip auto-compaction
		// rather than falling back to the 120k default — cheap local sessions
		// should not be truncated arbitrarily.
		if cfg.Provider.ContextWindow == 0 && cfg.Provider.Default == "ollama" {
			compOpts.MaxBudget = 0 // explicit skip
		}
		s.compactor = compaction.New(compOpts)
	}

	// Connect configured MCP servers and register their tools.
	mcpServers := make([]mcp.ServerConfig, 0, len(cfg.MCP))
	for _, m := range cfg.MCP {
		mcpServers = append(mcpServers, mcp.ServerConfig{Name: m.Name, Command: m.Command, Args: m.Args, Env: m.Env, Auth: m.Auth})
	}
	s.mcpClients = mcp.RegisterServers(context.Background(), reg, mcpServers, logger)

	// Wire sampling so MCP servers can request text generation from the model.
	if adapter != nil {
		samplingFn := buildSamplingHandler(adapter, cfg.Provider.Model, cfg.Provider.MaxTokens, logger)
		for _, cl := range s.mcpClients {
			cl.Sampling = samplingFn
		}
	}

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
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "…"
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

		const maxOutput = 1 << 20 // 1 MiB
		var sb strings.Builder
		runErr := eng.Run(ctx, conv, func(ev engine.Event) {
			if ev.Kind == engine.KindText && sb.Len() < maxOutput {
				sb.WriteString(ev.Text)
			}
		})
		return strings.TrimSpace(sb.String()), runErr
	}
}

// newWithDeps assembles a Server from explicit dependencies. It is the seam
// used by tests to inject a mock adapter and an in-memory store.
func newWithDeps(cfg *config.Config, logger *slog.Logger, store *session.Store, adapter provider.Adapter, tools *tool.Registry) *Server {
	s := &Server{cfg: cfg, store: store, adapter: adapter, tools: tools, logger: logger, runs: newRunRegistry()}
	s.http = &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// WriteTimeout is intentionally omitted: SSE streaming responses are
		// long-lived and a write deadline would abort them prematurely.
	}
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
	mux.HandleFunc("PATCH /sessions/{id}", s.handleUpdateSession)
	mux.HandleFunc("DELETE /sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /sessions/{id}/messages", s.handlePostMessage)
	mux.HandleFunc("POST /sessions/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /sessions/{id}/steer", s.handleSteer)
	mux.HandleFunc("GET /sessions/{id}/checkpoints", s.handleListCheckpoints)
	mux.HandleFunc("POST /sessions/{id}/rewind", s.handleRewind)
	mux.HandleFunc("GET /runs", s.handleListRuns)
	mux.HandleFunc("GET /teammates", s.handleListTeammates)
	mux.HandleFunc("GET /commands", s.handleListCommands)
	mux.HandleFunc("GET /memory", s.handleGetMemory)
	mux.HandleFunc("POST /memory", s.handleAppendMemory)
	mux.HandleFunc("GET /personas", s.handleListPersonas)
	mux.HandleFunc("GET /ui", s.handleWebUI)
	mux.HandleFunc("GET /ui/", s.handleWebUI)
	return s.authMiddleware(s.originMiddleware(mux))
}

// ListenAndServe runs the daemon until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.authToken == "" {
		return fmt.Errorf("server: refusing to start: auth token was not generated")
	}
	defer s.store.Close()
	defer func() {
		if s.audit != nil {
			_ = s.audit.Close()
		}
	}()
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
		if s.lspMgr != nil {
			s.lspMgr.Close()
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

// providerUnconfiguredErr returns a helpful error message that names the
// specific environment variable the user needs to set for their configured
// provider, rather than always blaming ANTHROPIC_API_KEY.
func (s *Server) providerUnconfiguredErr() error {
	switch s.cfg.Provider.Default {
	case "openai":
		if s.cfg.Provider.BaseURL != "" {
			return fmt.Errorf("no model provider configured — run /config to reconfigure or restart the daemon after making changes")
		}
		return fmt.Errorf("no model provider configured (set OPENAI_API_KEY and restart the daemon)")
	default:
		return fmt.Errorf("no model provider configured (set ANTHROPIC_API_KEY and restart the daemon)")
	}
}

// personaModel resolves the effective model for a persona: a config override
// wins, then the persona's own Model, then the global provider model.
func (s *Server) personaModel(p persona.Persona) string {
	if ov, ok := s.cfg.Personas[p.Name]; ok && ov.Model != "" {
		return ov.Model
	}
	if p.Model != "" {
		return p.Model
	}
	return s.cfg.Provider.Model
}

// outputGuardConfig merges the global output-guard default with a persona's
// override into a guard.Config.
func (s *Server) outputGuardConfig(p persona.Persona) guard.Config {
	c := guard.Config{
		Mode:       s.cfg.OutputGuard.Mode,
		Rubric:     s.cfg.OutputGuard.Rubric,
		MaxRetries: s.cfg.OutputGuard.MaxRetries,
	}
	if p.Guard != nil {
		if p.Guard.Disabled {
			return guard.Config{Disabled: true}
		}
		if p.Guard.Mode != "" {
			c.Mode = p.Guard.Mode
		}
		if len(p.Guard.Schema) > 0 {
			c.Schema = p.Guard.Schema
		}
		if p.Guard.Rubric != "" {
			c.Rubric = p.Guard.Rubric
		}
		if p.Guard.MaxRetries > 0 {
			c.MaxRetries = p.Guard.MaxRetries
		}
	}
	return c
}

func (s *Server) newEngine(mode string, approver permission.Approver, steerCh <-chan string, p persona.Persona, guardEnabled bool) (*engine.Engine, error) {
	if s.adapter == nil {
		return nil, s.providerUnconfiguredErr()
	}
	if approver == nil {
		approver = s.approver()
	}
	baseGate := permission.New(permission.ParseMode(mode), approver)

	var gate engine.Gate = baseGate
	engineHooks := s.hooks

	// Wrap with contextual security policies if any are enabled.
	if s.cfg.Security.EgressThenWrite || len(s.cfg.Security.NetworkAllowList) > 0 {
		ctxGate := permission.NewContextualGate(baseGate, permission.ContextualOpts{
			EgressThenWrite:  s.cfg.Security.EgressThenWrite,
			NetworkAllowList: s.cfg.Security.NetworkAllowList,
			Registry:         s.tools,
			OnDecision: func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			},
		})
		gate = ctxGate
		engineHooks = hooks.NewMulti(s.audit, ctxGate)
	}

	// Apply text-based allow/deny rules as the outermost gate so they are
	// evaluated before the contextual and mode gates. An explicit deny always
	// blocks; an explicit allow grants without prompting; otherwise the call
	// falls through to the gate(s) wrapped above.
	rules := s.permRules
	if len(p.Rules) > 0 {
		if pr, err := permission.ParseRules(p.Rules); err == nil {
			rules = append(append([]permission.Rule{}, s.permRules...), pr...)
		} else {
			s.logger.Warn("ignoring invalid persona rules", "persona", p.Name, "err", err)
		}
	}
	if len(rules) > 0 {
		gate = permission.NewRuleGate(gate, rules,
			permission.WithRuleObserver(func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			}))
	}

	var guardFn func(ctx context.Context, text string) (bool, string)
	var guardRetries int
	if guardEnabled {
		guardFn, guardRetries = guard.Resolve(s.outputGuardConfig(p), s.adapter, s.personaModel(p))
	}

	return engine.New(engine.Options{
		Adapter:               s.adapter,
		Tools:                 s.tools,
		Gate:                  gate,
		Compactor:             s.compactor,
		Hooks:                 engineHooks,
		Cost:                  cost.NewTracker(),
		BudgetUSD:             s.cfg.Cost.BudgetUSD,
		Model:                 s.personaModel(p),
		MaxTokens:             s.cfg.Provider.MaxTokens,
		MaxIterations:         s.cfg.Provider.MaxIterations,
		LoopThreshold:         s.cfg.Provider.LoopThreshold,
		ContextWindowTokens:   s.cfg.Provider.ContextWindow,
		SteerChan:             steerCh,
		OutputGuard:           guardFn,
		OutputGuardMaxRetries: guardRetries,
		Logger:                s.logger,
	})
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "model": s.cfg.Provider.Model})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	p, _ := persona.Get(req.Persona)
	mode := req.Mode
	if mode == "" && p.Mode != "" {
		mode = p.Mode
	}
	if mode == "" {
		mode = s.cfg.Permission.Mode
	}
	if mode != "plan" && mode != "build" && mode != "auto" {
		writeError(w, http.StatusBadRequest, "mode must be plan, build, or auto")
		return
	}
	system := req.System
	if system == "" {
		system = p.System
	}
	sess, err := s.store.Create(r.Context(), req.Title, system, mode, req.Persona)
	if err != nil {
		s.logger.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toMeta(session.Meta{ID: sess.ID, Title: sess.Title, Mode: sess.Mode, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt}))
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

// handleListRuns reports message runs currently in flight across all sessions,
// so concurrent user-driven parallel sessions are observable.
func (s *Server) handleListRuns(w http.ResponseWriter, _ *http.Request) {
	out := []api.RunInfo{}
	if s.runs != nil {
		out = s.runs.list()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	metas, err := s.store.List(r.Context())
	if err != nil {
		s.logger.Error("list sessions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.SessionMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, toMeta(m))
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
	id := r.PathValue("id")
	if err := s.store.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if s.checkpoints != nil {
		if err := s.checkpoints.DeleteForSession(r.Context(), id); err != nil {
			s.logger.Warn("delete session checkpoints", "session", id, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.System == nil && req.Mode == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if req.System != nil {
		system := *req.System
		if name, ok := strings.CutPrefix(system, "persona:"); ok {
			p, found := persona.Get(name)
			if !found {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown persona %q", name))
				return
			}
			system = p.System
		}
		if err := s.store.SetSystem(r.Context(), id, system); err != nil {
			s.logger.Error("set system", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if req.Mode != nil {
		m := *req.Mode
		if m != "plan" && m != "build" && m != "auto" {
			writeError(w, http.StatusBadRequest, "mode must be plan, build, or auto")
			return
		}
		if err := s.store.SetMode(r.Context(), id, m); err != nil {
			s.logger.Error("set mode", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	sess, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMeta(session.Meta{ID: sess.ID, Title: sess.Title, Mode: sess.Mode, InputTokens: sess.InputTokens, OutputTokens: sess.OutputTokens, CostUSD: sess.CostUSD, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt}))
}

func (s *Server) handleListCommands(w http.ResponseWriter, _ *http.Request) {
	var out []api.CommandInfo
	if s.cmdReg != nil {
		for _, c := range s.cmdReg.List() {
			out = append(out, api.CommandInfo{Name: c.Name, Description: c.Description, Args: c.Args})
		}
	}
	if out == nil {
		out = []api.CommandInfo{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetMemory(w http.ResponseWriter, _ *http.Request) {
	resp := api.MemoryResponse{
		ProjectMemory: readIfExists(s.memory.ProjectMemoryPath()),
		UserMemory:    readIfExists(s.memory.GlobalMemoryPath()),
	}
	for _, dir := range []string{
		filepath.Join(s.cfg.DataDir, "skills"),
		filepath.Join(s.workspace, ".aegis", "skills"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				resp.Skills = append(resp.Skills, strings.TrimSuffix(e.Name(), ".md"))
			}
		}
	}
	if resp.Skills == nil {
		resp.Skills = []string{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAppendMemory(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.AppendMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Entry) == "" {
		writeError(w, http.StatusBadRequest, "entry is required")
		return
	}
	path := s.memory.ProjectMemoryPath()
	if req.Scope == "user" {
		path = s.memory.GlobalMemoryPath()
	}
	if err := memory.Append(path, req.Entry); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPersonas(w http.ResponseWriter, _ *http.Request) {
	names := persona.Names()
	out := make([]api.PersonaInfo, 0, len(names))
	for _, name := range names {
		p, _ := persona.Get(name)
		out = append(out, api.PersonaInfo{Name: p.Name, Description: p.Description})
	}
	writeJSON(w, http.StatusOK, out)
}

func readIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.PostMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Text) == "" && len(req.Images) == 0 {
		writeError(w, http.StatusBadRequest, "text or images required")
		return
	}

	imageBlocks, err := buildImageBlocks(req.Images)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Serialize runs within the same session: at most one active run at a time.
	// Concurrent requests queue here rather than racing to mutate the session.
	sem := s.sessionSemaphore(id)
	select {
	case sem <- struct{}{}:
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting for active run to finish")
		return
	}
	defer func() { <-sem }()

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

	// All writes to w (events + heartbeat) go through writeMu so the two
	// goroutines never interleave a frame.
	var writeMu sync.Mutex
	send := func(ev api.Event) {
		data, _ := json.Marshal(ev)
		writeMu.Lock()
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data)
		flusher.Flush()
		writeMu.Unlock()
	}

	// Heartbeat: emit an SSE comment periodically so idle long-running tool
	// calls don't get dropped by intermediaries. The goroutine is joined before
	// returning so it never writes to w after the handler exits.
	hbCtx, hbCancel := context.WithCancel(r.Context())
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				writeMu.Lock()
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
				writeMu.Unlock()
			}
		}
	}()
	defer func() { hbCancel(); <-hbDone }()

	// Register a per-run approval channel keyed by a unique run id so a
	// concurrent run on the same session can't consume this run's answer.
	runID := newRunID()
	approvalCh := make(chan approvalDecision, 1)
	s.pendingApprovals.Store(runID, approvalCh)
	defer s.pendingApprovals.Delete(runID)

	// Track this run so concurrent parallel sessions are observable via /runs.
	if s.runs != nil {
		runTitle := sess.Title
		if runTitle == "" {
			runTitle = deriveTitle(req.Text)
		}
		s.runs.start(runID, id, runTitle)
		defer s.runs.finish(runID)
		baseSend := send
		send = func(ev api.Event) {
			s.runs.observe(runID, ev.Kind)
			baseSend(ev)
		}
	}

	var runApprover permission.Approver
	if s.cfg.Permission.AutoApproveExec {
		runApprover = permission.AutoApprove{}
	} else {
		runApprover = &sseApprover{
			send:      send,
			ch:        approvalCh,
			runID:     runID,
			sessionID: id,
			permCache: &s.sessionPermCache,
		}
	}

	// Steer channel: the TUI can POST /sessions/{id}/steer while the run is
	// active to inject a course-correction message between tool rounds.
	steerCh := make(chan string, 8)
	s.pendingSteers.Store(id, steerCh)
	defer s.pendingSteers.Delete(id)

	p, _ := persona.Get(sess.Persona)
	guardEnabled := s.cfg.OutputGuard.Enabled
	if req.GuardEnabled != nil {
		guardEnabled = *req.GuardEnabled
	}

	eng, err := s.newEngine(sess.Mode, runApprover, steerCh, p, guardEnabled)
	if err != nil {
		send(api.Event{Kind: api.KindError, Error: err.Error()})
		return
	}

	conv := &engine.Conversation{System: s.effectiveSystem(sess.System), Messages: sess.Messages}

	// Create a checkpoint for this turn before appending the user message, so a
	// rewind restores the conversation to just before this turn and undoes any
	// file changes the turn makes. seq is the pre-turn message count.
	var snap *checkpoint.Snapshotter
	if s.checkpoints != nil {
		if cp, err := s.checkpoints.Create(context.Background(), id, len(sess.Messages), req.Text); err != nil {
			s.logger.Warn("create checkpoint", "session", id, "err", err)
		} else {
			snap = s.checkpoints.NewSnapshotter(cp.ID)
		}
	}

	content := make([]provider.Block, 0, 1+len(imageBlocks))
	if strings.TrimSpace(req.Text) != "" {
		content = append(content, provider.TextBlock{Text: req.Text})
	}
	content = append(content, imageBlocks...)
	conv.Append(provider.Message{Role: provider.RoleUser, Content: content})

	// Carry the session's permission mode so the `agent` tool can clamp any
	// sub-agents it spawns to no more than this posture.
	runCtx := swarm.WithParentMode(r.Context(), sess.Mode)
	if snap != nil {
		runCtx = checkpoint.WithSnapshotter(runCtx, snap)
	}
	var (
		totalIn   int
		totalOut  int
		totalCost float64
		traces    []trace.TurnTrace
	)
	runErr := eng.Run(runCtx, conv, func(ev engine.Event) {
		// Trace events are server-internal observability records — collect them
		// for persistence but never forward them to the SSE client.
		if ev.Kind == engine.KindTrace {
			if ev.Trace != nil {
				traces = append(traces, *ev.Trace)
			}
			return
		}
		apiEv := toAPIEvent(ev)
		send(apiEv)
		if ev.Kind == engine.KindTurnDone && ev.Usage != nil && !ev.Usage.IsEstimated {
			totalIn += ev.Usage.InputTokens
			totalOut += ev.Usage.OutputTokens
			totalCost += apiEv.CostUSD
		}
	})

	// For non-interrupt aborts (max iterations, cost budget, loop detected) inject
	// a note so the model knows on the next turn what happened and what remains.
	if runErr != nil && !errors.Is(runErr, engine.ErrInterrupted) {
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
			provider.TextBlock{Text: fmt.Sprintf("[System: run aborted — %v. On your next message, summarize what completed and what still needs to be done.]", runErr)},
		}})
	}
	// Persist whatever was produced, even on partial failure.
	if err := s.store.SaveMessages(context.Background(), id, conv.Messages); err != nil {
		s.logger.Error("save messages", "session", id, "err", err)
	}
	if totalIn > 0 || totalOut > 0 {
		_ = s.store.AddUsage(context.Background(), id, totalIn, totalOut, totalCost)
	}
	if len(traces) > 0 {
		if err := s.store.AppendTraces(context.Background(), id, traces); err != nil {
			s.logger.Warn("save traces", "session", id, "err", err)
		}
	}
	if sess.Title == "" {
		go s.generateTitle(id, req.Text)
	}
	if runErr != nil {
		s.logger.Warn("run ended with error", "session", id, "err", runErr)
	}
}

// handleApprove answers a pending interactive approval request. The body must
// be {"approved": bool, "id": "<run id from the approval event>"}. Returns 204
// on success, 404 if no approval is pending for that run id, or 409 if it was
// already answered.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.ApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "approval id is required")
		return
	}
	val, ok := s.pendingApprovals.Load(req.ID)
	if !ok {
		writeError(w, http.StatusNotFound, "no pending approval for run")
		return
	}
	ch := val.(chan approvalDecision)
	select {
	case ch <- approvalDecision{Approved: req.Approved, AllowAlways: req.AllowAlways}:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusConflict, "approval already answered or not yet requested")
	}
}

// handleSteer injects a mid-run instruction into an active session run. The
// text is delivered to the engine between tool rounds via the steer channel;
// if no run is active for the session the request returns 404.
func (s *Server) handleSteer(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.SteerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	val, ok := s.pendingSteers.Load(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no active run for session")
		return
	}
	ch := val.(chan string)
	select {
	case ch <- req.Text:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusTooManyRequests, "steer buffer full; try again momentarily")
	}
}

// handleListCheckpoints returns the rewind points captured for a session, most
// recent first.
func (s *Server) handleListCheckpoints(w http.ResponseWriter, r *http.Request) {
	if s.checkpoints == nil {
		writeJSON(w, http.StatusOK, []api.CheckpointInfo{})
		return
	}
	cps, err := s.checkpoints.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.logger.Error("list checkpoints", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.CheckpointInfo, 0, len(cps))
	for _, cp := range cps {
		out = append(out, api.CheckpointInfo{
			ID:        cp.ID,
			Seq:       cp.Seq,
			Label:     cp.Label,
			FileCount: cp.FileCount,
			CreatedAt: cp.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRewind restores a session to a checkpoint. scope selects what to
// restore: "code" (files only), "conversation" (messages only), or "both"
// (default).
func (s *Server) handleRewind(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if s.checkpoints == nil {
		writeError(w, http.StatusServiceUnavailable, "checkpointing not available")
		return
	}
	id := r.PathValue("id")
	var req api.RewindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.CheckpointID == "" {
		writeError(w, http.StatusBadRequest, "checkpoint_id is required")
		return
	}
	scope := req.Scope
	if scope == "" {
		scope = "both"
	}
	if scope != "both" && scope != "code" && scope != "conversation" {
		writeError(w, http.StatusBadRequest, "scope must be both, code, or conversation")
		return
	}

	cp, err := s.checkpoints.Get(r.Context(), req.CheckpointID)
	if err != nil {
		if errors.Is(err, checkpoint.ErrNotFound) {
			writeError(w, http.StatusNotFound, "checkpoint not found")
			return
		}
		s.logger.Error("get checkpoint", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cp.SessionID != id {
		writeError(w, http.StatusBadRequest, "checkpoint does not belong to this session")
		return
	}

	resp := api.RewindResponse{Scope: scope}

	if scope == "both" || scope == "code" {
		n, err := s.checkpoints.RestoreFiles(r.Context(), cp.ID)
		if err != nil {
			s.logger.Warn("rewind: restore files", "checkpoint", cp.ID, "err", err)
		}
		resp.FilesRestored = n
		// Clear file-staleness tracking: we rewrote files out of band, so the
		// agent must re-read them rather than be blocked by a stale-mtime guard.
		if s.fileTracker != nil {
			s.fileTracker.Clear()
		}
	}

	if scope == "both" || scope == "conversation" {
		sess, err := s.store.Get(r.Context(), id)
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		keep := cp.Seq
		if keep < 0 {
			keep = 0
		}
		if keep > len(sess.Messages) {
			keep = len(sess.Messages)
		}
		if err := s.store.SaveMessages(r.Context(), id, sess.Messages[:keep]); err != nil {
			s.logger.Error("rewind: save truncated messages", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		resp.MessagesKept = keep
	} else if sess, err := s.store.Get(r.Context(), id); err == nil {
		resp.MessagesKept = len(sess.Messages)
	}

	writeJSON(w, http.StatusOK, resp)
}

// newRunID returns a short random identifier for a single message run.
func newRunID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// effectiveSystem combines the session's base system prompt with platform
// context, loaded project/user memory, skills, and context files (AGENTS.md,
// CLAUDE.md).
func (s *Server) effectiveSystem(base string) string {
	var parts []string
	if base != "" {
		parts = append(parts, base)
	}
	parts = append(parts, persona.ToolUseBlock())
	parts = append(parts, persona.CompletingTasksBlock())
	parts = append(parts, persona.PlatformBlock())
	if ctx := s.memory.LoadContext(); ctx != "" {
		parts = append(parts, ctx)
	}
	if mem := s.memory.Load(); mem != "" {
		parts = append(parts, mem)
	}
	if sk := skills.BuildBlock(s.workspace); sk != "" {
		parts = append(parts, sk)
	}
	if s.repoMap != "" {
		parts = append(parts, s.repoMap)
	}
	return strings.Join(parts, "\n\n")
}

// loadRepoMap loads the cached repository map for cwd, rebuilding it when the
// cache is stale (a source file changed since the last `aegis index`). The map
// is opt-in: when no cache exists, this returns an empty string and nothing is
// injected. Returns a ready-to-inject <repo_map> block, or "" on any failure.
func loadRepoMap(cwd string, logger *slog.Logger) string {
	cache := filepath.Join(cwd, ".aegis", "repomap.json")
	rendered, fresh, err := repomap.Load(cwd, cache, repomap.Options{})
	if err != nil || rendered == "" {
		return "" // not indexed, or unreadable cache
	}
	if !fresh {
		// The repo changed since indexing; rebuild so the prompt isn't stale.
		if m, buildErr := repomap.Build(cwd, repomap.Options{}); buildErr == nil {
			if saveErr := m.Save(cache); saveErr != nil {
				logger.Warn("repo map rebuilt but cache not saved", "err", saveErr)
			}
			rendered = m.Render()
		}
	}
	return repomap.Block(rendered)
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.logger.Error("store error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
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
	if ev.Kind == engine.KindGuard {
		out.Text = ev.GuardReason
	}
	if ev.Usage != nil {
		out.InputTokens = ev.Usage.InputTokens
		out.OutputTokens = ev.Usage.OutputTokens
		out.CacheReadTokens = ev.Usage.CacheReadTokens
		out.CacheCreationTokens = ev.Usage.CacheCreationTokens
		out.TokensEstimated = ev.Usage.IsEstimated
	}
	out.CostUSD = ev.CostUSD
	return out
}

func toMeta(m session.Meta) api.SessionMeta {
	return api.SessionMeta{
		ID:           m.ID,
		Title:        m.Title,
		Mode:         m.Mode,
		InputTokens:  m.InputTokens,
		OutputTokens: m.OutputTokens,
		CostUSD:      m.CostUSD,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func deriveTitle(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) > 60 {
		text = string(runes[:60]) + "…"
	}
	return text
}

// generateTitle calls the model asynchronously to produce a short session
// title from the user's first message. Falls back to deriveTitle when no
// SmallModel is configured (avoids a full-model call just for a title).
func (s *Server) generateTitle(sessionID, firstMessage string) {
	model := s.cfg.Provider.SmallModel
	if model == "" || s.adapter == nil {
		// No dedicated small model configured; use the simple truncation fallback.
		_ = s.store.SetTitle(context.Background(), sessionID, deriveTitle(firstMessage))
		return
	}

	prompt := "Give a short title (max 8 words, no punctuation) for a chat that started with:\n" + firstMessage
	req := provider.Request{
		Model:     model,
		MaxTokens: 48,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: prompt}}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := s.adapter.Stream(ctx, req)
	if err != nil {
		_ = s.store.SetTitle(context.Background(), sessionID, deriveTitle(firstMessage))
		return
	}

	var sb strings.Builder
	for ev := range ch {
		if ev.Type == provider.EventTextDelta {
			sb.WriteString(ev.Text)
		}
	}
	title := cleanTitle(strings.TrimSpace(sb.String()))
	if title == "" {
		title = deriveTitle(firstMessage)
	}
	_ = s.store.SetTitle(context.Background(), sessionID, title)
}

// cleanTitle strips thinking tags and trims whitespace from a model-generated title.
func cleanTitle(s string) string {
	// Remove <think>...</think> blocks produced by reasoning models.
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "</think>")
		if end < 0 {
			s = strings.TrimSpace(s[:start])
			break
		}
		s = strings.TrimSpace(s[:start] + s[start+end+len("</think>"):])
	}
	// Collapse internal whitespace and trim surrounding quotes.
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, `"'`)
	runes := []rune(s)
	if len(runes) > 70 {
		s = string(runes[:70]) + "…"
	}
	return s
}

// sessionSemaphore returns the buffered channel used to serialize runs for a
// session (capacity 1 — only one goroutine holds it at a time).
func (s *Server) sessionSemaphore(id string) chan struct{} {
	v, _ := s.sessionSems.LoadOrStore(id, make(chan struct{}, 1))
	return v.(chan struct{})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}

// buildSamplingHandler returns a mcp.SamplingHandler that calls the provider
// adapter to fulfil server-initiated sampling/createMessage requests. The
// response is assembled by collecting all text deltas from the stream.
func buildSamplingHandler(adapter provider.Adapter, model string, maxTokens int, logger *slog.Logger) mcp.SamplingHandler {
	return func(ctx context.Context, req mcp.SamplingRequest) (mcp.SamplingResponse, error) {
		var msgs []provider.Message
		for _, m := range req.Messages {
			role := provider.RoleUser
			if m.Role == "assistant" {
				role = provider.RoleAssistant
			}
			msgs = append(msgs, provider.Message{
				Role:    role,
				Content: []provider.Block{provider.TextBlock{Text: m.Content.Text}},
			})
		}

		mt := maxTokens
		if req.MaxTokens > 0 && req.MaxTokens < mt {
			mt = req.MaxTokens
		}

		stream, err := adapter.Stream(ctx, provider.Request{
			Model:     model,
			System:    req.SystemPrompt,
			Messages:  msgs,
			MaxTokens: mt,
		})
		if err != nil {
			return mcp.SamplingResponse{}, fmt.Errorf("mcp sampling: %w", err)
		}

		var sb strings.Builder
		var stopReason string
		for ev := range stream {
			switch ev.Type {
			case provider.EventTextDelta:
				sb.WriteString(ev.Text)
			case provider.EventDone:
				stopReason = string(ev.Stop)
			case provider.EventError:
				logger.Warn("mcp sampling stream error", "err", ev.Err)
				return mcp.SamplingResponse{}, ev.Err
			}
		}

		return mcp.SamplingResponse{
			Role:       "assistant",
			Content:    mcp.SamplingContent{Type: "text", Text: sb.String()},
			Model:      model,
			StopReason: stopReason,
		}, nil
	}
}

// cronShellRunner returns a function that runs a cron job's command using the
// given sandbox backend, streaming output to the task buffer via emit.
func cronShellRunner(sb sandbox.Backend, cwd string) func(ctx context.Context, command string, emit func(string)) error {
	const cronJobTimeout = 10 * time.Minute
	return func(ctx context.Context, command string, emit func(string)) error {
		ctx, cancel := context.WithTimeout(ctx, cronJobTimeout)
		defer cancel()
		return sb.ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: cwd}, emit)
	}
}

// --- authentication & security middleware ---

// generateAndWriteToken creates a cryptographic random token and writes it to
// path with user-only permissions. The client reads this file to authenticate.
func generateAndWriteToken(path string) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

// authMiddleware checks for a valid Bearer token on all requests except
// /healthz. Requests without a valid token receive 401.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /healthz is public; the web UI page itself is served without a token
		// (a browser navigation can't send one) and injects the token for its
		// own API calls, which remain authenticated.
		if r.URL.Path == "/healthz" || r.URL.Path == "/ui" || strings.HasPrefix(r.URL.Path, "/ui/") {
			next.ServeHTTP(w, r)
			return
		}
		// authToken is always non-empty at startup (ListenAndServe rejects an
		// empty token), but guard defensively to avoid an accidental open-door
		// if the field were ever zero-valued in a test helper.
		if s.authToken == "" {
			writeError(w, http.StatusInternalServerError, "server misconfigured: auth token missing")
			return
		}
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		}
		provided := auth[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.authToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originMiddleware blocks requests with a non-loopback Origin header to
// mitigate DNS rebinding attacks against the local daemon.
func (s *Server) originMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if !isLoopbackOrigin(origin) {
				writeError(w, http.StatusForbidden, "cross-origin request blocked")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackOrigin(origin string) bool {
	host := origin
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host = strings.TrimRight(host, "/")
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	// Strip IPv6 brackets that remain when there is no port (e.g. "[::1]").
	h = strings.Trim(h, "[]")
	ip := net.ParseIP(h)
	return (ip != nil && ip.IsLoopback()) || h == "localhost"
}
