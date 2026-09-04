// Session-scoped views of the daemon's shared state: the per-session tool
// registry clone, task scope, workdir, and the per-root caches those imply.
// Extracted from server.go (L4) — every accessor here answers the same
// question, "which of these does *this* session see", and they are the reason
// several of Server's sync.Maps exist.
package server

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/reqorigin"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/fiddler110/aegis/internal/webcache"
)

// sessionMaps groups the sync.Map family keyed by session ID (P80.3 — see
// the CLAUDE.md note on Server's field count). Each map is independent and
// keeps its own zero-value-is-ready sync.Map semantics; grouping them here
// is purely organizational; it does not add a lock or change concurrency
// behavior. tools, taskScopes, workdirs, skills and webCache are keyed by
// session ID directly; promptCache maps session ID to a *sync.Map keyed by
// promptSectionKey; permCache is keyed by "sessionID\x00toolName".
type sessionMaps struct {
	// tools maps session ID → a *tool.Registry clone of s.tools (P9).
	// tool_search exposes deferred tools by mutating a registry's exposed map;
	// without a per-session clone, that mutation was permanent and
	// process-wide, silently exposing a tool's schema to every other
	// concurrent or future session and persona sharing the daemon's one
	// registry. Lazily created on first use per session and reused across
	// that session's turns so a loaded tool stays loaded turn to turn.
	tools sync.Map // string → *tool.Registry

	// taskScopes maps session ID → that session's *permission.TaskScope
	// (P46.1), the per-task file-write allowlist the `scope` tool mutates and
	// ScopeGate enforces. Lazily created per session and reused across its
	// turns so a scope set during one turn stays in force until the model
	// clears it.
	taskScopes sync.Map // string → *permission.TaskScope

	// workdirs maps session ID → that session's own working directory
	// (P25.1), resolved and validated once at creation time in
	// handleCreateSession. Missing/empty means the session uses the
	// daemon's default workspace (s.workspace) — see workdirFor.
	workdirs sync.Map // string → string

	// skills maps session ID → extra embedded built-in skill names activated
	// on demand for that session (e.g. via /threat-model), layered on top of
	// the persistent cfg.Skills.BuiltinEnabled list. In-memory only: it
	// resets on daemon restart and never touches config, so built-ins stay
	// dormant by default and are only ever pulled in by an explicit request.
	skills sync.Map // string → []string

	// promptCache memoizes the *stable* system-prompt sections per session
	// (P67.2): sessionID → *sync.Map keyed by promptSectionKey. Only
	// sections declared with stableSection land here; a volatile one is
	// recomputed every turn by design. Cleared for a session in
	// handleDeleteSession alongside the other per-session maps, which is what
	// keeps it from growing without bound in a long-lived daemon.
	promptCache sync.Map // string → *sync.Map

	// permCache maps "sessionID\x00toolName" → struct{} for tools the user
	// has approved with "allow always" during the current daemon lifetime.
	permCache sync.Map

	// sems serializes runs within a session. Each session maps to a buffered
	// channel of size 1; acquiring it blocks until the prior run finishes.
	sems sync.Map // string → chan struct{}

	// webCache maps session ID → that session's *webcache.Cache (P71.6):
	// web_fetch/web_search memoization, so a URL or query already seen this
	// session is served from memory instead of re-issued — the mechanism the
	// deep-research skill's audit trail always claimed to have. Lazily
	// created per session and reused across its turns, exactly like tools,
	// so a page fetched in turn 1 is still cached after a compaction has
	// erased the model's own memory of fetching it. Cleared for a session in
	// handleDeleteSession alongside the other per-session maps.
	webCache sync.Map // string → *webcache.Cache
}

