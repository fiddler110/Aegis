// Session-scoped views of the daemon's shared state: the per-session tool
// registry clone, task scope, workdir, and the per-root caches those imply.
// Extracted from server.go (L4) — every accessor here answers the same
// question, "which of these does *this* session see", and they are the reason
// several of Server's sync.Maps exist.
package server

import (
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

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
	if v, ok := s.sessionSkills.Load(id); ok {
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
	if v, ok := s.sessionTools.Load(id); ok {
		return v.(*tool.Registry)
	}
	v, _ := s.sessionTools.LoadOrStore(id, s.tools.Clone())
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
		return loadRepoMap(root, repoMapOptions(s.cfg), s.logger), nil
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
//
// CLN-1: the body is sandbox.WithinRoot — same lexical comparison, same
// Windows case-folding, same "root itself counts as inside". This stays as a
// named local so the two call sites below read the way they did.
func withinRoot(root, target string) bool {
	return sandbox.WithinRoot(root, target)
}
