package cli

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

func chatTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// P66.13/QUAL-01. `aegis chat` built a bare permission.New(mode, approver), so a
// `permission.rules` deny rule an operator wrote in config was silently inert on
// the scripted path while binding everywhere else — a security control that
// applies on four paths out of five is a control nobody can rely on.
//
// The assertion is deliberately on the deny rule rather than on the gate's type:
// what broke was not that a layer was missing but that a *configured decision*
// stopped being made.
func TestChatGateHonorsConfigDenyRules(t *testing.T) {
	cfg := &config.Config{}
	cfg.Permission.Mode = "build"
	cfg.Permission.Rules = []string{"deny shell(*)"}

	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: t.TempDir(), DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	gate, _ := buildChatGate(cfg, persona.Persona{}, reg, "build", true /* --yes */, t.TempDir(), chatTestLogger())

	sh, ok := reg.Get("shell")
	if !ok {
		t.Fatal("shell tool not registered")
	}
	allowed, reason := gate.Check(context.Background(), sh, json.RawMessage(`{"command":"echo hi"}`))
	if allowed {
		t.Fatal("shell was allowed under a `deny shell(*)` rule; the CLI gate is not stacking the rule layer")
	}
	if reason == "" {
		t.Error("a denial with no reason tells the model nothing about what to do next")
	}

	// The control: with no rules configured, --yes still approves, so the test
	// above is measuring the rule and not the approver.
	cfg.Permission.Rules = nil
	gate, _ = buildChatGate(cfg, persona.Persona{}, reg, "build", true, t.TempDir(), chatTestLogger())
	if allowed, _ := gate.Check(context.Background(), sh, json.RawMessage(`{"command":"echo hi"}`)); !allowed {
		t.Error("shell was refused with no deny rule configured; the control arm does not isolate the rule layer")
	}
}

// A persona's advisory deny rules must reach the CLI gate too. `--persona` used
// to reach only the system prompt: its rules and tool list were read for the
// prompt and dropped before the gate was built.
func TestChatGateAppliesPersonaDenyRules(t *testing.T) {
	cfg := &config.Config{}
	cfg.Permission.Mode = "build"

	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: t.TempDir(), DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	sh, ok := reg.Get("shell")
	if !ok {
		t.Fatal("shell tool not registered")
	}

	p := persona.Persona{Name: "locked-down", Rules: []string{"deny shell(*)"}}
	gate, _ := buildChatGate(cfg, p, reg, "build", true, t.TempDir(), chatTestLogger())
	if allowed, _ := gate.Check(context.Background(), sh, json.RawMessage(`{"command":"echo hi"}`)); allowed {
		t.Error("a persona's deny rule did not reach the CLI permission gate")
	}
}

// P66.13/QUAL-02. buildChatSystem omitted <deferred_tools> entirely, so the
// deferred tools the whole P62.6 line is about were undiscoverable via
// tool_search on this path — a pure capability loss, with the token saving of
// deferring them already banked.
func TestChatSystemAdvertisesDeferredTools(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	// The local profile is what defers tools in the first place.
	cfg.Provider.Default = "ollama"

	root := t.TempDir()
	reg := tool.NewRegistry()
	regOpts := builtin.Options{Root: root, DataDir: cfg.DataDir, LocalProfile: true}
	if err := builtin.Register(reg, regOpts); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(reg.Deferred()) == 0 {
		t.Fatal("no deferred tools under the local profile; this test can no longer detect the omission")
	}

	sys := buildChatSystem(cfg, root, nil, "you are a bot", persona.Persona{}, reg)
	if !strings.Contains(sys, "<deferred_tools>") {
		t.Fatalf("CLI system prompt has no <deferred_tools> block, so tool_search has nothing to find:\n%s", sys)
	}
	if !strings.Contains(sys, "tool_search") {
		t.Error("the deferred-tools block must name the tool that loads them, or the list is inert")
	}

	// A nil registry must not produce an empty block — an advertisement of
	// nothing is worse than no advertisement.
	if strings.Contains(buildChatSystem(cfg, root, nil, "you are a bot", persona.Persona{}, nil), "<deferred_tools>") {
		t.Error("a nil registry produced a <deferred_tools> block")
	}
}
