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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/agentdef"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/embed"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/longmem"
	"github.com/fiddler110/aegis/internal/lsp"
	"github.com/fiddler110/aegis/internal/mcp"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/notify"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/plugins"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/fiddler110/aegis/internal/trace"
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
	knowledge   *knowledge.Store // project knowledge base (P3.3); nil when unavailable
	longMem     *longmem.Store   // long-term entity memory (P3.1); nil when unavailable
	runs        *runRegistry
	sandbox     sandbox.Backend
	lspMgr      *lsp.Manager
	audit       *hooks.Audit
	execHook    *hooks.Exec      // user-configured lifecycle hooks (P4.4); nil when none
	notifier    *notify.Notifier // background-session notifications (P5.4); nil when disabled
	cmdReg      *commands.Registry
	permRules   []permission.Rule // parsed text-based allow/deny rules; guarded by permMu
	permMu      sync.Mutex        // protects permRules (approvals add rules at runtime, TQ6)
	repoMap     string            // cached repository map block for the system prompt (empty when not indexed)
	personaDirs []string          // directories rescanned by refreshPersonas for hot reload
	workspace   string
	logger      *slog.Logger
	http        *http.Server
	authToken   string // shared secret for API authentication

	// sandboxFallback and sandboxFallbackReason record whether the configured
	// sandbox backend failed to initialize and the daemon fell back to
	// unsandboxed local execution (P7.4). Surfaced via /healthz so clients can
	// warn the user instead of silently trusting a sandbox that isn't there.
	sandboxFallback       bool
	sandboxFallbackReason string

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

	// sessionTools maps session ID → a *tool.Registry clone of s.tools (P9).
	// tool_search exposes deferred tools by mutating a registry's exposed map;
	// without a per-session clone, that mutation was permanent and
	// process-wide, silently exposing a tool's schema to every other
	// concurrent or future session and persona sharing the daemon's one
	// registry. Lazily created on first use per session and reused across
	// that session's turns so a loaded tool stays loaded turn to turn.
	sessionTools sync.Map // string → *tool.Registry
}

// sessionToolRegistry returns the session-scoped tool registry clone for id,
// creating one from s.tools on first use.
func (s *Server) sessionToolRegistry(id string) *tool.Registry {
	v, _ := s.sessionTools.LoadOrStore(id, s.tools.Clone())
	return v.(*tool.Registry)
}

// approvalDecision carries the client's answer to an interactive approval prompt.
type approvalDecision struct {
	Approved    bool
	AllowAlways bool
	Pattern     string // non-empty: persist "allow tool(pattern)" instead of caching per-tool (TQ6)
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

	// persistRule installs a pattern-scoped "allow tool(pattern)" permission
	// rule when the client answers allow-always with a pattern (TQ6). May be
	// nil (e.g. tests), in which case the per-tool cache is used instead.
	persistRule func(toolName, pattern string)
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
			// A pattern-scoped rule beats the whole-tool cache: approving
			// "npm test*" must not silently approve every future shell call.
			if d.Pattern != "" && a.persistRule != nil {
				a.persistRule(toolName, d.Pattern)
			} else {
				a.permCache.Store(cacheKey, struct{}{})
			}
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
	if err := skills.MaterializeBuiltins(cfg.DataDir); err != nil {
		logger.Warn("failed to materialize built-in skills", "err", err)
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

	sb, sandboxFallback, sandboxFallbackReason, err := SelectSandbox(cfg.Sandbox, cwd, logger)
	if err != nil {
		store.Close()
		return nil, err
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

	// Shared team task list for agent-team coordination (P5.1); reuses the
	// session DB. A failure here is non-fatal — team tools are simply skipped.
	teamTasks, err := swarm.NewTaskList(store.DB())
	if err != nil {
		logger.Warn("swarm: team task list unavailable", "err", err)
		teamTasks = nil
	}

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

	// Semantic recall layer (P5.8): opt-in; nil embedder keeps both stores
	// BM25-only, which is the zero-config default.
	embedder := embed.New(cfg.Embeddings.Enabled, cfg.Embeddings.Provider, cfg.Embeddings.Model, cfg.Embeddings.BaseURL)

	// Project knowledge base (P3.3) and long-term entity memory (P3.1) share
	// the per-user data directory. Failures here are non-fatal — the tools
	// are simply skipped, mirroring the teamTasks pattern above.
	knowledgeStore, err := knowledge.Open(cwd, cfg.KnowledgeDBPath(cwd), embedder)
	if err != nil {
		logger.Warn("knowledge store unavailable", "err", err)
		knowledgeStore = nil
	}
	projectName := filepath.Base(cwd)
	longMemStore, err := longmem.Open(projectName, cfg.LongMemDBPath(), embedder)
	if err != nil {
		logger.Warn("long-term memory store unavailable", "err", err)
		longMemStore = nil
	}

	reg := tool.NewRegistry()
	ft := filetracker.New()
	todoList := builtin.NewTodoList()
	if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL, Tasks: taskMgr, Cron: cronSched, Sandbox: sb, FileTracker: ft, LSP: lspMgr, TodoList: todoList, Search: builtin.SearchOptions{Provider: cfg.Search.Provider, APIKey: cfg.Search.APIKey, BaseURL: cfg.Search.BaseURL}, TeamTasks: teamTasks, MailboxRoot: swarm.MailboxRoot(cfg.DataDir), Knowledge: knowledgeStore, LongMem: longMemStore, BuiltinSkills: cfg.Skills.BuiltinEnabled, SecurityScan: security.OptionsFromConfig(cfg.Security), DASTAllowedTargets: cfg.Security.DAST.AllowedTargets, DASTAllowActive: cfg.Security.DAST.AllowActive}); err != nil {
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
			// P7.7: flag any rule that can never match because the target
			// tool's schema has none of the fields subjectFor knows how to
			// read — otherwise a scoped deny silently never fires.
			permission.WarnUnmatchableRules(rules, reg.All(), func(msg string, args ...any) {
				logger.Warn(msg, args...)
			})
		}
	}
	s.tasks = taskMgr
	s.cronSched = cronSched
	s.checkpoints = checkpointStore
	s.fileTracker = ft
	s.sandbox = sb
	s.sandboxFallback = sandboxFallback
	s.sandboxFallbackReason = sandboxFallbackReason
	s.lspMgr = lspMgr
	s.knowledge = knowledgeStore
	s.longMem = longMemStore
	s.workspace = cwd
	s.memory = memory.NewSources(cwd, cfg.DataDir)
	s.repoMap = loadRepoMap(cwd, logger)

	// Load custom agent definitions from user/project directories.
	if n := agentdef.LoadFromDirs(agentdef.DiscoverDirs(cfg.DataDir, cwd)...); n > 0 {
		logger.Info("loaded custom agent definitions", "count", n)
	}

	// Load custom persona templates from user/project directories. Refresh
	// (rather than LoadFromDirs) primes the change-detection signature so
	// later refreshPersonas calls are cheap no-ops until a file changes.
	s.personaDirs = persona.DiscoverDirs(cfg.DataDir, cwd)
	if n, _ := persona.Refresh(s.personaDirs...); n > 0 {
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
	s.notifier = notify.New(cfg.Notify.Desktop, cfg.Notify.Webhook, logger)
	s.execHook = hooks.NewExec(toExecSpecs(cfg.Hooks), logger)
	if s.execHook != nil {
		s.hooks = hooks.NewMulti(s.audit, s.execHook)
	} else {
		s.hooks = hooks.NewMulti(s.audit)
	}
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
		mcpServers = append(mcpServers, mcp.ServerConfig{
			Name:             m.Name,
			Command:          m.Command,
			Args:             m.Args,
			Env:              m.Env,
			Auth:             m.Auth,
			Capability:       m.Capability,
			ToolCapabilities: m.ToolCapabilities,
		})
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
	if err := reg.Register(builtin.NewAgentTool(s.swarm, s.tasks, builtin.WithCostCaps(cfg.Cost.BudgetUSD, cfg.Cost.MaxTokensPerRun))); err != nil {
		store.Close()
		return nil, err
	}

	return s, nil
}