// activateSessionSkill turns on a built-in skill for one session: it's added
// to that session's extra-enabled set (read by effectiveSystem for the
// <skills_available> index) and the session's tool registry clone gets an
// updated skill tool so the `skill` tool can load it immediately, without
// waiting for a restart or writing to config.
func (s *Server) activateSessionSkill(id, name string) {
	var extra []string
	if v, ok := s.sess.skills.Load(id); ok {
		extra = v.([]string)
	}
	for _, n := range extra {
		if strings.EqualFold(n, name) {
			return
		}
	}
	extra = append(append([]string{}, extra...), name)
	s.sess.skills.Store(id, extra)

	workdir := s.workdirFor(id)
	enabled := append(append([]string{}, s.cfg.Skills.BuiltinEnabled...), extra...)
	s.sessionToolRegistry(id).Upsert(builtin.NewSkillTool(workdir, s.cfg.DataDir, enabled))

	// P39.18: a skill that bundles scripts also brings typed tools for them, so
	// its body can say "call threat_model_scaffold with framework=stride"
	// instead of asking the model to compose a python command line. Registered
	// with the activation, on the session's own clone, so the daemon-wide
	// surface never grows for sessions that don't use the skill.
	if sk, ok := skills.Load(workdir, s.cfg.DataDir, enabled, name); ok {
		for _, t := range builtin.ThreatModelScriptTools(workdir, sk.Dir) {
			s.sessionToolRegistry(id).Upsert(t)
		}
	}
}

// sessionEnabledSkills returns the persistent config-level enabled built-ins
// plus any activated on demand for this session.
func (s *Server) sessionEnabledSkills(id string) []string {
	enabled := append([]string{}, s.cfg.Skills.BuiltinEnabled...)
	if v, ok := s.sess.skills.Load(id); ok {
		enabled = append(enabled, v.([]string)...)
	}
	return enabled
}

// sessionToolRegistry returns the session-scoped tool registry clone for id,
// creating one from s.tools on first use.
//
// The Load fast path is not a micro-optimization (P66.4/ARCH-11):
// LoadOrStore's argument is evaluated eagerly, and workdirFor/toolRegistryFor/
// effectiveSystem call this on every prompt build and every drive phase — so
// every call was cloning ~60 entries under a read lock on the global registry
// and immediately discarding the result.
func (s *Server) sessionToolRegistry(id string) *tool.Registry {
	if v, ok := s.sess.tools.Load(id); ok {
		return v.(*tool.Registry)
	}
	v, _ := s.sess.tools.LoadOrStore(id, s.tools.Clone())
	return v.(*tool.Registry)
}

// subAgentToolRegistry returns the registry a spawned teammate should run
// against: a fresh clone of its parent session's registry, so the teammate
// starts from the parent's exposure and session-scoped tools but its own
// tool_search loads stay its own (P66.4/ARCH-02).
//
// A spawn with no parent session — a detached background job whose session
// context was severed — falls back to a clone of the daemon-wide registry.
// Still a clone, not s.tools: an unparented teammate has even less claim to
// mutate global exposure than a parented one.
func (s *Server) subAgentToolRegistry(parentSessionID string) *tool.Registry {
	if parentSessionID == "" {
		return s.tools.Clone()
	}
	return s.sessionToolRegistry(parentSessionID).Clone()
}

// sessionWebCacheFor returns the session's *webcache.Cache (P71.6), creating
// an empty one on first use so web_fetch/web_search share the same cache
// across a session's turns the way sessionToolRegistry shares a registry
// clone.
func (s *Server) sessionWebCacheFor(id string) *webcache.Cache {
	if v, ok := s.sess.webCache.Load(id); ok {
		return v.(*webcache.Cache)
	}
	v, _ := s.sess.webCache.LoadOrStore(id, webcache.New())
	return v.(*webcache.Cache)
}

