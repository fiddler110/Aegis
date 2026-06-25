// Package permission gates tool execution by mode and capability.
//
// It implements two postures borrowed from opencode/Claude Code:
//   - Plan mode: read-only. The agent may inspect files and the network but
//     may not mutate the workspace or run commands.
//   - Build mode: the agent may mutate the workspace; running commands still
//     requires approval.
package permission

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/scottymacleod/aegis/internal/tool"
)

// Mode is the agent's permission posture.
type Mode string

const (
	ModePlan  Mode = "plan"
	ModeBuild Mode = "build"
)

// ParseMode normalizes a mode string, defaulting to plan.
func ParseMode(s string) Mode {
	if Mode(s) == ModeBuild {
		return ModeBuild
	}
	return ModePlan
}

// Decision is the outcome of a policy evaluation.
type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
	Ask   Decision = "ask"
)

// Policy maps (mode, capability) to a decision.
type Policy struct {
	Mode Mode
}

// Decide returns the policy decision for a capability under the current mode.
func (p Policy) Decide(cap tool.Capability) Decision {
	switch p.Mode {
	case ModeBuild:
		switch cap {
		case tool.CapExecute:
			return Ask
		default: // read, write, network, spawn
			return Allow
		}
	default: // plan
		switch cap {
		// Spawning is allowed in plan mode: a child inherits the parent's
		// (read-only) posture via permission sync, so it cannot mutate.
		case tool.CapRead, tool.CapNetwork, tool.CapSpawn:
			return Allow
		default: // write, execute
			return Deny
		}
	}
}

// Approver resolves Ask decisions, e.g. via an interactive TUI prompt.
type Approver interface {
	Approve(ctx context.Context, toolName, reason string) bool
}

// AutoDeny denies every Ask decision (safe default for non-interactive use).
type AutoDeny struct{}

func (AutoDeny) Approve(context.Context, string, string) bool { return false }

// AutoApprove allows every Ask decision (use only in trusted contexts).
type AutoApprove struct{}

func (AutoApprove) Approve(context.Context, string, string) bool { return true }

// Gate combines a policy with an approver to decide individual tool calls.
type Gate struct {
	Policy   Policy
	Approver Approver
}

// New builds a Gate for the given mode and approver. A nil approver defaults
// to AutoDeny.
func New(mode Mode, approver Approver) Gate {
	if approver == nil {
		approver = AutoDeny{}
	}
	return Gate{Policy: Policy{Mode: mode}, Approver: approver}
}

// Check decides whether a tool call may proceed, returning a human-readable
// reason when denied. It satisfies the engine's gate interface.
func (g Gate) Check(ctx context.Context, t tool.Tool, _ json.RawMessage) (bool, string) {
	cap := t.Capability()
	switch g.Policy.Decide(cap) {
	case Allow:
		return true, ""
	case Ask:
		reason := fmt.Sprintf("%s requires %s access", t.Name(), cap)
		if g.Approver.Approve(ctx, t.Name(), reason) {
			return true, ""
		}
		return false, fmt.Sprintf("%s denied: %s not approved", t.Name(), cap)
	default: // Deny
		return false, fmt.Sprintf("%s blocked: %s access not allowed in %s mode", t.Name(), cap, g.Policy.Mode)
	}
}
