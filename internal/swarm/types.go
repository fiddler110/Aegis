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
	// CheckpointID is the parent turn's checkpoint, if any (P9). The
	// in-process backend already captures a sub-agent's file writes for free
	// via ctx (checkpoint.WithSnapshotter); the subprocess backend can't —
	// a subprocess starts a whole separate process with its own ctx tree —
	// so this id crosses that boundary explicitly, letting the worker
	// reconstruct an equivalent Snapshotter of its own.
	CheckpointID string
}

// Result is a finished teammate's output.
type Result struct {
	AgentID string
	Output  string
	Err     string // non-empty if the teammate failed

	// CostUSD and Tokens are the teammate's own cumulative spend (P10.3),
	// populated only by the subprocess backend: a subprocess worker runs in a
	// separate process and can't share the parent's *cost.Tracker directly
	// via ctx the way an in-process sub-agent does, so it self-reports its
	// final totals here and SubprocessBackend folds them back into the
	// parent's shared ledger once the worker exits. Always zero for the
	// in-process backend, whose sub-agents already draw from the one shared
	// tracker with no reporting step needed.
	CostUSD float64
	Tokens  int
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

type costTrackerKey struct{}

// WithCostTracker returns a context carrying a shared spend ledger. tracker is
// typed any (rather than a concrete *cost.Tracker) so this package — which
// deliberately stays decoupled from the engine and its dependencies — need not
// import internal/cost; the caller that builds sub-agent engines type-asserts
// it back. Every sub-agent spawned from a context descending from this one
// (any depth, any workflow mode) shares the same ledger, so a session's
// BudgetUSD ceiling is checked against the fan-out tree's cumulative spend
// rather than resetting to a fresh allowance per spawned agent.
func WithCostTracker(ctx context.Context, tracker any) context.Context {
	return context.WithValue(ctx, costTrackerKey{}, tracker)
}

// CostTrackerFromContext returns the shared spend ledger carried by ctx, or
// nil if none was attached.
func CostTrackerFromContext(ctx context.Context) any {
	return ctx.Value(costTrackerKey{})
}