// SelectSandbox picks the command-execution backend per cfg.Backend:
// "container" forces a runtime (or auto-detects one); "auto" detects and
// picks the best available; "os" uses OS-level isolation without a container
// runtime; anything else (default) runs commands directly on the host.
//
// A fallback to the unsandboxed local backend is a silent security downgrade
// for an operator who believes sandboxing is active (P7.4): it is always
// logged, reported back via the fallback/reason return values (surfaced by
// the caller via /healthz for clients to warn the user), and — when
// cfg.Strict is set — turned into a hard error instead of a silent fallback.
//
// Exported (not just server-internal) so the subprocess swarm worker
// (internal/cli/worker.go) can reconstruct the same sandbox backend the
// daemon selected instead of running its shell tool unsandboxed (P10.2).
func SelectSandbox(cfg config.SandboxConfig, cwd string, logger *slog.Logger) (sb sandbox.Backend, fallback bool, reason string, err error) {
	switch cfg.Backend {
	case "container", "auto":
		opts := sandbox.ContainerOpts{
			Image:    cfg.Image,
			Network:  cfg.Network,
			Priority: sandbox.ParseRuntimes(cfg.Priority),
		}
		// Only "container" honors an explicit forced runtime; "auto" always detects.
		if cfg.Backend == "container" {
			opts.Prefer = sandbox.ContainerRuntime(cfg.Runtime)
		}
		csb, cerr := sandbox.NewContainerBackend(opts)
		if cerr != nil {
			if cfg.Strict {
				return nil, false, "", fmt.Errorf("sandbox: no container runtime available for backend %q and sandbox.strict is set: %w", cfg.Backend, cerr)
			}
			logger.Warn("sandbox: no container runtime available, falling back to local",
				"backend", cfg.Backend, "err", cerr)
			reason = fmt.Sprintf("configured sandbox backend %q unavailable (%v) — running unsandboxed on the host", cfg.Backend, cerr)
			return sandbox.NewLocalBackendWithEnv(cfg.StripEnv), true, reason, nil
		}
		logger.Info("sandbox backend", "runtime", csb.DetectedRuntime(), "image", cfg.Image)
		return csb, false, "", nil
	case "os":
		// OS-level isolation without a container runtime (P4.7): seatbelt on
		// macOS, bwrap on Linux. Falls back to local when unavailable.
		osb, oerr := sandbox.NewOSBackend(cwd, cfg.Network, cfg.StripEnv)
		if oerr != nil {
			if cfg.Strict {
				return nil, false, "", fmt.Errorf("sandbox: OS sandbox unavailable and sandbox.strict is set: %w", oerr)
			}
			logger.Warn("sandbox: OS sandbox unavailable, falling back to local", "err", oerr)
			reason = fmt.Sprintf("configured sandbox backend \"os\" unavailable (%v) — running unsandboxed on the host", oerr)
			return sandbox.NewLocalBackendWithEnv(cfg.StripEnv), true, reason, nil
		}
		logger.Info("sandbox backend", "mechanism", osb.Name(), "network", cfg.Network)
		return osb, false, "", nil
	default:
		return sandbox.NewLocalBackendWithEnv(cfg.StripEnv), false, "", nil
	}
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
	if s.execHook != nil {
		s.execHook.SubagentStop(context.Background(), id.AgentID, status, truncateSummary(summary, 200), res.Failed())
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
			return swarm.NewSubprocessBackend(exe, "__worker", s.swarmReg, mailboxRoot, s.cfg.SessionDBPath(), s.cfg.Cost.BudgetUSD, s.cfg.Cost.MaxTokensPerRun)
		}
		s.logger.Warn("cannot resolve executable path; falling back to in-process swarm backend")
	}
	return swarm.NewInProcessBackend(s.subAgentRunner(), s.swarmReg, mailboxRoot)
}

