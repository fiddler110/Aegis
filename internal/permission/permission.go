// Package permission gates tool execution by mode and capability.
//
// Three permission postures are supported:
//   - Plan mode: read-only. The agent may inspect files without prompting;
//     network access requires approval (defaults to deny in non-interactive
//     contexts) and the workspace may not be mutated or commands run at all.
//   - Build mode: the agent may mutate the workspace; shell execution still
//     requires an approver (defaults to deny in non-interactive contexts).
//   - Auto mode: all capabilities allowed without approval, including shell
//     execution. Use only in trusted, sandboxed environments.
package permission

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fiddler110/aegis/internal/tool"
)

// Mode is the agent's permission posture.
type Mode string

const (
	ModePlan  Mode = "plan"
	ModeBuild Mode = "build"
	ModeAuto  Mode = "auto" // build + execute auto-approved
)

// ParseMode normalizes a mode string, defaulting to plan for unknown values.
func ParseMode(s string) Mode {
	switch Mode(s) {
	case ModeBuild:
		return ModeBuild
	case ModeAuto:
		return ModeAuto
	default:
		return ModePlan
	}
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
	// StrictPlanMode refuses per-call capability *downgrades* in plan mode
	// (DR-2): a tool is then judged on the capability it declares, whatever
	// CapabilityFor says about the specific call.
	//
	// Plan mode's documented guarantee is that "the workspace may not be
	// mutated or commands run at all", and Decide does deny CapExecute there —
	// but EffectiveCapability is consulted first, so the shell tool runs
	// commands in plan mode whenever classifyShellCommand answers CapRead. That
	// is the intended P25.4c design and it is right on its own terms: before
	// it, a `git log` in plan mode was silently denied, which is worse. It does
	// mean, though, that every defect in ~1,080 lines of hand-written argument
	// parsing is a *plan-mode* defect — and plan mode is the posture an
	// operator chooses precisely when they want a hard boundary rather than a
	// convenient one.
	//
	// Off by default, so the shipped behavior is unchanged. Build and auto mode
	// ignore it entirely: the ergonomics the downgrade buys are kept where they
	// are wanted, and the guarantee is available where it is claimed.
	StrictPlanMode bool
}

// strictness orders decisions from most to least permissive, so two candidate
// capabilities can be compared by what they would actually allow.
func strictness(d Decision) int {
	switch d {
	case Allow:
		return 0
	case Ask:
		return 1
	default: // Deny
		return 2
	}
}

// resolveCapability picks the capability a call is judged on: the per-call
// effective one, except under StrictPlanMode in plan mode, where the *stricter*
// of the effective and declared capabilities wins. Comparing by decision rather
// than by capability name keeps a tool that reclassifies *upward* — a narrower
// call declaring itself more dangerous — honored in both settings.
func (p Policy) resolveCapability(effective, static tool.Capability) tool.Capability {
	if !p.StrictPlanMode || p.Mode != ModePlan || effective == static {
		return effective
	}
	if strictness(p.Decide(static)) > strictness(p.Decide(effective)) {
		return static
	}
	return effective
}

// Decide returns the policy decision for a capability under the current mode.
func (p Policy) Decide(cap tool.Capability) Decision {
	switch p.Mode {
	case ModeAuto:
		return Allow // all capabilities permitted without prompting
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
		case tool.CapRead, tool.CapSpawn:
			return Allow
		// Plan mode used to allow silent network egress here: a "read-only"
		// session could still read a sensitive file (CapRead, silently
		// allowed) and immediately exfiltrate it over CapNetwork (also
		// silently allowed), with no approval anywhere in the path — the
		// ContextualGate's egress-then-write rule doesn't help since it only
		// gates a *write* after egress, and it's opt-in besides. Requiring
		// approval for network in plan mode closes that silent side channel
		// while leaving local reads frictionless.
		case tool.CapNetwork:
			return Ask
		default: // write, execute
			return Deny
		}
	}
}

// Approver resolves Ask decisions, e.g. via an interactive TUI prompt.
// input is the raw tool arguments; approvers may inspect it for context-aware
// decisions (e.g. showing the path being written, the command being run).
type Approver interface {
	Approve(ctx context.Context, toolName, reason string, input json.RawMessage) bool
}

// AutoDeny denies every Ask decision (safe default for non-interactive use).
type AutoDeny struct{}

func (AutoDeny) Approve(context.Context, string, string, json.RawMessage) bool { return false }

// AutoApprove allows every Ask decision (use only in trusted contexts).
type AutoApprove struct{}

func (AutoApprove) Approve(context.Context, string, string, json.RawMessage) bool { return true }

