// Package server is the Aegis daemon. It owns the session store, the model
// adapter, the tool registry, and runs the agent engine, exposing everything
// over a local HTTP API (with server-sent events for streaming runs).
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	"github.com/fiddler110/aegis/internal/modelcaps"
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
	"github.com/fiddler110/aegis/internal/toolcallprobe"
	"github.com/fiddler110/aegis/internal/workspacetrust"
)

const maxRequestBody = 10 << 20 // 10 MiB

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
	// wsRoots memoizes workspace.additional_roots resolution per session
	// workdir (P52.13) — see workspaceroots.go.
	wsRoots     workspaceRootCache
	tasks       *task.Manager
	cronSched   *cron.Scheduler
	cronCancel  context.CancelFunc
	checkpoints *checkpoint.Store
	fileTracker *filetracker.Tracker
	toolCalling *toolcallprobe.Gate // P34.2: per-model tool-calling verdict cache
	// modelCaps is the P53.5 on-disk capability cache shared by the adapter
	// (the `think`-rejection latch) and toolCalling (probe verdict + P53.4
	// conformance sample). Nil in tests built through newWithDeps.
	modelCaps *modelcaps.Store
	// toolCallWarned records the (session, model) pairs already warned about a
	// tool-incapable model, so the notice informs once rather than nagging every
	// turn. Written only when a model actually fails the probe.
	toolCallWarned   map[string]struct{}
	toolCallWarnedMu sync.Mutex

	// reachCache coalesces the P35.11 provider-reachability probe. /status
	// polls at 1-2s and each Ollama-path probe is a live GET /api/version;
	// without caching a fast poll loop is a steady upstream request stream to
	// Ollama for a value that changes rarely. reachCacheMu guards a single
	// last-probe entry reused within reachCacheTTL (see
	// probeProviderReachability). reachNow is a clock seam (nil == time.Now),
	// injected by tests to exercise expiry deterministically.
	reachCacheMu sync.Mutex
	reachCache   reachEntry
	reachNow     func() time.Time
	knowledge    *knowledge.Store // project knowledge base (P3.3); nil when unavailable
	longMem      *longmem.Store   // long-term entity memory (P3.1); nil when unavailable
	embedder     embed.Embedder   // shared semantic-recall embedder (P5.8); nil = BM25-only
	runs         *runRegistry
	sandbox      sandbox.Backend
	lspMgr       *lsp.Manager
	audit        *hooks.Audit
	execHook     *hooks.Exec      // user-configured lifecycle hooks (P4.4); nil when none
	notifier     *notify.Notifier // background-session notifications (P5.4); nil when disabled
	cmdReg       *commands.Registry
	permRules    []permission.Rule // parsed text-based allow/deny rules; guarded by permMu
	permMu       sync.Mutex        // protects permRules (approvals add rules at runtime, TQ6)
	repoMap      string            // cached repository map block for the system prompt (empty when not indexed); guarded by repoMapMu
	repoMapMu    sync.Mutex        // protects repoMap (rebuilt at runtime by POST /repomap/index, P14.3)
	personaDirs  []string          // directories rescanned by refreshPersonas for hot reload

	// knowledgeStores/repoMaps cache a per-session-Workdir instance of each
	// daemon-wide singleton (P25.9): s.knowledge/s.repoMap above remain the
	// fast path for the daemon's own default workspace (root == s.workspace),
	// unchanged from before; a session on a different Workdir gets its own
	// lazily-created entry here instead of silently seeing the daemon's own
	// project. See knowledgeStoreFor/repoMapFor. (s.cmdReg has no per-turn,
	// session-scoped consumer — its only reader is the daemon-wide GET
	// /commands admin listing — so commands directory discovery needs no
	// per-root cache here.)
	knowledgeStores rootCache[*knowledge.Store]
	repoMaps        rootCache[string]
	// personaProjectDir/personaProjectTrusted gate control-field trust for
	// project-sourced persona files (P27.7/FIND-09) — see persona.LoadFromDirs.
	// Computed once at startup from the workspace-trust store, matching how
	// config.Load()'s own workspace-trust gate is only re-evaluated on restart.
	personaProjectDir     string
	personaProjectTrusted bool
	workspace             string
	logger                *slog.Logger
	http                  *http.Server
	authToken             string // shared secret for API authentication

	// tlsCert is the daemon's TLS certificate/key pair, loaded or generated in
	// New when server.tls.enabled is true (FIND-32/P24.18); nil when TLS is
	// disabled (the default), in which case ListenAndServe serves plain HTTP
	// exactly as before this feature existed.
	tlsCert *tls.Certificate

	// invalidAuthAttempts counts requests rejected by authMiddleware for a
	// missing or mismatched bearer token (FIND-11). It is a single
	// process-wide counter rather than a per-remote-address map so that the
	// audit fix itself can't be turned into a memory-growth DoS by an
	// attacker hammering the endpoint with spoofed/varying source data. This
	// counter is cumulative for the whole process lifetime (used only for
	// the logging cadence above) and is distinct from the consecutive-streak
	// tracking authLockMu/authConsecutiveFailures/authLockedUntil use to
	// drive the P27.12/FIND-14 lockout below.
	invalidAuthAttempts atomic.Uint64

	// authLockMu guards authConsecutiveFailures/authLockedUntil, the P27.12/
	// FIND-14 throttle for repeated invalid-auth attempts (extends the
	// FIND-11 counter above, which only logs, with an actual lockout). The
	// daemon is loopback-only, so there's no meaningful per-IP concept to
	// key this by — a simple process-wide streak counter with an
	// exponentially growing lockout window is enough to slow down a local
	// process guessing at the bearer token without adding real state.
	authLockMu sync.Mutex
	// authConsecutiveFailures counts invalid-auth attempts since the last
	// successful authenticated request (or process start); reset to 0 on
	// success so a legitimate client that briefly used a stale token never
	// accumulates a permanent penalty.
	authConsecutiveFailures int
	// authLockedUntil is the time before which authMiddleware short-circuits
	// every request (valid token or not) with 429, zero when not locked.
	authLockedUntil time.Time

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
	// ctxWin/ctxWinSrc/ctxWinFinal hold the *globally configured* model's
	// entry — the server-wide number /status reports and the summarizer runs
	// against; ctxWinByModel holds one entry per other model a turn has
	// actually run on (persona pin, /model override, small-model routing),
	// each with its own re-detect state (P52.1).
	ctxWinMu      sync.Mutex
	ctxWin        int
	ctxWinSrc     string
	ctxWinFinal   bool
	ctxWinByModel map[string]ctxWinEntry
	ollamaBase    string // native Ollama API base when (possibly) Ollama; "" otherwise
	summarizer    *compaction.Summarizer
	// compModel is the model compaction runs on (provider.small_model when set,
	// otherwise the global model). The summarizer is retuned from *this* model's
	// window, not the global one — see setWindowLocked (P52.1).
	compModel string

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

	// pendingSteers maps session ID → *steerBox for mid-run steering.
	// The box is written by handleSteer and drained by the engine between tool
	// rounds, then by handlePostMessage once the run ends.
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

	// taskScopes maps session ID → that session's *permission.TaskScope (P46.1),
	// the per-task file-write allowlist the `scope` tool mutates and ScopeGate
	// enforces. Lazily created per session and reused across its turns so a
	// scope set during one turn stays in force until the model clears it.
	taskScopes sync.Map // string → *permission.TaskScope

	// sessionWorkdirs maps session ID → that session's own working directory
	// (P25.1), resolved and validated once at creation time in
	// handleCreateSession. Missing/empty means the session uses the
	// daemon's default workspace (s.workspace) — see workdirFor.
	sessionWorkdirs sync.Map // string → string

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
	s.sessionToolRegistry(id).Upsert(builtin.NewSkillTool(s.workdirFor(id), s.cfg.DataDir, enabled))
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

