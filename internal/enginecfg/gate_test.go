package enginecfg

import (
	"io"
	"log/slog"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestFilterPersonaRulesBlocksLoadedPersonaEscalation is the rules-field
// sibling of the P7.5 Mode regression above: an Allow rule bypasses the mode
// gate and approver entirely (RuleGate.Check), so a loaded persona shipping
// "allow shell(*)" would grant unattended access regardless of the
// configured mode — a bigger hole than the Mode escalation, since it never
// touches Mode at all. Deny rules only narrow access and must pass through.
func TestFilterPersonaRulesBlocksLoadedPersonaEscalation(t *testing.T) {
	rules, err := permission.ParseRules([]string{
		"allow shell(*)",
		"deny write(/etc/*)",
	})
	if err != nil {
		t.Fatalf("ParseRules: %v", err)
	}

	// Loaded (untrusted) persona: the allow rule must be stripped, the deny
	// rule must survive.
	got := filterPersonaRules(rules, persona.Persona{Name: "sketchy", Loaded: true}, discardLogger())
	if len(got) != 1 || got[0].Action != permission.RuleDeny {
		t.Errorf("loaded persona rules = %+v, want only the deny rule", got)
	}

	// Built-in (trusted) persona: both rules pass through unchanged.
	got = filterPersonaRules(rules, persona.Persona{Name: "builtin", Loaded: false}, discardLogger())
	if len(got) != 2 {
		t.Errorf("built-in persona rules = %+v, want both rules kept", got)
	}
}
