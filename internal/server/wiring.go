// Construction helpers for New: each wire* method installs one subsystem onto
// the Server and is responsible for its own failure posture. Extracted from
// server.go (L4) so the constructor's shape — what the daemon is made of, and
// in what order — is readable without scrolling past the subsystems.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/fiddler110/aegis/internal/agentdef"
	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/longmem"
	"github.com/fiddler110/aegis/internal/lsp"
	"github.com/fiddler110/aegis/internal/mcp"
	"github.com/fiddler110/aegis/internal/modelcaps"
	"github.com/fiddler110/aegis/internal/notify"
	"github.com/fiddler110/aegis/internal/opregister"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/plugins"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// wireProvider builds the model adapter from cfg (a missing API key is not
// fatal: the daemon still serves session management and reports the error
// only when a turn is actually run) and stores it on s.
func (s *Server) wireProvider(cfg *config.Config, modelCaps *modelcaps.Store, logger *slog.Logger) {
	adapter, err := providerfactory.Build(cfg, logger, providerfactory.WithModelCaps(modelCaps))
	if err != nil {
		logger.Warn("provider not ready; message runs will fail until configured", "err", err)
		adapter = nil
	}
	// P34.5: a pre-P33.9 config silently gets none of the native adapter's
	// num_ctx/keep_alive/telemetry, forever. `aegis doctor` reports it too, but
	// the whole problem is that nobody thinks to look — so say it once here,
	// where every daemon start passes.
	if detail := providerfactory.LegacyOllamaCompatDetail(cfg.Provider); detail != "" {
		// This warning fires before the context window is auto-detected
		// (initContextWindow, below), so pass modelMax: 0 for the baseline
		// context_window recommendation. `aegis doctor` probes the model's real
		// max and calibrates the number (P35.3); this hot-path start does not.
		logger.Warn(detail, "fix", providerfactory.LegacyOllamaCompatFix(cfg.Provider, 0))
	}
	s.adapter = adapter
}

// wireCoreStores opens the background-task, checkpoint and operation-register
// stores, all of which share s.store's single DB connection.
func (s *Server) wireCoreStores(cfg *config.Config, logger *slog.Logger) error {
	// Background-task manager shares the session database's single connection.
	taskStore, err := task.NewStore(s.store.DB())
	if err != nil {
		return err
	}
	s.tasks = task.NewManager(taskStore, logger)

	// Checkpoint store shares the session database connection. Wired into the
	// session store's Delete/Prune so checkpoint snapshots are cleaned up on
	// every deletion path, not just the HTTP delete-session handler (P32.3).
	checkpointStore, err := checkpoint.NewStore(s.store.DB())
	if err != nil {
		return err
	}
	s.store.SetCheckpointCleaner(checkpointStore)
	s.checkpoints = checkpointStore

	// Operation register shares the session database connection too (P65.4).
	opRegisterStore, err := opregister.NewStore(s.store.DB())
	if err != nil {
		return err
	}
	s.opRegister = opRegisterStore
	return nil
}

// wireCron builds the background job scheduler and its RunFunc. Run after
// s.tasks exists: the fire-time permission check and notification hooks are
// plain method values (s.cronPermCheck, s.cronNotify) since s is already
// fully addressable here (P78.9) — pre-P78.9 these had to be closures reading
// a not-yet-assigned `var s *Server` lazily at actual fire time, long after
// New returned (P27.15/FIND-08).
func (s *Server) wireCron(cwd string, sb sandbox.Backend, logger *slog.Logger) error {
	cronStore, err := cron.NewStore(s.store.DB())
	if err != nil {
		return err
	}
	runCronCmd := cronShellRunner(sb, cwd)
	cronRun := newCronRunFunc(cronStore, s.tasks, runCronCmd, s.cronPermCheck, s.cronNotify, logger)
	s.cronSched = cron.NewScheduler(cronStore, cronRun, logger)
	return nil
}

