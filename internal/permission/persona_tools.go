package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/fiddler110/aegis/internal/tool"
)

// PersonaToolGate wraps a base gate with an advisory check against a
// persona's declared Tools list. This is deliberately NOT a security
// boundary — persona.Tools comes from the same file as a persona's Mode, and
// per the P7.5 trust model a persona file must never gain enforcement teeth
// beyond a confirmation prompt. A tool outside the list is logged and routed
// through the same Approver used for capability Ask decisions: a
// non-interactive approver (AutoApprove, e.g. auto mode) transparently
// "warns and allows", while an interactive approver (the TUI's sseApprover)
// prompts the user to confirm — and reuses that approver's session-scoped
// allow-always cache, so confirming once doesn't re-prompt every call.
// Declining the prompt blocks that specific call; approving (or the absence
// of any restriction) always still falls through to the base gate, which
// remains the actual security decision.
type PersonaToolGate struct {
	base        checker
	allowed     map[string]struct{} // lowercased tool names; empty = no restriction
	personaName string
	approver    Approver
	logger      *slog.Logger
	onDecision  func(ContextualDecision)
}

// NewPersonaToolGate wraps base with an advisory tool-list check for
// persona. A nil/empty tools list makes Check a pure passthrough. A nil
// approver defaults to AutoApprove — absent an interactive approver, an
// out-of-list tool call is warned about (via logger) and allowed, never
// hard-blocked.
func NewPersonaToolGate(base checker, personaName string, tools []string, approver Approver, logger *slog.Logger, onDecision func(ContextualDecision)) *PersonaToolGate {
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
			g.logger.Warn("tool call outside persona's declared tool list",
				"persona", g.personaName, "tool", t.Name())
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