// taskScopeFor returns the session's per-task file-write scope (P46.1),
// creating an empty (inactive) one on first use so the `scope` tool and
// ScopeGate share the same object across the session's turns.
func (s *Server) taskScopeFor(id string) *permission.TaskScope {
	v, _ := s.taskScopes.LoadOrStore(id, permission.NewTaskScope())
	return v.(*permission.TaskScope)
}

// toolRegistryFor returns the session-scoped registry clone for sessionID, or
// the daemon-wide registry when no session is in scope (""). The deferred-tool
// advertisement has to read the session's own clone: tool_search loads onto
// the clone (P9), and persona activation now preloads onto it too (P34.3), so
// sourcing the advertisement from s.tools would keep telling the model to
// tool_search for tools whose schemas it can already see.
func (s *Server) toolRegistryFor(sessionID string) *tool.Registry {
	if sessionID == "" {
		return s.tools
	}
	return s.sessionToolRegistry(sessionID)
}

// workdirFor returns the working directory session id was created with
// (P25.1), or the daemon's default workspace when id is unset, unknown, or
// was created without an explicit Workdir. Cheap in-memory lookup — see
// sessionWorkdirs — so it can be called on every tool/system-prompt build
// without hitting the session store.
func (s *Server) workdirFor(id string) string {
	if id != "" {
		if v, ok := s.sessionWorkdirs.Load(id); ok {
			if wd, _ := v.(string); wd != "" {
				return wd
			}
		}
	}
	return s.workspace
}

