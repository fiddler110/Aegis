// Package server is the Aegis daemon. It owns the session store, the model
// adapter, the tool registry, and runs the agent engine, exposing everything
// over a local HTTP API (with server-sent events for streaming runs).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fiddler110/aegis/internal/agentdef"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/embed"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/filetracker"
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
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
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
	repoMap     string            // cached repository map block for the system prompt (empty when not indexed); guarded by repoMapMu
	repoMapMu   sync.Mutex        // protects repoMap (rebuilt at runtime by POST /repomap/index, P14.3)
	personaDirs []string          // directories rescanned by refreshPersonas for hot reload
	workspace   string
	logger      *slog.Logger
	http        *http.Server
	authToken   string // shared secret for API authentication

	// invalidAuthAttempts counts requests rejected by authMiddleware for a
	// missing or mismatched bearer token (FIND-11). It is a single
	// process-wide counter rather than a per-remote-address map so that the
	// audit fix itself can't be turned into a memory-growth DoS by an
	// attacker hammering the endpoint with spoofed/varying source data.
	invalidAuthAttempts atomic.Uint64

	// pageTokens holds short-lived, single-use tokens minted per GET /ui load
	// (P15.12), each paired with a CSRF nonce binding the exchange to the
	// browser that loaded the page (FIND-01/P24.1): the page carries one of
	// these instead of authToken, and the frontend immediately trades it for
	// the real token via POST /auth/exchange (see
	// mintPageToken/exchangePageToken in auth.go), so a leaked page source
	// can't be replayed as a standing credential.
	pageTokenMu sync.Mutex
	pageTokens  map[string]pageTokenEntry

	// sandboxFallback and sandboxFallbackReason record whether the configured
	// sandbox backend failed to initialize and the daemon fell back to
	// unsandboxed local execution (P7.4). Surfaced via /healthz so clients can
	// warn the user instead of silently trusting a sandbox that isn't there.
	sandboxFallback       bool
	sandboxFallbackReason string

	// Effective context-window state (P23.1, see contextwindow.go): the token
	// window the daemon believes the model server will honor, driving
	// compaction thresholds and the /status + TUI usage surface. ctxWinFinal
	// marks the value authoritative (explicit config on a cloud provider, or
	// an Ollama loaded-model reading); until then runs re-detect. summarizer
	// is the concrete compactor so a late detection can retune it in place.
	ctxWinMu    sync.Mutex
	ctxWin      int
	ctxWinSrc   string
	ctxWinFinal bool
	ollamaBase  string // native Ollama API base when (possibly) Ollama; "" otherwise
	summarizer  *compaction.Summarizer

	// agentLimiter throttles how many sub-agents a 'parallel' workflow batch
	// runs simultaneously (P17), adapting from observed batch behavior. One
	// instance per daemon process, shared by every session's agent tool calls;
	// in-memory only, does not persist across restarts. Surfaced on GET /status.
	agentLimiter *swarm.AdaptiveLimiter

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

	// runSem bounds total concurrent active runs across every session
	// (P21.5). nil when server.max_concurrent_runs is 0 (unlimited).
	// Acquired non-blocking in handlePostMessage: a full semaphore rejects
	// the request immediately (429) rather than queuing it, since queuing
	// would just relocate the unbounded-fan-out problem from goroutines to a
	// backlog — see ServerConfig.MaxConcurrentRuns.
	runSem chan struct{}

	// sessionTools maps session ID → a *tool.Registry clone of s.tools (P9).
	// tool_search exposes deferred tools by mutating a registry's exposed map;
	// without a per-session clone, that mutation was permanent and
	// process-wide, silently exposing a tool's schema to every other
	// concurrent or future session and persona sharing the daemon's one
	// registry. Lazily created on first use per session and reused across
	// that session's turns so a loaded tool stays loaded turn to turn.
	sessionTools sync.Map // string → *tool.Registry

	// sessionSkills maps session ID → extra embedded built-in skill names
	// activated on demand for that session (e.g. via /threat-model), layered
	// on top of the persistent cfg.Skills.BuiltinEnabled list. In-memory only:
	// it resets on daemon restart and never touches config, so built-ins stay
	// dormant by default and are only ever pulled in by an explicit request.
	sessionSkills sync.Map // string → []string
}

// activateSessionSkill turns on a built-in skill for one session: it's added
// to that session's extra-enabled set (read by effectiveSystem for the
// <skills_available> index) and the session's tool registry clone gets an
// updated skill tool so the `skill` tool can load it immediately, without
// waiting for a restart or writing to config.
func (s *Server) activateSessionSkill(id, name string) {
	var extra []string
	if v, ok := s.sessionSkills.Load(id); ok {
		extra = v.([]string)
	}
	for _, n := range extra {
		if strings.EqualFold(n, name) {
			return
		}
	}
	extra = append(append([]string{}, extra...), name)
	s.sessionSkills.Store(id, extra)

	enabled := append(append([]string{}, s.cfg.Skills.BuiltinEnabled...), extra...)
	s.sessionToolRegistry(id).Upsert(builtin.NewSkillTool(s.workspace, s.cfg.DataDir, enabled))
}

