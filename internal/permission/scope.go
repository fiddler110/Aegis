package permission

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/fiddler110/aegis/internal/tool"
)

// TaskScope is a per-session, mutable allowlist of workspace-relative path
// globs that file-writing tool calls must stay within for the duration of a
// task (P46.1). It is the mechanical backing for "this task should only touch
// these files": a skill or the model pushes a scope via the `scope` tool at the
// start of a task and clears it when the task ends, and ScopeGate denies any
// write whose path falls outside it in between.
//
// Unlike the session-lifetime text rules (RuleGate) parsed once at load, a
// TaskScope is set and cleared at will during a run, giving blast-radius
// containment at a finer grain than the whole-session mode/rule gates. A nil or
// inactive (no patterns) TaskScope imposes no restriction at all, so the gate
// is a no-op until a scope is explicitly declared.
type TaskScope struct {
	mu       sync.Mutex
	patterns []string         // raw globs, for display
	res      []*regexp.Regexp // compiled, path-normalized matchers
}

// NewTaskScope returns an empty (inactive) scope.
func NewTaskScope() *TaskScope { return &TaskScope{} }

// Set replaces the scope's allowlist with the given path globs. An empty list
// clears the scope (same as Clear). Globs use the same syntax as permission
// rules: "*"/"?" wildcards, "*" spanning path separators (so "src/**" and
// "src/*" both match "src/a/b.go"). Patterns and the checked path are both
// path-normalized (separator unification, ".."/"." cleanup, case-folding on
// case-insensitive filesystems) so a traversal or case trick can't dodge the
// scope, mirroring RuleGate's Read/Write matching.
func (s *TaskScope) Set(patterns []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patterns = s.patterns[:0]
	s.res = s.res[:0]
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		s.patterns = append(s.patterns, p)
		s.res = append(s.res, globToRegexp(normalizePathLike(p)))
	}
}

// Clear deactivates the scope (removes all patterns).
func (s *TaskScope) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patterns = nil
	s.res = nil
}

// Active reports whether a scope restriction is currently in force.
func (s *TaskScope) Active() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.res) > 0
}

// Patterns returns a copy of the raw glob list (for display / audit).
func (s *TaskScope) Patterns() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.patterns...)
}

// Allowed reports whether path is within the current scope. An inactive scope
// allows everything.
func (s *TaskScope) Allowed(path string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.res) == 0 {
		return true
	}
	np := normalizePathLike(path)
	for _, re := range s.res {
		if re.MatchString(np) {
			return true
		}
	}
	return false
}

// scopeCtxKey is the context key carrying a session's *TaskScope so that both
// the `scope` tool (which mutates it) and ScopeGate (which reads it) reach the
// same object — the engine passes one context into gate.Check and tool.Execute
// alike, so a scope set by one turn's tool call is enforced on the next.
type scopeCtxKey struct{}

// WithTaskScope attaches a TaskScope to ctx.
func WithTaskScope(ctx context.Context, s *TaskScope) context.Context {
	return context.WithValue(ctx, scopeCtxKey{}, s)
}

// TaskScopeFromContext returns the TaskScope carried by ctx, or nil.
func TaskScopeFromContext(ctx context.Context) *TaskScope {
	s, _ := ctx.Value(scopeCtxKey{}).(*TaskScope)
	return s
}

// ScopeGate wraps a base gate and denies any file-writing tool call whose
// target path falls outside the active TaskScope carried on the context
// (P46.1). It is meant to be the outermost gate so an out-of-scope write is
// refused even when a text allow-rule would otherwise permit it — the scope is
// a further restriction on top of the session's standing permissions, not a
// competing grant. When no scope is active (the default), it is a transparent
// pass-through to base.
type ScopeGate struct {
	base       checker
	onDecision func(ContextualDecision)
}

// NewScopeGate wraps base with per-task scope enforcement.
func NewScopeGate(base checker, onDecision func(ContextualDecision)) *ScopeGate {
	return &ScopeGate{base: base, onDecision: onDecision}
}

// Check implements engine.Gate.
func (g *ScopeGate) Check(ctx context.Context, t tool.Tool, input json.RawMessage) (bool, string) {
	sc := TaskScopeFromContext(ctx)
	// Gate on the per-call effective capability, not the tool's static one
	// (P63.3, following P32.2 for ContextualGate): a tool that reclassifies
	// into CapWrite for a specific call via CapabilityFor must still be
	// confined by the scope, which is the outermost containment gate.
	if sc.Active() && tool.EffectiveCapability(ctx, t, input) == tool.CapWrite {
		for _, p := range scopeWritePaths(input) {
			if !sc.Allowed(p) {
				reason := t.Name() + " blocked: path " + p + " is outside the declared task scope (" + strings.Join(sc.Patterns(), ", ") + ")"
				if g.onDecision != nil {
					g.onDecision(ContextualDecision{
						Tool: t.Name(), Cap: string(tool.CapWrite),
						Rule: "task_scope", Decision: Deny, Reason: reason,
					})
				}
				return false, reason
			}
		}
	}
	return g.base.Check(ctx, t, input)
}

// scopeWritePaths extracts the workspace-relative paths a write-capability tool
// call would modify: the top-level "path"/"file_path" fields and each
// "edits[].path" (multi_edit). A tool with none of these — e.g. git_commit,
// whose subject is a commit message, or remember — contributes no paths and is
// therefore never scope-restricted; scope is about which files get their
// contents rewritten, not about every write-capability action.
func scopeWritePaths(input json.RawMessage) []string {
	if len(input) == 0 {
		return nil
	}
	var args struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(input, &args) != nil {
		return nil
	}
	var paths []string
	if args.Path != "" {
		paths = append(paths, args.Path)
	}
	if args.FilePath != "" {
		paths = append(paths, args.FilePath)
	}
	// editPathsFromInput (rules.go) is shared with the rule gate on purpose: the
	// scope gate handled edits[] and subjectFor did not, and that divergence is
	// what let a deny rule miss multi_edit entirely.
	return append(paths, editPathsFromInput(input)...)
}