// knowledgeStoreFor returns the knowledge store for root (P25.9): the
// daemon's own store unchanged when root is its default workspace (the
// common case, and the only case before P25.9), or a lazily-opened,
// cached store scoped to root's own ".aegis/knowledge.db" otherwise. Errors
// opening a non-default root's store are returned rather than logged so
// callers (project_knowledge) can fall back to their own fixed default.
func (s *Server) knowledgeStoreFor(root string) (*knowledge.Store, error) {
	if root == "" || root == s.workspace {
		return s.knowledge, nil
	}
	return s.knowledgeStores.getOrCreate(root, func() (*knowledge.Store, error) {
		return knowledge.Open(root, s.cfg.KnowledgeDBPath(root), s.embedder)
	})
}

// repoMapFor returns the cached repository-map system-prompt block for
// root (P25.9), lazily loading and caching it on first use for a root other
// than the daemon's own default workspace.
func (s *Server) repoMapFor(root string) string {
	if root == "" || root == s.workspace {
		s.repoMapMu.Lock()
		defer s.repoMapMu.Unlock()
		return s.repoMap
	}
	v, _ := s.repoMaps.getOrCreate(root, func() (string, error) {
		return loadRepoMap(root, s.logger), nil
	})
	return v
}

// workdirAllowed reports whether workdir (already resolved to an absolute
// path) is permitted for a session on this daemon, given how AllowRemote and
// SessionWorkdirAllowlist are configured (P25.1). A remote-accessible daemon
// must not become an arbitrary-filesystem oracle: once AllowRemote is set,
// only the daemon's own default workspace (or a directory nested under it)
// or a directory nested under an allowlisted root is accepted. The default
// loopback-only bind trusts any existing directory, matching today's model
// where a local client is already as trusted as a local shell user.
func (s *Server) workdirAllowed(workdir string) bool {
	if !s.cfg.Server.AllowRemote {
		return true
	}
	if withinRoot(s.workspace, workdir) {
		return true
	}
	for _, allowed := range s.cfg.Server.SessionWorkdirAllowlist {
		abs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if withinRoot(abs, workdir) {
			return true
		}
	}
	return false
}