// wireLSP starts every configured language server. No-op when none are
// configured, leaving s.lspMgr nil.
func (s *Server) wireLSP(cfg *config.Config, cwd string, logger *slog.Logger) {
	if len(cfg.LSP) == 0 {
		return
	}
	lspMgr := lsp.NewManager(cwd, logger)
	for _, lc := range cfg.LSP {
		if err := lspMgr.Start(context.Background(), lsp.ServerConfig{
			Name: lc.Name, Command: lc.Command, Args: lc.Args, Extensions: lc.Extensions, Trust: lc.Trust,
		}); err != nil {
			logger.Warn("lsp server start failed", "name", lc.Name, "err", err)
		}
	}
	s.lspMgr = lspMgr
}

// wireKnowledgeAndMemory opens the project knowledge base (P3.3) and
// long-term entity memory (P3.1) stores, which share the per-user data
// directory. Failures here are non-fatal — the tools are simply skipped.
func (s *Server) wireKnowledgeAndMemory(cfg *config.Config, cwd string, logger *slog.Logger) {
	knowledgeStore, err := knowledge.Open(cwd, cfg.KnowledgeDBPath(cwd), s.embedder)
	if err != nil {
		logger.Warn("knowledge store unavailable", "err", err)
		knowledgeStore = nil
	}
	s.knowledge = knowledgeStore

	projectName := filepath.Base(cwd)
	longMemStore, err := longmem.Open(projectName, cfg.LongMemDBPath(), s.embedder)
	if err != nil {
		logger.Warn("long-term memory store unavailable", "err", err)
		longMemStore = nil
	}
	s.longMem = longMemStore
}