// sessionEnabledSkills returns the persistent config-level enabled built-ins
// plus any activated on demand for this session.
func (s *Server) sessionEnabledSkills(id string) []string {
	enabled := append([]string{}, s.cfg.Skills.BuiltinEnabled...)
	if v, ok := s.sessionSkills.Load(id); ok {
		enabled = append(enabled, v.([]string)...)
	}
	return enabled
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
	cronRun := newCronRunFunc(cronStore, taskMgr, runCronCmd,
		func() permission.Mode { return permission.ParseMode(cfg.Permission.Mode) }, logger)
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
				Name: lc.Name, Command: lc.Command, Args: lc.Args, Extensions: lc.Extensions, Trust: lc.Trust,
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
	if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL, Tasks: taskMgr, Cron: cronSched, Sandbox: sb, FileTracker: ft, LSP: lspMgr, TodoList: todoList, Search: builtin.SearchOptions{Provider: cfg.Search.Provider, APIKey: cfg.Search.APIKey, BaseURL: cfg.Search.BaseURL, ScanOutput: cfg.Search.ScanOutput}, TeamTasks: teamTasks, MailboxRoot: swarm.MailboxRoot(cfg.DataDir), Knowledge: knowledgeStore, LongMem: longMemStore, BuiltinSkills: cfg.Skills.BuiltinEnabled, SecurityScan: security.OptionsFromConfig(cfg.Security), DASTAllowedTargets: cfg.Security.DAST.AllowedTargets, DASTAllowActive: cfg.Security.DAST.AllowActive}); err != nil {
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
		// Resolve the effective context window first (P23.1): explicit config,
		// or auto-detected from a local Ollama server — whose OpenAI-compat
		// endpoint otherwise truncates oversized prompts silently.
		s.initContextWindow(context.Background())
		win, _ := s.effectiveContextWindow()
		compOpts := compaction.Options{
			Adapter:       adapter,
			Model:         compModel,
			ContextWindow: win,
		}
		// A local provider whose window is still unknown (Ollama unreachable at
		// startup): skip auto-compaction rather than falling back to the 120k
		// default. maybeRefreshContextWindow retunes the summarizer once the
		// server is up and the window is known.
		if win == 0 && cfg.Provider.Default == "ollama" {
			compOpts.MaxBudget = 0 // explicit skip
		}
		s.summarizer = compaction.New(compOpts)
		s.compactor = s.summarizer
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
			ScanOutput:       m.ScanOutput,
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
	s.agentLimiter = swarm.NewAdaptiveLimiter(builtin.MaxParallelAgents)
	if err := reg.Register(builtin.NewAgentTool(s.swarm, s.tasks,
		builtin.WithCostCaps(cfg.Cost.BudgetUSD, cfg.Cost.MaxTokensPerRun),
		builtin.WithConcurrencyLimiter(s.agentLimiter),
	)); err != nil {
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
		// FIND-06 / P24.10: Docker/Podman socket access is privilege-equivalent
		// to local root regardless of the per-container hardening flags below —
		// surface that once per daemon start so an operator relying on the
		// container backend for isolation sees it. See docs/security_scan.md.
		if notice := sandbox.SocketPrivilegeNotice(csb.DetectedRuntime()); notice != "" {
			logger.Info(notice)
		}
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
	if cfg.Server.MaxConcurrentRuns > 0 {
		s.runSem = make(chan struct{}, cfg.Server.MaxConcurrentRuns)
	}
	s.agentLimiter = swarm.NewAdaptiveLimiter(builtin.MaxParallelAgents)
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
	mux.HandleFunc("PATCH /config/sandbox", s.handlePatchConfigSandbox)
	mux.HandleFunc("GET /config/security", s.handleGetConfigSecurity)
	mux.HandleFunc("PATCH /config/security", s.handlePatchConfigSecurity)
	mux.HandleFunc("GET /config/skills", s.handleGetConfigSkills)
	mux.HandleFunc("PATCH /config/skills", s.handlePatchConfigSkills)
	mux.HandleFunc("POST /config/harden", s.handleConfigHarden)
	mux.HandleFunc("POST /debate", s.handleDebate)
	mux.HandleFunc("POST /knowledge", s.handleKnowledge)
	mux.HandleFunc("POST /repomap/index", s.handleRepoMapIndex)
	mux.HandleFunc("GET /ui", s.handleWebUI)
	mux.HandleFunc("GET /ui/", s.handleWebUI)
	mux.HandleFunc("GET /ui/assets/", s.handleWebUIAssets)
	mux.HandleFunc("POST /auth/exchange", s.handleAuthExchange)
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
	if s.authToken == "" {
		return fmt.Errorf("server: refusing to start: auth token was not generated")
	}
	if err := s.validateListenAddr(); err != nil {
		return err
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

// handleStatusInfo serves the P14.5 /status TUI surface: daemon/provider
// identity, sandbox fallback state (same fields as /healthz), and the
// cross-session daily spend the P9.5/P10.5 caps already track in the session
// store. Kept as a separate endpoint from /healthz rather than growing that
// one, since /healthz is polled frequently (waitForDaemon) and shouldn't pay
// for two extra DB reads per call.
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
	}
	writeJSON(w, http.StatusOK, resp)
}