// withinRoot reports whether target equals root or is nested under it. Both
// arguments must already be absolute, cleaned paths.
func withinRoot(root, target string) bool {
	root, target = filepath.Clean(root), filepath.Clean(target)
	if runtime.GOOS == "windows" {
		root, target = strings.ToLower(root), strings.ToLower(target)
	}
	if root == target {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	// P53.6: an unrecognized provider.tool_call_shim is treated as off, which is
	// indistinguishable from not setting it — so say so once at startup rather
	// than leaving a typo ("auto", "true") looking like a working setting.
	if !cfg.Provider.ToolCallShimValid() {
		logger.Warn("provider.tool_call_shim is not a recognized value; the tool-call shim stays off",
			"value", cfg.Provider.ToolCallShim, "expected", `"off" or "on"`)
	} else if cfg.Provider.ToolCallShimEnabled() {
		logger.Info("tool-call shim enabled: tool schemas are served in the system prompt and calls are parsed from the model's reply (provider.tool_call_shim)")
	}
	// Screen untrusted bundled skill directories through the same filesystem
	// scan `aegis security scan` drives, so a compromised .aegis/skills/ bundle's
	// scripts surface a HIGH/CRITICAL warning rather than reaching the model
	// unflagged (P44.1). Degrades to a silent no-op when the multiscanner image
	// isn't built and no host scanner is installed.
	scanOpts := security.OptionsFromConfig(cfg.Security)
	skills.SetBundleScanner(func(ctx context.Context, dir string) []string {
		return security.ScanBundleWarnings(ctx, dir, scanOpts)
	})
	store, err := session.Open(cfg.SessionDBPath())
	if err != nil {
		return nil, err
	}

	// Per-model capability cache (P53.5): opened before the adapter so the
	// Ollama adapter starts with the `think`-rejection latches a previous
	// process already paid for, and reconciled against the models actually
	// present so a re-pulled tag can't inherit the old weights' verdicts.
	modelCaps := cfg.OpenModelCaps()
	reconcileModelCaps(cfg, modelCaps, logger)

	// A missing API key is not fatal: the daemon still serves session
	// management and reports the error only when a turn is actually run.
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

	// Background-task manager shares the session database's single connection.
	taskStore, err := task.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, err
	}
	taskMgr := task.NewManager(taskStore, logger)

	// Checkpoint store shares the session database connection. Wired into the
	// session store's Delete/Prune so checkpoint snapshots are cleaned up on
	// every deletion path, not just the HTTP delete-session handler (P32.3).
	checkpointStore, err := checkpoint.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, err
	}
	store.SetCheckpointCleaner(checkpointStore)

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

	// Cron scheduler: fires due jobs as background tasks. s is declared here
	// (nil) and assigned below by newWithDeps — the RunFunc closure only
	// reads it at actual fire time, long after New returns, so capturing the
	// not-yet-initialized variable is safe (P27.15/FIND-08: the fire-time
	// permission check needs the fully assembled gate stack, which in turn
	// needs the tool registry and parsed permission rules that don't exist
	// until later in this constructor).
	var s *Server
	cronStore, err := cron.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, err
	}
	runCronCmd := cronShellRunner(sb, cwd)
	cronRun := newCronRunFunc(cronStore, taskMgr, runCronCmd,
		func(ctx context.Context, j cron.Job) (bool, string) { return s.cronPermCheck(ctx, j) }, logger)
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
	// KnowledgeProvider is a closure over s (nil here, assigned below by
	// newWithDeps) rather than a direct method value, so the deferred read at
	// actual tool-call time sees the fully constructed Server — same pattern
	// as cronRun/s.cronPermCheck above.
	knowledgeProvider := builtin.KnowledgeProviderFunc(func(root string) (*knowledge.Store, error) { return s.knowledgeStoreFor(root) })
	if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL, Tasks: taskMgr, Cron: cronSched, Sandbox: sb, FileTracker: ft, LSP: lspMgr, TodoList: todoList, Search: builtin.SearchOptions{Provider: cfg.Search.Provider, APIKey: cfg.Search.APIKey, BaseURL: cfg.Search.BaseURL, ScanOutput: cfg.Search.ScanOutput}, TeamTasks: teamTasks, MailboxRoot: swarm.MailboxRoot(cfg.DataDir), Knowledge: knowledgeStore, KnowledgeProvider: knowledgeProvider, LongMem: longMemStore, BuiltinSkills: cfg.Skills.BuiltinEnabled, SecurityScan: security.OptionsFromConfig(cfg.Security), DASTAllowedTargets: cfg.Security.DAST.AllowedTargets, DASTAllowActive: cfg.Security.DAST.AllowActive, LocalProfile: cfg.Provider.LocalPromptProfile(), GitPreCommitTestCommand: cfg.Git.PreCommitTestCommand, GitPreCommitTestTimeout: time.Duration(cfg.Git.PreCommitTestTimeoutSec) * time.Second}); err != nil {
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
	if cfg.WorkspaceTrust.Frozen {
		logger.Warn("workspace not trusted: project .aegis/config.yaml security-relevant settings are frozen to user/global values",
			"dir", cfg.WorkspaceTrust.Dir, "changes", cfg.WorkspaceTrust.Changes, "fix", "run `aegis trust` to review and accept them")
	}
	if _, isLocal := sb.(*sandbox.LocalBackend); isLocal {
		if cfg.Permission.Mode != string(permission.ModePlan) {
			// FIND-04/P27.14: the local backend strips secrets from the
			// spawned env (P7.2) but gives no fs/network/process isolation —
			// an approval prompt (build mode) or auto-approval (auto mode) is
			// the only compensating control once a command is approved. The
			// sharper mode-specific warnings below cover auto/auto_approve_exec;
			// this persistent one covers the default build-mode case too,
			// which had no startup signal at all before this.
			logger.Warn("sandbox backend is 'local' (unconfined): approved shell/execute tool calls run directly on the host with no filesystem/network isolation; consider sandbox.backend: os (macOS/Linux, no container runtime needed) or container for real isolation")
		}
		if cfg.Permission.Mode == string(permission.ModeAuto) && !cfg.Permission.AutoApproveExec {
			logger.Warn("permission mode 'auto' with the local sandbox runs model-issued shell commands directly on the host with no approval; use the container sandbox backend or 'build' mode for untrusted work")
		}
		if cfg.Permission.AutoApproveExec {
			// auto_approve_exec + an unsandboxed backend means every
			// model-issued shell command runs on the host with no approval
			// and no isolation whatsoever — unattended RCE by design. A WARN
			// line alone (the pre-P25.2 behavior) is too easy to miss,
			// especially since it's also the exact combo the docs suggest
			// for containerized auto-runs, so a plain backend typo (e.g.
			// sandbox.backend: podman before P25.2's aliasing) silently
			// landed here. Refuse to start unless the operator has
			// explicitly acknowledged the risk via the opt-out.
			if err := unsandboxedAutoExecError(cfg.Permission, cfg.Sandbox.Backend, sandboxFallback, sandboxFallbackReason); err != nil {
				if knowledgeStore != nil {
					_ = knowledgeStore.Close()
				}
				if longMemStore != nil {
					_ = longMemStore.Close()
				}
				store.Close()
				return nil, err
			}
			logger.Warn("auto_approve_exec is enabled with the local sandbox: every shell command runs on the host without prompting (permission.allow_unsandboxed_auto_exec is set, so this is not blocking startup)")
		}
	}
	if cfg.Security.EgressThenWrite || len(cfg.Security.NetworkAllowList) > 0 {
		if _, ok := reg.Get("shell"); ok {
			logger.Warn("network security policy (egress_then_write / network_allowlist) does not constrain the shell tool; commands such as curl/wget/nc bypass it — enforce egress with the container sandbox for a hard guarantee")
		}
	}

	s = newWithDeps(cfg, logger, store, adapter, reg)
	// Hand the tool-calling gate the same capability store the adapter got, so
	// a probe verdict measured in an earlier process is reused instead of
	// re-run (P53.5). Replacing the gate rather than parameterising
	// newWithDeps keeps that constructor's 50-odd test call sites untouched,
	// and is safe because nothing has consulted the gate yet — it is only ever
	// reached from a run, which cannot start until New returns.
	s.modelCaps = modelCaps
	s.toolCalling = toolcallprobe.NewGate(
		toolcallprobe.WithTrials(cfg.Provider.ToolCallProbeTrials),
		toolcallprobe.WithStore(modelcaps.ProbeStore{S: modelCaps}),
	)
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
	s.embedder = embedder
	s.workspace = cwd
	s.memory = memory.NewSources(cwd, cfg.DataDir)
	s.repoMap = loadRepoMap(cwd, logger)
	_, _ = s.repoMaps.getOrCreate(cwd, func() (string, error) { return s.repoMap, nil })

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
	// no check. Queried directly against the trust store (not
	// cfg.WorkspaceTrust.Trusted) because that field is forced true whenever
	// no project config.yaml exists — a repo could ship only a persona file
	// with no config.yaml and still need gating.
	s.personaDirs = persona.DiscoverDirs(cfg.DataDir, cwd)
	s.personaProjectDir = persona.ProjectDir(cwd)
	s.personaProjectTrusted = workspacetrust.Open(config.WorkspaceTrustStorePath()).IsTrusted(cwd)
	if n, _ := persona.Refresh(s.personaProjectDir, s.personaProjectTrusted, s.personaDirs...); n > 0 {
		logger.Info("loaded custom personas", "count", n)
	}

	s.cmdReg = commands.Discover(commands.CommandDirs(cfg.DataDir, cwd)...)

	token, err := generateAndWriteToken(cfg.AuthTokenPath())
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("auth token: %w", err)
	}
	s.authToken = token

	if cfg.Server.TLS.Enabled {
		cert, err := ensureTLSCert(cfg.TLSCertPath(), cfg.TLSKeyPath())
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("tls cert: %w", err)
		}
		s.tlsCert = &cert
	}

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
		s.compModel = compModel
		// Resolve the effective context window first (P23.1): explicit config,
		// or auto-detected from a local Ollama server — whose OpenAI-compat
		// endpoint otherwise truncates oversized prompts silently.
		s.initContextWindow(context.Background())
		// Keyed to the model compaction actually *runs on*, not the global one
		// (P52.1, second half). Compaction prefers provider.small_model, so
		// tuning the summarizer to the primary model's window is the same
		// wrong-model-window bug P52.1 fixed for the engine, one layer down —
		// and worse here, because the summarizer's own request is what gets
		// silently truncated, producing the broken/empty summary that P39.8's
		// latch exists to stop looping on. When compModel is the global model
		// this is a cache hit and behaves exactly as before.
		win, _ := s.effectiveContextWindowFor(context.Background(), compModel)
		compOpts := compaction.Options{
			// Serve the summarizer's own model with its own num_ctx (P52.4),
			// for the same reason every other run gets one.
			Adapter:       provider.WithNumCtx(adapter, win),
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
			ScanOutput:       m.ScanOutputEnabled(),
			ScanArguments:    m.ScanArguments,
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
		builtin.WithDataDir(cfg.DataDir),
	)); err != nil {
		store.Close()
		return nil, err
	}

	return s, nil
}