// subAgentRunner returns a swarm.RunFunc that executes a teammate by building a
// sub-engine over the daemon's shared adapter and tools. The child runs with its
// own (clamped) permission mode. Its cost tracker is the same shared ledger the
// top-level session run attached to ctx (D1): every sub-agent in a fan-out
// tree, at any depth, draws against one BudgetUSD ceiling instead of each spawn
// getting a fresh allowance — a background/detached spawn whose context was
// severed from the request falls back to a fresh tracker since there's no
// ledger left to share.
func (s *Server) subAgentRunner() swarm.RunFunc {
	return func(ctx context.Context, cfg swarm.SpawnConfig) (string, error) {
		if s.adapter == nil {
			return "", fmt.Errorf("no model provider configured")
		}
		model := cfg.Model
		if model == "" {
			model = s.cfg.Provider.Model
		}
		tracker, _ := swarm.CostTrackerFromContext(ctx).(*cost.Tracker)
		if tracker == nil {
			tracker = cost.NewTracker()
		}
		// Sub-agents get the same gate stack a top-level run does (contextual
		// egress/network policy, text allow/deny rules) — only the mode gate
		// was ever meaningfully child-specific, and clampMode already confines
		// that above the swarm layer. A bare mode gate here let a spawned
		// teammate route straight around an operator's egress-then-write or
		// deny rule (P10.1).
		gate, engineHooks := s.buildGate(cfg.Mode, s.approver(), persona.Persona{})
		eng, err := engine.New(engine.Options{
			Adapter:         s.adapter,
			Tools:           s.tools,
			Gate:            gate,
			Compactor:       s.compactor,
			Hooks:           engineHooks,
			Cost:            tracker,
			BudgetUSD:       s.cfg.Cost.BudgetUSD,
			MaxTokensPerRun: s.cfg.Cost.MaxTokensPerRun,
			Model:           model,
			MaxTokens:       s.cfg.Provider.MaxTokens,
			Logger:          s.logger,
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
	mux.HandleFunc("POST /sessions/{id}/background", s.handleSetBackground) // P3.2
	mux.HandleFunc("GET /sessions/{id}/events", s.handleGetBGEvents)        // P3.2
	mux.HandleFunc("POST /sessions/{id}/archive", s.handleArchiveSession)
	mux.HandleFunc("POST /sessions/{id}/unarchive", s.handleUnarchiveSession)
	mux.HandleFunc("POST /sessions/prune", s.handlePruneSessions)
	mux.HandleFunc("GET /runs", s.handleListRuns)
	mux.HandleFunc("GET /teammates", s.handleListTeammates)
	mux.HandleFunc("GET /commands", s.handleListCommands)
	mux.HandleFunc("GET /memory", s.handleGetMemory)
	mux.HandleFunc("POST /memory", s.handleAppendMemory)
	mux.HandleFunc("GET /personas", s.handleListPersonas)
	mux.HandleFunc("POST /security/scan", s.handleScan)
	mux.HandleFunc("POST /debate", s.handleDebate)
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
		if s.knowledge != nil {
			_ = s.knowledge.Close()
		}
		if s.longMem != nil {
			_ = s.longMem.Close()
		}
	}()
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

	// Start the session auto-pruner when a TTL is configured.
	if s.cfg.Cleanup.SessionTTLDays > 0 {
		interval := 24 * time.Hour
		if h := s.cfg.Cleanup.IntervalHours; h > 0 {
			interval = time.Duration(h) * time.Hour
		}
		ttl := time.Duration(s.cfg.Cleanup.SessionTTLDays) * 24 * time.Hour
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					n, err := s.store.Prune(context.Background(), ttl)
					if err != nil {
						s.logger.Error("auto-prune sessions", "err", err)
					} else if n > 0 {
						s.logger.Info("auto-pruned old sessions", "deleted", n, "ttl_days", s.cfg.Cleanup.SessionTTLDays)
					}
				}
			}
		}()
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
			// A loaded (non-built-in) persona is untrusted content (P7.5),
			// the same as its Mode and Rules fields: honoring "output_guard:
			// none" unconditionally would let a project-level persona.md
			// silently switch off the last safety net with no warning
			// surfaced anywhere. Built-in personas are reviewed and shipped
			// with Aegis, so they remain trusted to disable the guard.
			if p.Loaded {
				s.logger.Warn("ignoring output_guard: none from untrusted (loaded) persona", "persona", p.Name)
			} else {
				return guard.Config{Disabled: true}
			}
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

// buildGate assembles the shared permission gate stack — base mode gate →
// contextual egress/network policy → text allow/deny rules → persona tool
// advisory (outermost) — used by every engine run the daemon starts, top-level
// or sub-agent, so a spawned teammate can't bypass an operator's security
// posture just because it took a different code path to get an engine (P10.1).
// Mode clamping happens above this call (resolveSessionMode / clampMode); an
// empty persona.Persona{} skips the persona-specific layers (rules/tools),
// which is what sub-agent runs pass since they have no persona of their own.
func (s *Server) buildGate(mode string, approver permission.Approver, p persona.Persona) (engine.Gate, engine.Hooks) {
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
	s.permMu.Lock()
	rules := append([]permission.Rule{}, s.permRules...)
	s.permMu.Unlock()
	if len(p.Rules) > 0 {
		if pr, err := permission.ParseRules(p.Rules); err == nil {
			rules = append(rules, filterPersonaRules(pr, p, s.logger)...)
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

	// A persona's declared Tools list is advisory only (P7.5: never a
	// security boundary) — wrapped outermost so a call outside the list is
	// flagged before the real allow/deny rules below run.
	if len(p.Tools) > 0 {
		gate = permission.NewPersonaToolGate(gate, p.Name, p.Tools, approver, s.logger,
			func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			})
	}

	return gate, engineHooks
}

func (s *Server) newEngine(mode string, approver permission.Approver, steerCh <-chan string, p persona.Persona, guardEnabled bool, tracker *cost.Tracker, tools *tool.Registry) (*engine.Engine, error) {
	if s.adapter == nil {
		return nil, s.providerUnconfiguredErr()
	}
	if tools == nil {
		tools = s.tools
	}
	if approver == nil {
		approver = s.approver()
	}
	gate, engineHooks := s.buildGate(mode, approver, p)

	var guardFn guard.Func
	var guardRetries int
	if guardEnabled {
		guardFn, guardRetries = guard.Resolve(s.outputGuardConfig(p), s.adapter, s.personaModel(p))
	}

	if tracker == nil {
		tracker = cost.NewTracker()
	}
	return engine.New(engine.Options{
		Adapter:               s.adapter,
		Tools:                 tools,
		Gate:                  gate,
		Compactor:             s.compactor,
		Hooks:                 engineHooks,
		Cost:                  tracker,
		BudgetUSD:             s.cfg.Cost.BudgetUSD,
		MaxTokensPerRun:       s.cfg.Cost.MaxTokensPerRun,
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
	resp := api.HealthStatus{
		Status:                "ok",
		Model:                 s.cfg.Provider.Model,
		SandboxFallback:       s.sandboxFallback,
		SandboxFallbackReason: s.sandboxFallbackReason,
	}
	writeJSON(w, http.StatusOK, resp)
}

// permModeRank orders permission modes by permissiveness for the P7.5 persona
// escalation guard: plan < build < auto. An unrecognized mode ranks as plan
// (least permissive) so it can never be treated as an escalation.
func permModeRank(mode string) int {
	switch mode {
	case "build":
		return 1
	case "auto":
		return 2
	default:
		return 0
	}
}

// resolveSessionMode picks the permission mode for a new session: an explicit
// reqMode always wins; otherwise a persona's Mode is used, except a loaded
// (non-built-in) persona — including one installed by a bundle (P5.7) or
// picked up from .aegis/personas/*.md — is less trusted than a built-in, so
// its Mode must not silently escalate a session past the configured default
// when the caller didn't explicitly ask for a mode (P7.5). Built-in personas
// are reviewed and shipped with Aegis, so they remain fully trusted. Returns
// "" when neither reqMode nor an applicable persona mode is set, leaving the
// caller to apply the configured default.
func (s *Server) resolveSessionMode(reqMode string, p persona.Persona) string {
	if reqMode != "" {
		return reqMode
	}
	if p.Mode == "" {
		return ""
	}
	if p.Loaded && permModeRank(p.Mode) > permModeRank(s.cfg.Permission.Mode) {
		s.logger.Warn("persona requested a more permissive mode than the configured default; ignoring",
			"persona", p.Name, "persona_mode", p.Mode, "default_mode", s.cfg.Permission.Mode)
		return ""
	}
	return p.Mode
}

// filterPersonaRules strips Allow rules contributed by a loaded (non-built-in)
// persona before they are merged into a session's rule set. A loaded persona
// is untrusted content (P7.5) in exactly the same way its Mode field is: an
// Allow rule short-circuits both the mode gate and the approver (RuleGate.Check),
// so an unfiltered "allow shell(*)" in a project-level persona.md would grant
// unattended access regardless of the configured plan/build/auto mode — a
// strictly bigger hole than the Mode escalation resolveSessionMode already
// blocks, since it bypasses mode entirely rather than just requesting a more
// permissive one. Deny rules only narrow access, so they carry none of that
// risk and pass through unchanged. Built-in personas (Loaded == false) are
// reviewed and shipped with Aegis, so their rules remain fully trusted.
func filterPersonaRules(rules []permission.Rule, p persona.Persona, logger *slog.Logger) []permission.Rule {
	if !p.Loaded {
		return rules
	}
	kept := make([]permission.Rule, 0, len(rules))
	for _, r := range rules {
		if r.Action == permission.RuleDeny {
			kept = append(kept, r)
			continue
		}
		if logger != nil {
			logger.Warn("ignoring persona allow rule from untrusted (loaded) persona", "persona", p.Name)
		}
	}
	return kept
}

// refreshPersonas rescans the persona directories so file edits, additions,
// and deletions take effect without a daemon restart. A directory signature
// makes this a cheap no-op when nothing changed, so persona-touching handlers
// call it on every request.
func (s *Server) refreshPersonas() {
	if n, changed := persona.Refresh(s.personaDirs...); changed {
		s.logger.Info("reloaded persona files", "count", n)
	}
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.refreshPersonas()
	p, _ := persona.Get(req.Persona)
	mode := s.resolveSessionMode(req.Mode, p)
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
	var (
		metas []session.Meta
		err   error
	)
	if r.URL.Query().Get("archived") == "true" {
		metas, err = s.store.ListAll(r.Context())
	} else {
		metas, err = s.store.List(r.Context())
	}
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
	s.sessionTools.Delete(id)
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
	if req.System == nil && req.Mode == nil && req.Persona == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}

	// A persona switch can arrive as the Persona field or as the legacy
	// "persona:<name>" System prefix; both take the same full-profile path so
	// the switch carries model, rules, and guard overrides — not just the
	// system prompt.
	personaName := ""
	if req.Persona != nil {
		personaName = strings.TrimSpace(*req.Persona)
		if personaName == "" {
			writeError(w, http.StatusBadRequest, "persona name is required")
			return
		}
	}
	if req.System != nil {
		if name, ok := strings.CutPrefix(*req.System, "persona:"); ok {
			personaName = name
			req.System = nil
		}
	}
	if personaName != "" {
		s.refreshPersonas()
		p, found := persona.Get(personaName)
		if !found {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown persona %q", personaName))
			return
		}
		if err := s.store.SetPersona(r.Context(), id, p.Name); err != nil {
			s.logger.Error("set persona", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if req.System == nil {
			req.System = &p.System
		}
		// Apply the persona's permission mode (subject to the P7.5 escalation
		// guard) unless the request pins a mode explicitly.
		if req.Mode == nil {
			if m := s.resolveSessionMode("", p); m != "" {
				req.Mode = &m
			}
		}
	}

	if req.System != nil {
		if err := s.store.SetSystem(r.Context(), id, *req.System); err != nil {
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
	for _, sk := range skills.Discover(s.workspace, s.cfg.DataDir, s.cfg.Skills.BuiltinEnabled) {
		resp.Skills = append(resp.Skills, sk.Name)
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
	s.refreshPersonas()
	names := persona.Names()
	out := make([]api.PersonaInfo, 0, len(names))
	for _, name := range names {
		p, _ := persona.Get(name)
		out = append(out, api.PersonaInfo{Name: p.Name, Description: p.Description})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleScan runs the security scanners directly against the daemon's own
// workspace and returns the formatted report — the same underlying scan the
// security_scan tool runs, exposed so `/scan` in the TUI (and other direct
// callers) gets a deterministic report without spending a model turn. Not
// tied to any session; a scan isn't conversation state.
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.ScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	opts := security.OptionsFromConfig(s.cfg.Security)

	if req.Image != "" {
		report := security.ScanImage(r.Context(), req.Image, security.DefaultImageScanners(), opts)
		writeJSON(w, http.StatusOK, api.ScanResponse{Report: report.Format()})
		return
	}

	dir := s.workspace
	if req.Path != "" {
		resolved, err := sandbox.ValidatePath(s.workspace, req.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		dir = resolved
	}

	if req.SBOM {
		sbom, method, err := security.GenerateSBOM(r.Context(), dir, opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		security.WriteSBOMArtifact(dir, sbom)
		writeJSON(w, http.StatusOK, api.ScanResponse{
			Report: fmt.Sprintf("SBOM generated via %s and written to %s (%d bytes)", method, security.SBOMArtifactPath(dir), len(sbom)),
		})
		return
	}

	report := security.RunWithOptions(r.Context(), dir, security.DefaultScanners(), opts)
	writeJSON(w, http.StatusOK, api.ScanResponse{Report: report.Format()})
}

// handleDebate runs a multi-agent debate (P12) directly against the daemon's
// configured model, independent of any session — the same underlying
// mechanism the `agent` tool's debate mode runs, exposed so `/debate` in the
// TUI (and `aegis debate`) can adversarially review a claim without first
// spending a conversational turn to produce it. Unlike /security/scan, this
// does spend model turns (one per role per round) since debate is inherently
// model-driven; there is no scanner-only equivalent.
func (s *Server) handleDebate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.DebateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Claim == "" {
		writeError(w, http.StatusBadRequest, "claim is required")
		return
	}
	if s.adapter == nil {
		writeError(w, http.StatusServiceUnavailable, "no model provider configured")
		return
	}

	tracker := cost.NewTracker()
	cfg := debate.Config{
		ProposerPersona: req.ProposerPersona,
		CriticPersona:   req.CriticPersona,
		ArbiterPersona:  req.ArbiterPersona,
		MaxRounds:       req.MaxRounds,
		Tracker:         tracker,
		BudgetUSD:       s.cfg.Cost.BudgetUSD,
		MaxTokens:       s.cfg.Cost.MaxTokensPerRun,
	}
	transcript, err := debate.Run(r.Context(), req.Claim, cfg, s.debateRoleRunner(tracker))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.DebateResponse{
		Report:     transcript.Format(),
		Verdict:    transcript.Verdict.Outcome,
		Confidence: transcript.Verdict.Confidence,
	})
}

// debateRoleRunner returns a debate.RunFunc that executes one role turn as a
// bare, session-less engine call over the daemon's shared adapter and tools —
// the same construction subAgentRunner uses for a spawned teammate, without
// the swarm identity/mailbox machinery a direct (non-session) debate call has
// no use for. Every role call shares tracker, so debate.Run's budget check
// (P12.6) sees the run's true cumulative spend across rounds.
func (s *Server) debateRoleRunner(tracker *cost.Tracker) debate.RunFunc {
	return func(ctx context.Context, systemPrompt, prompt string) (string, error) {
		gate, engineHooks := s.buildGate("build", s.approver(), persona.Persona{})
		eng, err := engine.New(engine.Options{
			Adapter:         s.adapter,
			Tools:           s.tools,
			Gate:            gate,
			Compactor:       s.compactor,
			Hooks:           engineHooks,
			Cost:            tracker,
			BudgetUSD:       s.cfg.Cost.BudgetUSD,
			MaxTokensPerRun: s.cfg.Cost.MaxTokensPerRun,
			Model:           s.cfg.Provider.Model,
			MaxTokens:       s.cfg.Provider.MaxTokens,
			Logger:          s.logger,
		})
		if err != nil {
			return "", err
		}
		conv := &engine.Conversation{System: systemPrompt}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: prompt}}})

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

	var dailyCostBefore float64
	if cap := s.cfg.Cost.SessionCapUSD; cap > 0 && sess.CostUSD >= cap {
		writeError(w, http.StatusPaymentRequired, fmt.Sprintf("session spend cap reached: $%.4f of $%.2f limit", sess.CostUSD, cap))
		return
	}
	if cap := s.cfg.Cost.DailyCapUSD; cap > 0 {
		dailyCostBefore, err = s.store.TodayCost(r.Context())
		if err != nil {
			s.logger.Warn("read daily cost", "err", err)
		}
		if dailyCostBefore >= cap {
			writeError(w, http.StatusPaymentRequired, fmt.Sprintf("daily spend cap reached: $%.4f of $%.2f limit", dailyCostBefore, cap))
			return
		}
	}
	sessionTokensBefore := sess.InputTokens + sess.OutputTokens
	if cap := s.cfg.Cost.SessionTokenCap; cap > 0 && sessionTokensBefore >= cap {
		writeError(w, http.StatusPaymentRequired, fmt.Sprintf("session token cap reached: %d of %d limit", sessionTokensBefore, cap))
		return
	}
	var dailyTokensBefore int
	if cap := s.cfg.Cost.DailyTokenCap; cap > 0 {
		dailyTokensBefore, err = s.store.TodayTokens(r.Context())
		if err != nil {
			s.logger.Warn("read daily tokens", "err", err)
		}
		if dailyTokensBefore >= cap {
			writeError(w, http.StatusPaymentRequired, fmt.Sprintf("daily token cap reached: %d of %d limit", dailyTokensBefore, cap))
			return
		}
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
			send:        send,
			ch:          approvalCh,
			runID:       runID,
			sessionID:   id,
			permCache:   &s.sessionPermCache,
			persistRule: s.addPermissionRule,
		}
	}

	// Steer channel: the TUI can POST /sessions/{id}/steer while the run is
	// active to inject a course-correction message between tool rounds.
	steerCh := make(chan string, 8)
	s.pendingSteers.Store(id, steerCh)
	defer s.pendingSteers.Delete(id)

	s.refreshPersonas() // pick up persona file edits without a daemon restart
	p, _ := persona.Get(sess.Persona)
	guardEnabled := s.cfg.OutputGuard.Enabled
	if req.GuardEnabled != nil {
		guardEnabled = *req.GuardEnabled
	}

	// Shared across this engine and every sub-agent it spawns (D1): embedding
	// the same tracker in runCtx below means fan-out draws from one ledger
	// instead of each spawned agent getting its own fresh BudgetUSD allowance.
	tracker := cost.NewTracker()
	// Session-scoped tool registry clone (P9): tool_search loads a deferred
	// tool onto this session's own exposure state, not the daemon-wide
	// registry every other session and persona shares.
	sessionTools := s.sessionToolRegistry(id)
	eng, err := s.newEngine(sess.Mode, runApprover, steerCh, p, guardEnabled, tracker, sessionTools)
	if err != nil {
		send(api.Event{Kind: api.KindError, Error: err.Error()})
		return
	}

	conv := &engine.Conversation{System: s.effectiveSystem(sess.System), Messages: sess.Messages, Persisted: len(sess.Messages)}

	// P3.2: background (detached) sessions run the engine in a goroutine bound to
	// a server-level context so the turn continues even after the HTTP client
	// disconnects. All events are buffered to SQLite in addition to being sent
	// over SSE while the client is still connected.
	if sess.Background {
		origSend := send
		send = func(ev api.Event) {
			origSend(ev) // best-effort SSE while client is connected
			if data, jerr := json.Marshal(ev); jerr == nil {
				_ = s.store.AppendBGEvent(context.Background(), id, string(data))
			}
		}
	}

	// Create a checkpoint for this turn before appending the user message, so a
	// rewind restores the conversation to just before this turn and undoes any
	// file changes the turn makes. seq is the pre-turn message count.
	var snap *checkpoint.Snapshotter
	if s.checkpoints != nil {
		if cp, err := s.checkpoints.Create(context.Background(), id, len(sess.Messages), req.Text); err != nil {
			s.logger.Warn("create checkpoint", "session", id, "err", err)
		} else {
			snap = s.checkpoints.NewSnapshotter(cp.ID)
			// P3.4: capture the HEAD commit SHA asynchronously so rollback can reset to it.
			go func(cpID string) {
				if sha := captureGitSHA(context.Background(), s.workspace); sha != "" {
					if err := s.checkpoints.SetGitSHA(context.Background(), cpID, sha); err != nil {
						s.logger.Warn("set checkpoint git sha", "checkpoint", cpID, "err", err)
					}
				}
			}(cp.ID)
		}
	}

	content := make([]provider.Block, 0, 1+len(imageBlocks))
	if strings.TrimSpace(req.Text) != "" {
		// P5.5: expand @path#L10-40 file mentions to inline file excerpts.
		content = append(content, provider.TextBlock{Text: expandFileMentions(req.Text, s.workspace)})
	}
	content = append(content, imageBlocks...)
	conv.Append(provider.Message{Role: provider.RoleUser, Content: content})

	// Carry the session's permission mode so the `agent` tool can clamp any
	// sub-agents it spawns to no more than this posture. P3.2: background sessions
	// use a server-level context so the run continues after the HTTP client drops.
	baseRunCtx := r.Context()
	if sess.Background {
		baseRunCtx = context.Background()
	}
	runCtx := swarm.WithParentMode(baseRunCtx, sess.Mode)
	runCtx = swarm.WithCostTracker(runCtx, tracker)
	if snap != nil {
		runCtx = checkpoint.WithSnapshotter(runCtx, snap)
	}
	if s.execHook != nil {
		s.execHook.SessionStart(runCtx, id)
		defer s.execHook.Stop(context.Background(), id)
	}
	var (
		totalIn   int
		totalOut  int
		totalCost float64
		traces    []trace.TurnTrace
	)
	// flushMessages durably saves whatever of conv.Messages hasn't been saved
	// yet. It's called after every tool round (not just once at the very end)
	// so a crash mid-run loses at most the current turn's in-flight model
	// call, not the whole turn's transcript — tool side effects (files
	// written, shell commands executed) already happened on disk by the time
	// their result messages are appended, so leaving them unpersisted until
	// eng.Run fully returns let history desync from real repo state with no
	// record of what actually ran. Safe to call from the emit callback: it
	// runs on the same goroutine as eng.Run, synchronously between conv
	// mutations, never concurrently with them.
	flushMessages := func() {
		if conv.Persisted < 0 {
			if err := s.store.SaveMessages(context.Background(), id, conv.Messages); err != nil {
				s.logger.Error("save messages", "session", id, "err", err)
				return
			}
			conv.Persisted = len(conv.Messages)
			return
		}
		if conv.Persisted >= len(conv.Messages) {
			return
		}
		if err := s.store.AppendMessages(context.Background(), id, conv.Messages[conv.Persisted:]); err != nil {
			s.logger.Error("append messages", "session", id, "err", err)
			return
		}
		conv.Persisted = len(conv.Messages)
	}
	runErr := eng.Run(runCtx, conv, func(ev engine.Event) {
		// Trace events are server-internal observability records — collect them
		// for persistence but never forward them to the SSE client.
		if ev.Kind == engine.KindTrace {
			if ev.Trace != nil {
				traces = append(traces, *ev.Trace)
			}
			flushMessages()
			return
		}
		apiEv := toAPIEvent(ev)
		send(apiEv)
		if ev.Kind == engine.KindTurnDone {
			flushMessages()
			if ev.Usage != nil {
				// Tokens count even for estimated usage (local/Ollama models
				// report no real usage) — only the dollar figure is skipped
				// for those, since pricing an estimate would be misleading
				// (P10.5). Before this fix, AddUsage/AddDailyTokens never saw
				// a local model's turns at all, so session/daily token caps
				// had nothing to check against.
				totalIn += ev.Usage.InputTokens
				totalOut += ev.Usage.OutputTokens
				if !ev.Usage.IsEstimated {
					totalCost += apiEv.CostUSD
				}
			}
		}
	})

	// For non-interrupt aborts (max iterations, cost budget, loop detected) inject
	// a note so the model knows on the next turn what happened and what remains.
	if runErr != nil && !errors.Is(runErr, engine.ErrInterrupted) {
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
			provider.TextBlock{Text: fmt.Sprintf("[System: run aborted — %v. On your next message, summarize what completed and what still needs to be done.]", runErr)},
		}})
	}
	// Final flush: covers the abort note just appended above, plus anything
	// from the last turn the incremental flushes above hadn't caught yet.
	flushMessages()
	if totalIn > 0 || totalOut > 0 {
		_ = s.store.AddUsage(context.Background(), id, totalIn, totalOut, totalCost)
	}
	if totalCost > 0 && s.cfg.Cost.DailyCapUSD > 0 {
		if err := s.store.AddDailyCost(context.Background(), totalCost); err != nil {
			s.logger.Warn("add daily cost", "err", err)
		}
	}
	totalTokens := totalIn + totalOut
	if totalTokens > 0 && s.cfg.Cost.DailyTokenCap > 0 {
		if err := s.store.AddDailyTokens(context.Background(), totalTokens); err != nil {
			s.logger.Warn("add daily tokens", "err", err)
		}
	}
	s.alertOnCostThreshold(send, sess.CostUSD, totalCost, dailyCostBefore)
	s.alertOnTokenThreshold(send, sessionTokensBefore, totalTokens, dailyTokensBefore)
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

	// Notify when a detached (background) session finishes: the user is not
	// watching the TUI, so surface completion/failure out-of-band (P5.4).
	if sess.Background && s.notifier != nil {
		ev := notify.Event{
			SessionID: id,
			Title:     sess.Title,
			Status:    notify.StatusCompleted,
			Message:   fmt.Sprintf("Background session %q completed", displayTitle(sess.Title, id)),
			CostUSD:   totalCost,
		}
		if runErr != nil && !errors.Is(runErr, engine.ErrInterrupted) {
			ev.Status = notify.StatusError
			ev.Message = fmt.Sprintf("Background session %q failed: %v", displayTitle(sess.Title, id), runErr)
		}
		s.notifier.Notify(context.Background(), ev)
	}
}

// alertOnCostThreshold sends a KindCostAlert event when this turn's spend
// pushes either the session or the daily total across the configured alert
// fraction of its cap, but only on the turn that crosses it (not every turn
// past the threshold) — checked by comparing the before/after totals (P9.5).
func (s *Server) alertOnCostThreshold(send func(api.Event), sessionCostBefore, turnCost, dailyCostBefore float64) {
	if turnCost <= 0 {
		return
	}
	frac := s.cfg.Cost.AlertThreshold
	if frac <= 0 {
		return
	}
	if cap := s.cfg.Cost.SessionCapUSD; cap > 0 {
		threshold := cap * frac
		after := sessionCostBefore + turnCost
		if sessionCostBefore < threshold && after >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("session spend at $%.4f — %.0f%% of the $%.2f cap", after, after/cap*100, cap)})
		}
	}
	if cap := s.cfg.Cost.DailyCapUSD; cap > 0 {
		threshold := cap * frac
		after := dailyCostBefore + turnCost
		if dailyCostBefore < threshold && after >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("daily spend at $%.4f — %.0f%% of the $%.2f cap", after, after/cap*100, cap)})
		}
	}
}