// wireTools builds the tool registry and registers every built-in and plugin
// tool. Must run after s.tasks, s.cronSched, s.sandbox, s.lspMgr, s.knowledge
// and s.longMem are set, since regOpts wires all of them through into the
// registered tools' host options.
func (s *Server) wireTools(cfg *config.Config, cwd string, sb sandbox.Backend, logger *slog.Logger) error {
	// Shared team task list for agent-team coordination (P5.1); reuses the
	// session DB. A failure here is non-fatal — team tools are simply skipped.
	teamTasks, err := swarm.NewTaskList(s.store.DB())
	if err != nil {
		logger.Warn("swarm: team task list unavailable", "err", err)
		teamTasks = nil
	}

	reg := tool.NewRegistry()
	ft := filetracker.New()
	todoList := builtin.NewTodoList()
	// knowledgeProvider is a plain method value (P78.9) — pre-P78.9 this had to
	// be a closure reading a not-yet-assigned `var s *Server` lazily at actual
	// tool-call time, same as the cron RunFunc above.
	knowledgeProvider := builtin.KnowledgeProviderFunc(s.knowledgeStoreFor)
	regOpts := enginecfg.BuiltinOptions(cfg, cwd)
	regOpts.Tasks = s.tasks
	regOpts.Cron = s.cronSched
	regOpts.Sandbox = sb
	regOpts.FileTracker = ft
	regOpts.LSP = s.lspMgr
	regOpts.TodoList = todoList
	// Search now comes from enginecfg.BuiltinOptions itself (a config-derived
	// field, not host wiring) — see its own comment.
	regOpts.TeamTasks = teamTasks
	regOpts.MailboxRoot = swarm.MailboxRoot(cfg.DataDir)
	regOpts.Knowledge = s.knowledge
	regOpts.KnowledgeProvider = knowledgeProvider
	regOpts.LongMem = s.longMem
	// LocalProfile and the rest of the config-derived option set are decided by
	// enginecfg.BuiltinOptions (P62.10/P66.13); everything assigned above is host
	// wiring only the daemon has, which is why it is assigned rather than shared.
	if err := builtin.Register(reg, regOpts); err != nil {
		return err
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

	s.fileTracker = ft
	s.tools = reg
	return nil
}

// wireSecurityWarnings surfaces startup warnings for security postures that
// are easy to misconfigure in ways that silently weaken isolation, and
// refuses to start outright for the one combination that means unattended
// RCE (auto-approved execution over an unsandboxed backend). Must run after
// s.tools/s.sandbox are set.
func (s *Server) wireSecurityWarnings(cfg *config.Config, logger *slog.Logger) error {
	if cfg.WorkspaceTrust.Frozen {
		logger.Warn("workspace not trusted: project .aegis/config.yaml security-relevant settings are frozen to user/global values",
			"dir", cfg.WorkspaceTrust.Dir, "changes", cfg.WorkspaceTrust.Changes,
			// P66.25/SEC-07: distinguishes "never trusted" from "trusted, then
			// the repository's security config moved under you".
			"stale_grant", cfg.WorkspaceTrust.Stale,
			"fix", "run `aegis trust` to review and accept them")
	}
	if _, isLocal := s.sandbox.(*sandbox.LocalBackend); isLocal {
		if cfg.Permission.Mode != string(permission.ModePlan) {
			// FIND-04/P27.14: the local backend strips secrets from the
			// spawned env (P7.2) but gives no fs/network/process isolation —
			// an approval prompt (build mode) or auto-approval (auto mode) is
			// the only compensating control once a command is approved. The
			// sharper mode-specific warnings below cover auto/auto_approve_exec;
			// this persistent one covers the default build-mode case too,
			// which had no startup signal at all before this.
			logger.Warn("sandbox backend is 'local' (unconfined): approved shell/execute tool calls run directly on the host with no filesystem/network isolation; consider sandbox.backend: container (default, needs Docker/Podman) or os (macOS/Linux, no container runtime needed) for real isolation")
		}
		if cfg.Permission.Mode == string(permission.ModeAuto) || cfg.Permission.AutoApproveExec {
			// auto mode / auto_approve_exec + an unsandboxed backend means every
			// model-issued shell command runs on the host with no approval
			// and no isolation whatsoever — unattended RCE by design. A WARN
			// line alone (the pre-P25.2 behavior) is too easy to miss,
			// especially since it's also the exact combo the docs suggest
			// for containerized auto-runs, so a plain backend typo (e.g.
			// sandbox.backend: podman before P25.2's aliasing) silently
			// landed here. Refuse to start unless the operator has
			// explicitly acknowledged the risk via the opt-out.
			if err := unsandboxedAutoExecError(cfg.Permission, cfg.Sandbox.Backend, s.sandboxFallback, s.sandboxFallbackReason); err != nil {
				return err
			}
			logger.Warn("auto-approved execution is enabled with the local sandbox: every shell command runs on the host without prompting (permission.allow_unsandboxed_auto_exec is set, so this is not blocking startup)",
				"mode", cfg.Permission.Mode, "auto_approve_exec", cfg.Permission.AutoApproveExec)
		}
	}
	if cfg.Security.EgressThenWrite || len(cfg.Security.NetworkAllowList) > 0 {
		if _, ok := s.tools.Get("shell"); ok {
			logger.Warn("network security policy (egress_then_write / network_allowlist) does not constrain the shell tool; commands such as curl/wget/nc bypass it — enforce egress with the container sandbox for a hard guarantee")
		}
	}
	return nil
}

// wirePermissionRules parses text-based permission rules once at startup. A
// malformed rule is logged and skipped rather than aborting the daemon. Must
// run after s.tools is set (P7.7's unmatchable-rule check reads its schema).
func (s *Server) wirePermissionRules(cfg *config.Config, logger *slog.Logger) {
	if len(cfg.Permission.Rules) == 0 {
		return
	}
	rules, err := permission.ParseRules(cfg.Permission.Rules)
	if err != nil {
		logger.Warn("ignoring invalid permission rules", "err", err)
		return
	}
	s.permRules = rules
	logger.Info("loaded permission rules", "count", len(rules))
	// P7.7: flag any rule that can never match because the target tool's
	// schema has none of the fields subjectFor knows how to read — otherwise a
	// scoped deny silently never fires.
	permission.WarnUnmatchableRules(rules, s.tools.All(), func(msg string, args ...any) {
		logger.Warn(msg, args...)
	})
}

// wirePersonasAndCommands loads custom agent definitions, hot-reloadable
// persona templates and slash commands from the user/project directories.
func (s *Server) wirePersonasAndCommands(cfg *config.Config, cwd string, logger *slog.Logger) {
	// Load custom agent definitions from user/project directories.
	if n := agentdef.LoadFromDirs(agentdef.DiscoverDirs(cfg.DataDir, cwd)...); n > 0 {
		logger.Info("loaded custom agent definitions", "count", n)
	}

	// Load custom persona templates from user/project directories. Refresh
	// (rather than LoadFromDirs) primes the change-detection signature so
	// later refreshPersonas calls are cheap no-ops until a file changes.
	//
	// personaProjectDir/personaProjectTrusted gate the project-sourced
	// persona directory's control fields (mode/tools/rules/output_guard) on
	// the same workspace-trust decision applyWorkspaceTrust/`aegis trust`
	// already governs for project config.yaml (P27.7/FIND-09), rather than
	// applying an untrusted repo's persona frontmatter as real settings with
	// no check. Queried through config.WorkspaceTrusted rather than the store
	// directly, because since P66.25/SEC-07 the answer is "is there a grant
	// *and* does it still cover what is on disk" — a caller that opened the
	// store itself would answer the old, content-blind question.
	s.personaDirs = persona.DiscoverDirs(cfg.DataDir, cwd)
	s.personaProjectDir = persona.ProjectDir(cwd)
	s.personaProjectTrusted = config.WorkspaceTrusted(cwd)
	if n, _ := persona.Refresh(s.personaProjectDir, s.personaProjectTrusted, s.personaDirs...); n > 0 {
		logger.Info("loaded custom personas", "count", n)
	}

	s.cmdReg = commands.Discover(commands.CommandDirs(cfg.DataDir, cwd)...)
}

// wireAuthAndTLS generates (or loads) the daemon's bearer token and, when
// configured, its TLS certificate/key pair.
func (s *Server) wireAuthAndTLS(cfg *config.Config) error {
	token, err := generateAndWriteToken(cfg.AuthTokenPath())
	if err != nil {
		return fmt.Errorf("auth token: %w", err)
	}
	s.authToken = token

	// P81.3/FIND-03: a second, separately-stored credential for the config
	// PATCH endpoints that can weaken the daemon's security posture. Written
	// to its own file so that "reads daemon.token" and "can weaken command
	// isolation" are no longer the same permission.
	adminToken, err := generateAndWriteToken(cfg.AdminTokenPath())
	if err != nil {
		return fmt.Errorf("admin token: %w", err)
	}
	s.adminToken = adminToken

	if cfg.Server.TLS.Enabled {
		cert, err := ensureTLSCert(cfg.TLSCertPath(), cfg.TLSKeyPath(), s.logger)
		if err != nil {
			return fmt.Errorf("tls cert: %w", err)
		}
		s.tlsCert = &cert
	}
	return nil
}

// wireHooks builds the audit trail, desktop/webhook notifier and
// user-configured lifecycle hooks.
func (s *Server) wireHooks(cfg *config.Config, logger *slog.Logger) {
	s.audit = hooks.NewAudit(filepath.Join(cfg.DataDir, "audit.jsonl"))
	s.notifier = notify.New(cfg.Notify.Desktop, cfg.Notify.Webhook, logger)
	s.execHook = enginecfg.ExecHooks(cfg, logger)
	if s.execHook != nil {
		s.hooks = hooks.NewMulti(s.audit, s.execHook)
	} else {
		s.hooks = hooks.NewMulti(s.audit)
	}
}

// wireCompaction sizes and builds the compaction summarizer against the
// model compaction actually runs on (provider.small_model when set,
// otherwise the global model). Callers must only invoke this when s.adapter
// is non-nil.
func (s *Server) wireCompaction(cfg *config.Config) {
	compModel := cfg.Provider.Model
	if cfg.Provider.SmallModel != "" {
		compModel = cfg.Provider.SmallModel // prefer a fast small model for compaction
	}
	s.compModel = compModel
	// Resolve the effective context window first (P23.1): explicit config, or
	// auto-detected from a local Ollama server — whose OpenAI-compat endpoint
	// otherwise truncates oversized prompts silently.
	s.initContextWindow(context.Background())
	// Keyed to the model compaction actually *runs on*, not the global one
	// (P52.1, second half). Compaction prefers provider.small_model, so tuning
	// the summarizer to the primary model's window is the same wrong-model-
	// window bug P52.1 fixed for the engine, one layer down — and worse here,
	// because the summarizer's own request is what gets silently truncated,
	// producing the broken/empty summary that P39.8's latch exists to stop
	// looping on. When compModel is the global model this is a cache hit and
	// behaves exactly as before.
	win, _ := s.effectiveContextWindowFor(context.Background(), compModel)
	compOpts := compaction.Options{
		// Serve the summarizer's own model with its own num_ctx (P52.4), for
		// the same reason every other run gets one.
		Adapter:       provider.WithNumCtx(s.adapter, win),
		Model:         compModel,
		ContextWindow: win,
		// P66.14: the trigger reserves room for the completion, so the
		// summarizer needs the same max_tokens the engine's gate uses —
		// otherwise the two gate on different numbers, which is LLM-02.
		MaxTokens: cfg.Provider.MaxTokens,
		// A local model server caches the KV of each request's longest common
		// prefix, so rewriting the middle of the conversation costs a full
		// prefill recompute instead of nothing — make the deterministic prune
		// pre-pass headroom-gated rather than unconditional there. Same
		// local/loopback test admission control uses; cloud providers are
		// unaffected. Measured worth ~1.7x on wall clock once P62.4 fixed the
		// estimate the trigger runs on; see config.CompactionConfig for why the
		// first measurement said the opposite. compaction.preserve_prefix_cache
		// overrides the detection, so the gate stays A/B-able without a rebuild.
		PreservePrefixCache: cfg.Compaction.PreservePrefixCacheOr(
			config.LocalBackend(cfg.Provider.Default, cfg.Provider.BaseURL)),
		// P67.6: how many clearable tool results the cold-cache pass leaves
		// verbatim. 0 takes the package default; the pass floors it at 1.
		ColdCacheKeep: cfg.Compaction.ColdCacheKeep,
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

// wireMCP connects every configured MCP server, registers its tools, and —
// once s.adapter exists — wires sampling so those servers can request text
// generation from the model.
func (s *Server) wireMCP(cfg *config.Config, logger *slog.Logger) {
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
			ScanOutput:       m.ScanOutputEnabled(),
			ScanArguments:    m.ScanArguments,
		})
	}
	// P81.2/FIND-02: pins each stdio server's resolved binary digest and
	// advertised tool-name set across restarts and live tools/list_changed
	// notifications — see mcp.TrustStore's doc comment for why this is
	// trust-on-first-use rather than a second approval prompt for the same
	// act of adding the server to config.
	mcpTrust := mcp.OpenTrustStore(filepath.Join(cfg.DataDir, "mcp_trust.json"))
	s.mcpClients = mcp.RegisterServers(context.Background(), s.tools, mcpServers, logger, mcpTrust)

	if s.adapter != nil {
		samplingFn := buildSamplingHandler(s.adapter, cfg.Provider.Model, cfg.Provider.MaxTokens, logger)
		for _, cl := range s.mcpClients {
			cl.Sampling = samplingFn
		}
	}
}

// wireSwarm chooses the sub-agent backend and registers the `agent` tool.
func (s *Server) wireSwarm(cfg *config.Config, logger *slog.Logger) error {
	s.swarmReg = swarm.NewRegistry()
	s.swarm = s.buildSwarmBackend(swarm.MailboxRoot(cfg.DataDir))
	s.swarm.OnStop(s.onSubagentStop)
	s.agentLimiter = swarm.NewAdaptiveLimiter(builtin.MaxParallelAgents)
	return s.tools.Register(builtin.NewAgentTool(s.swarm, s.tasks,
		builtin.WithCostCaps(cfg.Cost.BudgetUSD, cfg.Cost.MaxTokensPerRun),
		builtin.WithConcurrencyLimiter(s.agentLimiter),
		builtin.WithDataDir(cfg.DataDir),
		builtin.WithDebateSeatModel(func(p string) string {
			return enginecfg.DebateSeatModel(cfg, p)
		}),
		// P69.6: the seat models above decide *which* models a debate makes
		// resident; this decides what each of them can afford to be served at
		// while the others are loaded. Inert unless provider.vram_budget_gb is
		// set, so wiring it changes nothing for an install that has not opted in.
		builtin.WithResidentSetClaim(s.claimResidentSet),
	))
}