// SelectSandbox picks the command-execution backend per cfg.Backend:
// "container" forces a runtime (or auto-detects one); "auto" detects and
// picks the best available; "os" uses OS-level isolation without a container
// runtime; "" or "local" runs commands directly on the host by design.
//
// A fallback to the unsandboxed local backend is a silent security downgrade
// for an operator who believes sandboxing is active (P7.4): it is always
// logged, reported back via the fallback/reason return values (surfaced by
// the caller via /healthz for clients to warn the user), and — when
// cfg.Strict is set — turned into a hard error instead of a silent fallback.
//
// cfg.Backend is expected to already be validated/aliased by
// config.SandboxConfig.Normalize (config.Load calls it, so any cfg built by
// the normal config path is covered); an unrecognized value reaching here
// anyway is rejected rather than silently treated as "local" (P25.2) — a
// container-runtime name typed straight into sandbox.backend (e.g. "podman"
// instead of backend: container + runtime: podman) used to fall through to
// this same default case and run every command unsandboxed on the host with
// no warning at all.
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
		osb, oerr := sandbox.NewOSBackend(cwd, cfg.Network, cfg.StripEnv, cfg.OSExtraReadPaths)
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
	case "", "local":
		return sandbox.NewLocalBackendWithEnv(cfg.StripEnv), false, "", nil
	default:
		return nil, false, "", fmt.Errorf(
			"sandbox: unknown sandbox.backend %q (want \"local\", \"container\", \"auto\", or \"os\"); "+
				"if this names a container runtime, set sandbox.backend: container and sandbox.runtime: %q instead — "+
				"config.Load() normally rejects this before startup, so this cfg was built outside the normal config path",
			cfg.Backend, cfg.Backend,
		)
	}
}