// alertOnTokenThreshold is the token-denominated counterpart to
// alertOnCostThreshold (P10.5) — same crossing-edge logic, checked against
// SessionTokenCap/DailyTokenCap instead of the dollar caps, so it still fires
// for local/unpriced models whose turnCost is always 0.
func (s *Server) alertOnTokenThreshold(send func(api.Event), sessionTokensBefore, turnTokens, dailyTokensBefore int) {
	if turnTokens <= 0 {
		return
	}
	frac := s.cfg.Cost.AlertThreshold
	if frac <= 0 {
		return
	}
	if cap := s.cfg.Cost.SessionTokenCap; cap > 0 {
		threshold := float64(cap) * frac
		after := sessionTokensBefore + turnTokens
		if float64(sessionTokensBefore) < threshold && float64(after) >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("session tokens at %d — %.0f%% of the %d cap", after, float64(after)/float64(cap)*100, cap)})
		}
	}
	if cap := s.cfg.Cost.DailyTokenCap; cap > 0 {
		threshold := float64(cap) * frac
		after := dailyTokensBefore + turnTokens
		if float64(dailyTokensBefore) < threshold && float64(after) >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("daily tokens at %d — %.0f%% of the %d cap", after, float64(after)/float64(cap)*100, cap)})
		}
	}
}