// taskScopeFor returns the session's per-task file-write scope (P46.1),
// creating an empty (inactive) one on first use so the `scope` tool and
// ScopeGate share the same object across the session's turns.
func (s *Server) taskScopeFor(id string) *permission.TaskScope {
	v, _ := s.sess.taskScopes.LoadOrStore(id, permission.NewTaskScope())
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
		if v, ok := s.sess.workdirs.Load(id); ok {
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

// repoMapRecheckInterval bounds how often a prompt build may re-ask whether the
// repository map has gone stale (P66.20/PERF-04). The daemon used to load the
// map once at construction and inject that value for its whole lifetime, so a
// long-lived daemon described the repository as it looked at startup — every
// file the agent itself created was missing from the overview it was reasoning
// against.
//
// loadRepoMap already does the right thing (fingerprint, rebuild only when
// stale); it was simply never called again. The staleness check is a stat-only
// walk measured at ~11.5ms against this repository, versus ~185ms for a full
// rebuild and a turn measured in seconds, so re-asking is affordable — but a
// turn is not the only thing that builds a prompt and several may land in a
// burst, so the answer is reused for a few seconds. Short enough that a file
// written by one turn is in the map by the next, long enough that a rapid
// sequence of turns pays for one walk rather than one each.
const repoMapRecheckInterval = 5 * time.Second

// repoMapFor returns the cached repository-map system-prompt block for
// root (P25.9), lazily loading it on first use for a root other than the
// daemon's own default workspace and re-checking staleness at most once per
// repoMapRecheckInterval.
//
// The primary workspace keeps its own field rather than joining the rootCache
// below because POST /repomap/index writes it directly (P14.3): an explicit
// `/index` must still publish its rebuild immediately, and it does — the
// refresh here starts from whatever that handler last stored and re-checks it
// against the same on-disk cache the handler saved.
func (s *Server) repoMapFor(root string) string {
	if root == "" || root == s.workspace {
		s.repoMapMu.Lock()
		defer s.repoMapMu.Unlock()
		if time.Since(s.repoMapCheckedAt) >= repoMapRecheckInterval {
			s.repoMapCheckedAt = time.Now()
			s.repoMap = loadRepoMap(s.workspace, repoMapOptions(s.cfg), s.logger)
		}
		return s.repoMap
	}
	e, err := s.repoMaps.getOrCreate(root, func() (*repoMapEntry, error) {
		return &repoMapEntry{}, nil
	})
	if err != nil {
		return ""
	}
	return e.block(root, repoMapOptions(s.cfg), s.logger)
}

// repoMapEntry is one secondary workdir's cached map block plus the clock that
// bounds its staleness re-check. The rootCache holds the entry — created once,
// never replaced — and the entry's own mutex guards the value, so a slow
// rebuild for one root never blocks a lookup for another (rootCache's own
// mutex is shared across every root it holds).
type repoMapEntry struct {
	mu        sync.Mutex
	rendered  string
	loaded    bool
	checkedAt time.Time
}

func (e *repoMapEntry) block(root string, opts repomap.Options, logger *slog.Logger) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.loaded || time.Since(e.checkedAt) >= repoMapRecheckInterval {
		e.loaded = true
		e.checkedAt = time.Now()
		e.rendered = loadRepoMap(root, opts, logger)
	}
	return e.rendered
}

// workdirAllowed reports whether workdir (already resolved to an absolute
// path) is permitted for a session on this daemon (P25.1, P81.9).
//
// The allowlist used to be skipped entirely on the default loopback-only
// bind, on the reasoning that a local token holder is already as trusted as
// a local shell user. That reasoning holds for the operator's own shell and
// for nothing else reachable on a loopback daemon: an MCP client, an editor
// plugin over ACP, and a scheduled job all authenticate with the same
// bearer token without the operator having chosen a directory. The
// allowlist is now applied unconditionally, with the exemption re-targeted
// at what actually deserves it — origin, not bind address. TUI and CLI are
// interactive, operator-driven local-shell surfaces (see reqorigin's doc
// comment); Web, ACP and MCP are not, and get no free pass regardless of how
// the daemon is bound.
func (s *Server) workdirAllowed(workdir, origin string) bool {
	if origin == reqorigin.TUI || origin == reqorigin.CLI {
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
	// Reuse the same trust grant workspace.additional_roots already asks
	// for (config.WorkspaceTrusted/TrustWorkspace) rather than inventing a
	// second consent mechanism: an operator who has already run
	// `aegis trust --dir <path>` for a directory has made exactly the
	// judgment call this gate exists to require.
	return config.WorkspaceTrusted(workdir)
}

// withinRoot reports whether target equals root or is nested under it. Both
// arguments must already be absolute, cleaned paths.
//
// CLN-1: the body is sandbox.WithinRoot — same lexical comparison, same
// Windows case-folding, same "root itself counts as inside". This stays as a
// named local so the two call sites below read the way they did.
func withinRoot(root, target string) bool {
	return sandbox.WithinRoot(root, target)
}
