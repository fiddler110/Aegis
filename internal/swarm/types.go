// Package swarm provides multi-agent coordination: identities, a durable
// file-based mailbox, a team registry, and pluggable backends that launch
// sub-agents ("teammates"). It is a pure coordination layer — it does not import
// the engine; the caller supplies a RunFunc that knows how to execute a
// sub-agent to completion.
package swarm

import "context"

// BackendType names a teammate execution backend.
type BackendType string

const (
	// BackendInProcess runs a teammate as a goroutine in the current process.
	BackendInProcess BackendType = "in_process"
	// BackendSubprocess runs a teammate as a separate headless process.
	BackendSubprocess BackendType = "subprocess"
)

// Identity names a teammate within a team.
type Identity struct {
	AgentID         string // "<name>@<team>"
	Name            string
	Team            string
	ParentSessionID string
}

// NewIdentity builds an Identity, defaulting an empty team to "default".
func NewIdentity(name, team, parentSessionID string) Identity {
	if team == "" {
		team = "default"
	}
	return Identity{
		AgentID:         name + "@" + team,
		Name:            name,
		Team:            team,
		ParentSessionID: parentSessionID,
	}
}

// SpawnConfig describes a teammate to launch.
type SpawnConfig struct {
	Name            string // teammate name (a unique id is derived from it)
	Team            string // team to join; empty -> "default"
	Prompt          string // the task for the teammate
	SystemPrompt    string // system prompt (from an agent definition)
	Mode            string // permission mode for the child (must be <= parent)
	Model           string // model override; empty -> daemon default
	ParentSessionID string
	Depth           int // spawn depth, for recursion guards
}

// Result is a finished teammate's output.
type Result struct {
	AgentID string
	Output  string
	Err     string // non-empty if the teammate failed
}

// Failed reports whether the teammate ended in error.
func (r Result) Failed() bool { return r.Err != "" }

// Backend launches and manages teammates.
type Backend interface {
	// Spawn launches a teammate and returns a handle to await its result.
	Spawn(ctx context.Context, cfg SpawnConfig) (*Handle, error)
	// Shutdown waits for in-flight teammates to finish (or ctx to cancel).
	Shutdown(ctx context.Context)
	// OnStop registers a listener invoked when a teammate finishes (the
	// SUBAGENT_STOP lifecycle event). It must be set before any Spawn call.
	OnStop(fn func(id Identity, res Result))
}

// RunFunc executes a teammate to completion and returns its final text output.
// The caller (the daemon) supplies an implementation that builds and runs a
// sub-engine; swarm itself stays decoupled from the engine.
type RunFunc func(ctx context.Context, cfg SpawnConfig) (string, error)

// Handle tracks one spawned teammate.
type Handle struct {
	Identity Identity
	done     chan Result
}

// Wait blocks until the teammate finishes or ctx is cancelled.
func (h *Handle) Wait(ctx context.Context) (Result, error) {
	select {
	case r := <-h.done:
		return r, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// --- spawn-depth context plumbing (recursion guard) ---

type depthKey struct{}

// WithDepth returns a context carrying the given spawn depth.
func WithDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, depthKey{}, depth)
}

// DepthFromContext returns the spawn depth carried by ctx (0 if none).
func DepthFromContext(ctx context.Context) int {
	if d, ok := ctx.Value(depthKey{}).(int); ok {
		return d
	}
	return 0
}

type parentModeKey struct{}

// WithParentMode returns a context carrying the caller's permission mode, used
// to clamp a spawned child so it cannot exceed the parent's posture.
func WithParentMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, parentModeKey{}, mode)
}

// ParentModeFromContext returns the caller's permission mode (empty if none).
func ParentModeFromContext(ctx context.Context) string {
	if m, ok := ctx.Value(parentModeKey{}).(string); ok {
		return m
	}
	return ""
}
