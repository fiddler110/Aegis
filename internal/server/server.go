// Package server is the Aegis daemon. It owns the session store, the model
// adapter, the tool registry, and runs the agent engine, exposing everything
// over a local HTTP API (with server-sent events for streaming runs).
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
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
	"github.com/fiddler110/aegis/internal/opregister"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/fiddler110/aegis/internal/toolcallprobe"
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
	// opRegister is the durable P65.4 record of tool calls that started and
	// haven't finished — the cross-process half of the engine's in-memory
	// startedTools (P65.1). Consulted per turn in newEngine so a session
	// resumed after a daemon restart classifies an orphaned tool_use as "may
	// have run" instead of unconditionally "never started".
	opRegister  *opregister.Store
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

	// daemonCtx is cancelled when ListenAndServe's caller shuts the daemon
	// down. Background work spawned off the request path (goroutines that
	// must outlive the handler that started them, like the async checkpoint
	// git-SHA capture in messages.go) derives its own bounded context from
	// this one instead of context.Background(), so it is cancelled early on
	// graceful shutdown rather than only bounded by its own timeout.
	// Defaults to context.Background() in newWithDeps so tests that never
	// call ListenAndServe still get a valid, non-nil context.
	daemonCtx context.Context

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
	// missing or mismatched bearer token, plus rejections from the
	// authMiddleware-exempt POST /auth/exchange handler (FIND-11, extended by
	// P63.5 — one cumulative counter keeps the log cadence coherent across
	// both, while only the middleware's failures feed the lockout streak
	// below). It is a single
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
	// pageTokenCapWarned suppresses a repeat of the maxPageTokens warning
	// while the cap stays engaged, so a flood produces one diagnosable line
	// per episode rather than one per request. Cleared as soon as a mint
	// succeeds again. Guarded by pageTokenMu.
	pageTokenCapWarned bool

	// mintLimiters holds the P81.16 per-remote-address token buckets that sit
	// *in front of* the maxPageTokens cap. The cap alone is a memory bound,
	// not a fairness one: because refusing (rather than evicting) is the
	// deliberate posture there, anything that can drive 1024 unexchanged
	// mints inside pageTokenTTL locks the operator out of their own UI. The
	// limiter makes that cost the flooder its own bucket instead of the
	// shared cap. Bounded to maxMintLimiters entries and swept of idle ones,
	// so the anti-DoS control cannot itself become the memory-growth DoS an
	// attacker varying source addresses would otherwise get for free.
	mintLimitMu  sync.Mutex
	mintLimiters map[string]*mintLimiter

	// sandboxFallback and sandboxFallbackReason record whether the configured
	// sandbox backend failed to initialize and the daemon fell back to
	// unsandboxed local execution (P7.4). Surfaced via the authenticated
	// /status (never /healthz, which needs no credential) so clients can
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
	// autofitWin holds the windows the P72.1 boot fit solved for, per model. It
	// is not a second answer to "what window is served" — that stays the ctxWin
	// entries — but the *asked-for* number, which is what reconciliation compares
	// a detection against. See configWindowFor (contextwindow.go, autofit.go).
	// Guarded by ctxWinMu.
	autofitWin map[string]int
	// weightsSeen caches each model's measured resident weight bytes. Weights do
	// not change with the window, but the *derivation* stops working at a large
	// one for sliding-window models — see recordWeights (autofit.go). Guarded by
	// ctxWinMu.
	weightsSeen map[string]int64
	ollamaBase  string // native Ollama API base when (possibly) Ollama; "" otherwise
	summarizer  *compaction.Summarizer
	// compModel is the model compaction runs on (provider.small_model when set,
	// otherwise the global model). The summarizer is retuned from *this* model's
	// window, not the global one — see setWindowLocked (P52.1).
	compModel string

	// residentSetSem serializes resident-set claims (P69.6, residentset.go).
	// Cap 1: two debates planning windows at once on one GPU would each install
	// the other's models out from under it. Lazily created via residentSetGate
	// so a Server built field-by-field in a test is not a nil-channel deadlock.
	residentSetMu  sync.Mutex
	residentSetSem chan struct{}

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

	// promptSectionCache memoizes the *stable* system-prompt sections per
	// session (P67.2): sessionID → *sync.Map keyed by promptSectionKey. Only
	// sections declared with stableSection land here; a volatile one is
	// recomputed every turn by design. Cleared for a session in
	// handleDeleteSession alongside the other per-session maps, which is what
	// keeps it from growing without bound in a long-lived daemon.
	promptSectionCache sync.Map // string → *sync.Map

	// closeOnce makes Close idempotent: ListenAndServe defers it, and an
	// embedder driving the Server through Handler() calls it directly, so both
	// can happen in one process without double-closing a database.
	closeOnce sync.Once
}