// displayTitle returns a human-friendly session label, falling back to a
// truncated id when the session has no title yet.
func displayTitle(title, id string) string {
	if title != "" {
		return title
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// addPermissionRule installs a pattern-scoped allow rule from an interactive
// "allow always" approval (TQ6): it takes effect for subsequent runs in this
// daemon immediately and is appended to the project config
// (.aegis/config.yaml → permission.rules) so it survives restarts. A rule
// that fails to parse or persist is logged, never fatal — the approval that
// produced it has already been granted.
func (s *Server) addPermissionRule(toolName, pattern string) {
	line := fmt.Sprintf("allow %s(%s)", toolName, pattern)
	rule, err := permission.ParseRule(line)
	if err != nil {
		s.logger.Warn("ignoring invalid approval-derived permission rule", "rule", line, "err", err)
		return
	}
	s.permMu.Lock()
	s.permRules = append(s.permRules, rule)
	s.permMu.Unlock()
	if err := config.AppendProjectPermissionRule(s.workspace, line); err != nil {
		s.logger.Warn("permission rule active for this daemon but not persisted", "rule", line, "err", err)
		return
	}
	s.logger.Info("persisted permission rule from approval", "rule", line)
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
	case ch <- approvalDecision{Approved: req.Approved, AllowAlways: req.AllowAlways, Pattern: req.Pattern}:
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
			GitSHA:    cp.GitSHA,
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

	// Serialize against an in-flight run on this session (same semaphore
	// handlePostMessage acquires): without this, a turn that's already running
	// finishes after the truncation below and appends its tail using a
	// Persisted offset captured before the rewind, silently reviving content
	// the user just rewound away. Waiting here is safe — the alternative queue
	// order (run first, then rewind) is exactly what handlePostMessage already
	// imposes on a second concurrent request.
	sem := s.sessionSemaphore(id)
	select {
	case sem <- struct{}{}:
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting for active run to finish")
		return
	}
	defer func() { <-sem }()

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
		// P3.4: git-native rollback — run `git reset --hard <sha>` before restoring
		// snapshotted files so untracked changes and index state are also reset.
		if req.GitRollback && cp.GitSHA != "" {
			gitArgs := []string{"-C", s.workspace, "reset", "--hard", cp.GitSHA}
			if out, err := execGitCmd(r.Context(), gitArgs...); err != nil {
				s.logger.Warn("git rollback failed", "sha", cp.GitSHA, "out", out, "err", err)
			} else {
				s.logger.Info("git rollback", "sha", cp.GitSHA)
			}
		}
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

// handleSetBackground marks or unmarks a session as a background (detached)
// session. A background session's engine runs are detached from the HTTP
// request context so the turn continues even if the TUI disconnects (P3.2).
func (s *Server) handleSetBackground(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.SetBackgroundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.store.SetBackground(r.Context(), id, req.Background); err != nil {
		s.logger.Error("set background", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetBGEvents returns buffered engine events for a background session,
// supporting incremental polling via the ?since=<id> query parameter (P3.2).
func (s *Server) handleGetBGEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		fmt.Sscan(v, &since)
	}
	events, err := s.store.ListBGEvents(r.Context(), id, since)
	if err != nil {
		s.logger.Error("list bg events", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.BGEventItem, 0, len(events))
	for _, e := range events {
		out = append(out, api.BGEventItem{ID: e.ID, Data: e.Data})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleArchiveSession soft-deletes a session; it is hidden from normal listings.
func (s *Server) handleArchiveSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Archive(r.Context(), id); err != nil {
		s.logger.Error("archive session", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnarchiveSession restores an archived session to active status.
func (s *Server) handleUnarchiveSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Unarchive(r.Context(), id); err != nil {
		s.logger.Error("unarchive session", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePruneSessions deletes non-archived sessions older than the configured TTL,
// or an explicit ?days=N override from the request.
func (s *Server) handlePruneSessions(w http.ResponseWriter, r *http.Request) {
	days := s.cfg.Cleanup.SessionTTLDays
	if v := r.URL.Query().Get("days"); v != "" {
		fmt.Sscan(v, &days)
	}
	if days <= 0 {
		writeError(w, http.StatusBadRequest, "days must be > 0 (or configure cleanup.session_ttl_days)")
		return
	}
	n, err := s.store.Prune(r.Context(), time.Duration(days)*24*time.Hour)
	if err != nil {
		s.logger.Error("prune sessions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.logger.Info("pruned old sessions", "deleted", n, "ttl_days", days)
	writeJSON(w, http.StatusOK, api.PruneResponse{Deleted: n})
}

// execGitCmd runs a git sub-command and returns combined output. Used for
// git-native rollback (P3.4).
func execGitCmd(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// captureGitSHA returns the current HEAD commit SHA via `git rev-parse HEAD`,
// returning an empty string if git is unavailable or the directory is not a
// repo.
func captureGitSHA(ctx context.Context, root string) string {
	out, err := execGitCmd(ctx, "-C", root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return out
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
	if sk := skills.BuildIndex(s.workspace, s.cfg.DataDir, s.cfg.Skills.BuiltinEnabled); sk != "" {
		parts = append(parts, sk)
	}
	if s.repoMap != "" {
		parts = append(parts, s.repoMap)
	}
	if dt := deferredToolsBlock(s.tools); dt != "" {
		parts = append(parts, dt)
	}
	if db := debateIntegrationBlock(s.cfg.Security.Debate); db != "" {
		parts = append(parts, db)
	}
	return strings.Join(parts, "\n\n")
}

// debateIntegrationBlock returns the P12.5 opt-in instruction text wiring the
// `agent` tool's debate mode into the two existing security workflows that
// benefit from adversarial review, or "" if neither toggle is enabled (the
// default — debate multiplies model calls per item, so this is never
// injected silently). Both toggles can be on independently; the block only
// mentions the ones actually enabled.
func debateIntegrationBlock(cfg config.DebateIntegrationConfig) string {
	if !cfg.ThreatModel && !cfg.Triage {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Debate mode (P12)\n")
	if cfg.ThreatModel {
		b.WriteString("- Threat modeling: before writing an identified threat/mitigation pair into the threat model document, call the `agent` tool with mode:\"debate\" and claim set to that threat/mitigation pair. Adjust the entry's severity/mitigation per the arbiter's verdict before finalizing it.\n")
	}
	if cfg.Triage {
		b.WriteString("- Security-audit triage: before suppressing a borderline or disputed-severity scan finding via the baseline, call the `agent` tool with mode:\"debate\" and claim set to the finding (severity, location, rationale). Only suppress if the verdict upholds the low-risk assessment.\n")
	}
	return b.String()
}

// toExecSpecs converts config hook entries into hooks.ExecSpec values.
func toExecSpecs(cfgHooks []config.HookConfig) []hooks.ExecSpec {
	specs := make([]hooks.ExecSpec, 0, len(cfgHooks))
	for _, h := range cfgHooks {
		specs = append(specs, hooks.ExecSpec{
			Event:      h.Event,
			Command:    h.Command,
			Tools:      h.Tools,
			TimeoutSec: h.TimeoutSec,
		})
	}
	return specs
}

// deferredToolsBlock advertises tools that are registered but not exposed by
// default (P4.6). The model loads them on demand with the tool_search tool.
func deferredToolsBlock(reg *tool.Registry) string {
	if reg == nil {
		return ""
	}
	deferred := reg.Deferred()
	if len(deferred) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<deferred_tools>\n")
	sb.WriteString("These tools are not loaded yet. When a task needs one, call `tool_search` with keywords to load it before use.\n")
	for _, d := range deferred {
		fmt.Fprintf(&sb, "- %s: %s\n", d.Name, d.Description)
	}
	sb.WriteString("</deferred_tools>")
	return sb.String()
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
		Background:   m.Background,
		Archived:     m.Archived,
		ArchivedAt:   m.ArchivedAt,
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

// expandFileMentions replaces @path#L10-40 tokens in text with the referenced
// file lines so the model sees the content directly (P5.5).
// Tokens must be preceded by whitespace or start-of-text and have the form
// @<relpath>#L<start>-<end> or @<relpath>#<start>-<end>.
// If a token cannot be resolved (file missing, range invalid) it is left as-is.
func expandFileMentions(text, workspace string) string {
	if !strings.Contains(text, "@") || !strings.Contains(text, "#") {
		return text
	}
	fields := strings.Fields(text)
	changed := false
	for i, f := range fields {
		if !strings.HasPrefix(f, "@") || !strings.Contains(f, "#") {
			continue
		}
		atPath, rangeStr, _ := strings.Cut(strings.TrimPrefix(f, "@"), "#")
		if atPath == "" || rangeStr == "" {
			continue
		}
		// Parse #L10-40 or #10-40.
		rangeStr = strings.TrimPrefix(rangeStr, "L")
		var start, end int
		if _, err := fmt.Sscanf(rangeStr, "%d-%d", &start, &end); err != nil || start < 1 || end < start {
			continue
		}
		abs := filepath.Join(workspace, filepath.FromSlash(atPath))
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		if end > len(lines) {
			end = len(lines)
		}
		excerpt := strings.Join(lines[start-1:end], "\n")
		fields[i] = fmt.Sprintf("```\n// @%s#L%d-%d\n%s\n```", atPath, start, end, excerpt)
		changed = true
	}
	if !changed {
		return text
	}
	return strings.Join(fields, " ")
}
