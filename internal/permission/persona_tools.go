package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/fiddler110/aegis/internal/tool"
)

// PersonaToolGate wraps a base gate with a check against a persona's declared
// Tools list. By default (enforcing=false) this is NOT a security boundary —
// persona.Tools comes from the same file as a persona's Mode, and per the
// P7.5 trust model a persona file must never gain enforcement teeth beyond a
// confirmation prompt. A tool outside the list is logged and routed through
// the same Approver used for capability Ask decisions: a non-interactive
// approver (AutoApprove, e.g. auto mode) transparently "warns and allows",
// while an interactive approver (the TUI's sseApprover) prompts the user to
// confirm — and reuses that approver's session-scoped allow-always cache, so
// confirming once doesn't re-prompt every call. Declining the prompt blocks
// that specific call; approving (or the absence of any restriction) always
// still falls through to the base gate, which remains the actual security
// decision.
//
// enforcing=true (P81.20/FIND-20 item 4, persona.Persona.ToolsEnforced) opts
// a persona into a hard boundary instead: a call to a tool outside the list
// is refused outright, with no approver consulted at all. This does not
// reopen the P7.5 hole an honored Mode or allow-Rule would: enforcing mode
// only ever *narrows* what a session may call, so honoring it even from a
// loaded (untrusted) persona file cannot escalate anything — the same
// reasoning that lets filterPersonaRules keep a loaded persona's deny rules
// while dropping its allow rules. Advisory stays the default; a persona must
// opt in.
type PersonaToolGate struct {
	base        checker
	allowed     map[string]struct{} // lowercased tool names; empty = no restriction
	personaName string
	enforcing   bool
	approver    Approver
	logger      *slog.Logger
	onDecision  func(ContextualDecision)
}

// NewPersonaToolGate wraps base with a tool-list check for persona. A
// nil/empty tools list makes Check a pure passthrough. A nil approver
// defaults to AutoApprove — under the advisory (default) posture, an
// out-of-list tool call is warned about (via logger) and allowed, never
// hard-blocked; under enforcing it is always hard-blocked and the approver is
// not consulted.
func NewPersonaToolGate(base checker, personaName string, tools []string, enforcing bool, approver Approver, logger *slog.Logger, onDecision func(ContextualDecision)) *PersonaToolGate {
	allowed := make(map[string]struct{}, len(tools))
	for _, name := range tools {
		allowed[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	if approver == nil {
		approver = AutoApprove{}
	}
	return &PersonaToolGate{
		base:        base,
		allowed:     allowed,
		personaName: personaName,
		enforcing:   enforcing,
		approver:    approver,
		logger:      logger,
		onDecision:  onDecision,
	}
}

// Check implements engine.Gate.
func (g *PersonaToolGate) Check(ctx context.Context, t tool.Tool, input json.RawMessage) (bool, string) {
	if len(g.allowed) == 0 {
		return g.base.Check(ctx, t, input)
	}
	name := strings.ToLower(t.Name())
	if _, ok := g.allowed[name]; !ok {
		reason := fmt.Sprintf("persona %q does not list %q among its declared tools", g.personaName, t.Name())
		if g.logger != nil {
			verb := "outside persona's declared tool list"
			if g.enforcing {
				verb = "outside persona's enforced tool list, refused"
			}
			g.logger.Warn("tool call "+verb, "persona", g.personaName, "tool", t.Name())
		}
		if g.enforcing {
			if g.onDecision != nil {
				g.onDecision(ContextualDecision{Tool: t.Name(), Cap: string(t.Capability()), Rule: "persona_tools_enforced", Reason: reason, Decision: Deny})
			}
			return false, fmt.Sprintf("%s refused: %s (persona %q enforces its tool list)", t.Name(), reason, g.personaName)
		}
		approved := g.approver.Approve(ctx, t.Name(), reason, input)
		if g.onDecision != nil {
			d := ContextualDecision{Tool: t.Name(), Cap: string(t.Capability()), Rule: "persona_tools", Reason: reason}
			if approved {
				d.Decision = Allow
			} else {
				d.Decision = Deny
			}
			g.onDecision(d)
		}
		if !approved {
			return false, fmt.Sprintf("%s declined: %s (not in persona %q's tool list)", t.Name(), reason, g.personaName)
		}
	}
	return g.base.Check(ctx, t, input)
}