// Gate combines a policy with an approver to decide individual tool calls.
type Gate struct {
	Policy   Policy
	Approver Approver
	// OnDecision, when set, receives a record for every call whose *effective*
	// capability differs from the tool's static one — the silent downgrade
	// described on CapabilityDowngradeRule. It is the same sink the contextual
	// gate's rules report to; enginecfg.BuildGate wires both from one option so
	// an operator reads one stream. A Gate built without it decides
	// identically and reports nothing, which is what a bare permission.New
	// gets.
	OnDecision func(ContextualDecision)
	// SandboxBackendLabel, when set, is appended to the Ask reason for an
	// execute-capability call (P81.22/FIND-22): "this command will run
	// unconfined" needs to be visible at the point of the approval decision,
	// not only in a startup log line the operator may never see. Empty skips
	// the annotation, which is what every Gate built before this field
	// existed still gets.
	SandboxBackendLabel string
}

// CapabilityDowngradeRule is the Rule name on the record Gate.Check emits when
// a tool reclassifies a call below its static capability (M7).
//
// Downgrades were entirely unobservable: shell is statically CapExecute, and a
// call classified CapRead is *allowed silently in every mode*, so an operator
// reviewing an audit trail saw an execute-capable tool run with no approval and
// no record of why. That silence is the mechanism CRIT-1, CRIT-2 and CRIT-3 all
// ride on — each is a way to make the classifier answer CapRead for a call that
// is not a read — and a downgrade record is what would have made any of them
// visible in a log rather than only in a code reading.
//
// It is a record, not a decision: the call was allowed on the narrower basis,
// and the Decision field says which tier that basis landed in.
const CapabilityDowngradeRule = "capability_override"

// DestructiveEscalationRule is the Rule name on the record Gate.Check emits
// when a call that would otherwise be silently allowed is escalated to Ask
// because the tool declares it destructive (P67.10) — an overwrite, a
// delete, or an irreversible send. Auto mode is exempt: its whole contract
// is "no approval, trusted context," and this stays consistent with that.
const DestructiveEscalationRule = "destructive_escalation"

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
func (g Gate) Check(ctx context.Context, t tool.Tool, input json.RawMessage) (bool, string) {
	// EffectiveCapability, not the tool's static Capability() (P25.4c): a
	// call a tool itself classifies as narrower than its usual capability —
	// e.g. shell's read-only allowlist — should be gated (and, in plan
	// mode, allowed) on that narrower basis instead of always paying the
	// tool's worst-case capability.
	static := t.Capability()
	cap := g.Policy.resolveCapability(tool.EffectiveCapability(ctx, t, input), static)
	decision := g.Policy.Decide(cap)
	if cap != static && g.OnDecision != nil {
		g.OnDecision(ContextualDecision{
			Tool: t.Name(), Cap: string(cap), Rule: CapabilityDowngradeRule,
			Decision: decision,
			Reason: fmt.Sprintf("gated as %s rather than the tool's declared %s",
				cap, static),
		})
	}
	// P67.10: a call the policy would otherwise allow silently still gets
	// asked about when the tool declares it destructive — an overwrite, a
	// delete, or an irreversible send. Auto mode is exempt: Decide already
	// short-circuits every capability to Allow there, and this stays
	// consistent with auto mode's "no approval, trusted context" contract.
	destructive := decision == Allow && g.Policy.Mode != ModeAuto && tool.EffectiveDestructive(ctx, t, input)
	if destructive {
		if g.OnDecision != nil {
			g.OnDecision(ContextualDecision{
				Tool: t.Name(), Cap: string(cap), Rule: DestructiveEscalationRule,
				Decision: Ask,
				Reason:   fmt.Sprintf("%s is irreversible, asking despite %s normally being allowed", t.Name(), cap),
			})
		}
		decision = Ask
	}
	switch decision {
	case Allow:
		return true, ""
	case Ask:
		reason := fmt.Sprintf("%s requires %s access", t.Name(), cap)
		if destructive {
			reason += " (irreversible)"
		}
		// P81.22/FIND-22: an execute-capability approval is the moment an
		// unconfined command is about to run — say so here, not only in a
		// startup log the operator has likely scrolled past.
		if cap == tool.CapExecute && g.SandboxBackendLabel != "" {
			reason = fmt.Sprintf("%s (sandbox: %s)", reason, g.SandboxBackendLabel)
		}
		if g.Approver.Approve(ctx, t.Name(), reason, input) {
			return true, ""
		}
		return false, fmt.Sprintf("%s denied: %s access was not approved — ask the user to approve or switch to auto mode", t.Name(), cap)
	default: // Deny
		hint := "switch to build mode to allow write/execute access, or use a read-only tool instead"
		if cap == tool.CapExecute {
			hint = "switch to auto mode or enable auto_approve_exec to allow shell execution"
		}
		return false, fmt.Sprintf("%s blocked: %s access not allowed in %s mode (%s)", t.Name(), cap, g.Policy.Mode, hint)
	}
}