// unsandboxedAutoExecError returns a startup-refusal error when
// permission.auto_approve_exec is set while the effective sandbox backend is
// the unsandboxed local one (P25.2), unless the operator has explicitly
// opted out via permission.allow_unsandboxed_auto_exec. Callers only invoke
// this once they've already established the backend is local (a
// *sandbox.LocalBackend) — this function itself doesn't re-check that, so
// it's a plain function of config rather than needing a sandbox.Backend,
// which keeps it unit-testable without spinning up a real backend.
func unsandboxedAutoExecError(perm config.PermissionConfig, configuredBackend string, sandboxFallback bool, sandboxFallbackReason string) error {
	if perm.AllowUnsandboxedAutoExec {
		return nil
	}
	why := fmt.Sprintf("sandbox.backend is %q", configuredBackend)
	if sandboxFallback {
		why = sandboxFallbackReason
	}
	return fmt.Errorf(
		"refusing to start: permission.auto_approve_exec is enabled but the effective sandbox backend is unsandboxed local execution (%s) — every model-issued shell command would run on the host with no approval and no isolation. "+
			"Configure a real sandbox (sandbox.backend: container or os), or set permission.allow_unsandboxed_auto_exec: true if this is intentional",
		why,
	)
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
	return swarm.NewInProcessBackend(s.subAgentRunner(), s.swarmReg, mailboxRoot, s.cfg.Cost.BudgetUSD, s.cfg.Cost.MaxTokensPerRun)
}