// New constructs a daemon from config. The workspace root for tools is the
// process working directory.
//
// It is a sequence of wire* stages, several of which take a durable resource —
// an open SQLite handle, a spawned language-server or MCP-server process, a
// persistent workspace container, an audit file — and each of which can fail.
//
// The err return is named so one deferred rollback can undo whatever came up
// before the failure. Before this, each error path hand-wrote its own cleanup
// and every one of them was the cleanup that was correct when that stage was
// added: a wireTools failure closed only the session store, stranding the
// sandbox, the language servers and both recall stores; a wireSwarm failure —
// the last stage, so the one with the most behind it — additionally stranded
// the spawned MCP server processes and the audit file handle. Only
// wireSecurityWarnings ever closed the knowledge/long-memory pair, because it
// happened to be the stage someone debugged.
//
// The success path deliberately registers nothing: a Server handed back to the
// caller owns its subsystems, and ListenAndServe's own shutdown sequence closes
// them when the daemon stops. This is strictly the undo for a constructor that
// never returned one.
//
// Note the `err =` rather than `err :=` at each stage below. A shadowed error
// in an `if err := ...` would leave the named return nil, and the rollback
// would silently never run.
func New(cfg *config.Config, logger *slog.Logger) (_ *Server, err error) {
	var rollback []func()
	// onFailure registers a teardown for a resource just acquired. Order is
	// reverse-acquisition, so a stage never tears down something a later stage
	// still holds a reference to.
	onFailure := func(undo func()) { rollback = append(rollback, undo) }
	defer func() {
		if err == nil {
			return
		}
		for i := len(rollback) - 1; i >= 0; i-- {
			rollback[i]()
		}
	}()

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
	// P67.6: same reasoning as the shim warning above — an unparseable duration
	// falls back to the default, which is indistinguishable from not setting it.
	if _, ok := cfg.Compaction.ColdCacheAfterOr(); !ok {
		logger.Warn("compaction.cold_cache_after is not a duration; using the default",
			"value", cfg.Compaction.ColdCacheAfter, "default", config.ColdCacheAfterDefault,
			"expected", `a Go duration such as "20m", or "off"`)
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
	onFailure(func() { store.Close() })

	// s is constructed here (P78.9), right after its one prerequisite (store),
	// instead of ~250 lines into the function as it was pre-P78.9. adapter and
	// tools start nil and are filled in by wireProvider/wireTools below once
	// they exist; every other subsystem is filled in by exactly one wire*
	// method call further down. The payoff: every wire* method runs against a
	// real, already-addressable *s*, so none of them need the "var s *Server;
	// closure reads s lazily at call time" forward-reference trick the cron
	// RunFunc and the knowledge-provider closure used to require — cronPermCheck/
	// cronNotify/knowledgeStoreFor are now passed as plain method values.
	s := newWithDeps(cfg, logger, store, nil, nil)

	// Per-model capability cache (P53.5): opened before the adapter so the
	// Ollama adapter starts with the `think`-rejection latches a previous
	// process already paid for, and reconciled against the models actually
	// present so a re-pulled tag can't inherit the old weights' verdicts.
	modelCaps := cfg.OpenModelCaps()
	reconcileModelCaps(cfg, modelCaps, logger)
	s.wireProvider(cfg, modelCaps, logger)

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	s.workspace = cwd

	if err = s.wireCoreStores(cfg, logger); err != nil {
		return nil, err
	}

	sb, sandboxFallback, sandboxFallbackReason, sbErr := SelectSandbox(cfg.Sandbox, cwd, logger)
	if sbErr != nil {
		err = sbErr
		return nil, err
	}
	s.sandbox = sb
	s.sandboxFallback = sandboxFallback
	s.sandboxFallbackReason = sandboxFallbackReason
	// A Docker/Podman backend may already hold a persistent per-workspace
	// container by this point, which outlives the process that started it.
	onFailure(func() { s.sandbox.Close() })

	if err = s.wireCron(cwd, sb, logger); err != nil {
		return nil, err
	}

	s.wireLSP(cfg, cwd, logger)
	// Each configured language server is a spawned child process.
	onFailure(func() {
		if s.lspMgr != nil {
			s.lspMgr.Close()
		}
	})

	// Semantic recall layer (P5.8): opt-in; nil embedder keeps both stores
	// BM25-only, which is the zero-config default.
	s.embedder = embed.New(cfg.Embeddings.Enabled, cfg.Embeddings.Provider, cfg.Embeddings.Model, cfg.Embeddings.BaseURL)
	s.wireKnowledgeAndMemory(cfg, cwd, logger)
	onFailure(func() {
		if s.knowledge != nil {
			_ = s.knowledge.Close()
		}
		if s.longMem != nil {
			_ = s.longMem.Close()
		}
	})

	if err = s.wireTools(cfg, cwd, sb, logger); err != nil {
		return nil, err
	}

	if err = s.wireSecurityWarnings(cfg, logger); err != nil {
		return nil, err
	}

	// Hand the tool-calling gate the same capability store the adapter got, so
	// a probe verdict measured in an earlier process is reused instead of
	// re-run (P53.5). Replacing the gate here (rather than parameterising
	// newWithDeps) keeps that constructor's 50-odd test call sites untouched,
	// and is safe because nothing has consulted the gate yet — it is only ever
	// reached from a run, which cannot start until New returns.
	s.modelCaps = modelCaps
	s.toolCalling = toolcallprobe.NewGate(
		toolcallprobe.WithTrials(cfg.Provider.ToolCallProbeTrials),
		toolcallprobe.WithStore(modelcaps.ProbeStore{S: modelCaps}),
	)

	s.wirePermissionRules(cfg, logger)

	s.memory = memory.NewSources(cwd, cfg.DataDir)
	s.repoMap = loadRepoMap(cwd, repoMapOptions(cfg), logger)
	_, _ = s.repoMaps.getOrCreate(cwd, func() (string, error) { return s.repoMap, nil })

	s.wirePersonasAndCommands(cfg, cwd, logger)

	if err = s.wireAuthAndTLS(cfg); err != nil {
		return nil, err
	}

	s.wireHooks(cfg, logger)
	// The audit sink is an open file the daemon appends policy decisions to.
	onFailure(func() {
		if s.audit != nil {
			_ = s.audit.Close()
		}
	})

	if s.adapter != nil {
		s.wireCompaction(cfg)
	}

	s.wireMCP(cfg, logger)
	// Each MCP client is a spawned server process (stdio transport) or a live
	// connection. wireSwarm below is the last stage, so without this a failure
	// there left every configured MCP server running with nothing attached.
	onFailure(func() {
		for _, c := range s.mcpClients {
			_ = c.Close()
		}
	})

	if err = s.wireSwarm(cfg, logger); err != nil {
		return nil, err
	}

	return s, nil
}

// newWithDeps assembles a Server from explicit dependencies. It is the seam
// used by tests to inject a mock adapter and an in-memory store.
func newWithDeps(cfg *config.Config, logger *slog.Logger, store *session.Store, adapter provider.Adapter, tools *tool.Registry) *Server {
	// The gate probes once inline (the message path's blocking verdict) and
	// refines the rest of the P53.4 conformance sample in the background, so
	// provider.tool_call_probe_trials never costs first-message latency.
	s := &Server{cfg: cfg, store: store, adapter: adapter, tools: tools, logger: logger, runs: newRunRegistry(),
		toolCalling: toolcallprobe.NewGate(toolcallprobe.WithTrials(cfg.Provider.ToolCallProbeTrials)),
		daemonCtx:   context.Background()}
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