// subAgentRunner returns a swarm.RunFunc that executes a teammate by building a
// sub-engine over the daemon's shared adapter and tools. The child runs with its
// own (clamped) permission mode. Its cost tracker is normally the same shared
// ledger the top-level session run attached to ctx (D1): every sub-agent in a
// fan-out tree, at any depth, draws against one BudgetUSD ceiling instead of
// each spawn getting a fresh allowance — a background/detached spawn whose
// context was severed from the request falls back to a fresh tracker since
// there's no ledger left to share.
//
// FIND-14: when InProcessBackend.Spawn has attached a per-agent budget override
// (this teammate's own guaranteed floor share of the shared pool), the teammate
// runs against a fresh local tracker capped at that share instead of checking
// the shared tracker against the daemon's full cap — otherwise every teammate
// checks the same live aggregate, so one expensive sibling can push it past the
// cap and starve the rest. The local tracker's actual spend folds back into the
// shared ledger when the teammate finishes (mirroring SubprocessBackend's
// AddWorkerCost), so a sibling spawned afterward still sees the updated total.
func (s *Server) subAgentRunner() swarm.RunFunc {
	return func(ctx context.Context, cfg swarm.SpawnConfig) (string, error) {
		if s.adapter == nil {
			return "", fmt.Errorf("no model provider configured")
		}
		model := cfg.Model
		if model == "" {
			model = s.cfg.Provider.Model
		}
		sharedTracker, _ := swarm.CostTrackerFromContext(ctx).(*cost.Tracker)

		tracker := sharedTracker
		budgetUSD := s.cfg.Cost.BudgetUSD
		maxTokensPerRun := s.cfg.Cost.MaxTokensPerRun
		var foldBack *cost.Tracker
		if usd, toks, ok := swarm.BudgetOverrideFromContext(ctx); ok {
			foldBack = cost.NewTracker()
			tracker = foldBack
			budgetUSD, maxTokensPerRun = usd, toks
		} else if tracker == nil {
			tracker = cost.NewTracker()
		}
		// Sub-agents get the same gate stack a top-level run does (contextual
		// egress/network policy, text allow/deny rules) — only the mode gate
		// was ever meaningfully child-specific, and clampMode already confines
		// that above the swarm layer. A bare mode gate here let a spawned
		// teammate route straight around an operator's egress-then-write or
		// deny rule (P10.1).
		gate, engineHooks := s.buildGate(cfg.Mode, s.approver(), persona.Persona{})
		// A spawn can name its own model (swarm.SpawnConfig.Model), which hits
		// the same shared-adapter mismatch a routed turn does — serve it with
		// *its* window, not the primary model's num_ctx (P52.4). Identical to
		// today whenever the spawn runs on the global model.
		spawnWin, _ := s.effectiveContextWindowFor(ctx, model)
		eng, err := engine.New(engine.Options{
			Adapter:   s.modelAdapter(spawnWin),
			Tools:     s.tools,
			Gate:      gate,
			Compactor: s.compactor,
			// A spawn had a Compactor but no window to measure against
			// (ContextWindowTokens was left 0), so the engine's *per-turn*
			// 85%-fill check never ran for a sub-agent however long it grew —
			// only Run's entry-point Compact did, gated by the summarizer's own
			// budget, which is tuned to the compaction model rather than to the
			// model this spawn is running on. Now that the window is resolved
			// per model just above, feed it in and the spawn gets the same
			// proactive protection (and the same nothing-left-to-compact notice)
			// a top-level run has.
			ContextWindowTokens: spawnWin,
			Hooks:               engineHooks,
			Cost:                tracker,
			BudgetUSD:           budgetUSD,
			MaxTokensPerRun:     maxTokensPerRun,
			// A spawned teammate inherits the operator's time bound rather than
			// getting its own share of it, unlike the cost/token floors above:
			// those are divisible (spend is additive across siblings), elapsed
			// time is not — teammates run concurrently, so "N minutes" means the
			// same N minutes for each of them.
			MaxWallClockPerRun: s.cfg.Cost.MaxWallClockPerRun(),
			Model:              model,
			MaxTokens:          s.cfg.Provider.MaxTokens,
			// Same model server as the parent session, so the same tool-calling
			// fallback (P53.6) — a shimmed session whose spawns weren't shimmed
			// would hand every teammate a model that can't call a tool.
			ToolCallShim: s.cfg.Provider.ToolCallShimEnabled(),
			Logger:       s.logger,
			// Set explicitly from cfg.Workdir (P25.8) rather than relying on
			// the parent session's tool.WithWorkdir ctx value leaking through
			// the spawn's context chain — that accidental inheritance only
			// ever reached foreground in-process spawns; a detached/
			// background spawn's job runs under a context derived from
			// context.Background() (task.Manager.Start) and loses it.
			Workdir: cfg.Workdir,
			// A spawn is confined the same way its parent session is: the
			// additional roots are a property of the workspace, not of who
			// is asking (P52.13).
			ExtraRoots: s.workspaceRootsFor(cfg.Workdir),
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
		// Fold this teammate's actual spend back into the shared ledger, so a
		// sibling spawned afterwards computes its own share against a total
		// that includes this one (FIND-14).
		if foldBack != nil && sharedTracker != nil {
			sharedTracker.AddWorkerCost(foldBack.TotalUSD(), foldBack.TotalTokens())
		}
		return strings.TrimSpace(sb.String()), runErr
	}
}

// newWithDeps assembles a Server from explicit dependencies. It is the seam
// used by tests to inject a mock adapter and an in-memory store.
func newWithDeps(cfg *config.Config, logger *slog.Logger, store *session.Store, adapter provider.Adapter, tools *tool.Registry) *Server {
	// The gate probes once inline (the message path's blocking verdict) and
	// refines the rest of the P53.4 conformance sample in the background, so
	// provider.tool_call_probe_trials never costs first-message latency.
	s := &Server{cfg: cfg, store: store, adapter: adapter, tools: tools, logger: logger, runs: newRunRegistry(),
		toolCalling: toolcallprobe.NewGate(toolcallprobe.WithTrials(cfg.Provider.ToolCallProbeTrials))}
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
		// Session-scoped knowledge stores opened for a non-default Workdir
		// (P25.9) — s.knowledge above only covers the daemon's own workspace.
		for _, ks := range s.knowledgeStores.values() {
			_ = ks.Close()
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
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
// identity, sandbox fallback state (same fields as /healthz), the
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
