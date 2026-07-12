package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
)

// roleScriptedBackend is a swarm.Backend that returns a scripted response
// keyed by the spawned sub-agent's SystemPrompt, so a test can simulate
// distinct critic/proposer/arbiter turns through the real spawn path (unlike
// fakeBackend, which returns one fixed output for every call).
type roleScriptedBackend struct {
	root      string
	responses map[string]string // systemPrompt -> response
	spawns    int
	lastCfgs  []swarm.SpawnConfig
}

func (b *roleScriptedBackend) Spawn(ctx context.Context, cfg swarm.SpawnConfig) (*swarm.Handle, error) {
	b.spawns++
	b.lastCfgs = append(b.lastCfgs, cfg)
	backend := swarm.NewInProcessBackend(func(context.Context, swarm.SpawnConfig) (string, error) {
		resp, ok := b.responses[cfg.SystemPrompt]
		if !ok {
			return "(no scripted response for this role)", nil
		}
		return resp, nil
	}, swarm.NewRegistry(), swarm.MailboxRoot(b.root))
	return backend.Spawn(ctx, cfg)
}
func (b *roleScriptedBackend) Shutdown(context.Context)                  {}
func (b *roleScriptedBackend) OnStop(func(swarm.Identity, swarm.Result)) {}

func debateRoleBackend(t *testing.T, critique, rebuttal, verdict string) *roleScriptedBackend {
	t.Helper()
	return &roleScriptedBackend{
		root: t.TempDir(),
		responses: map[string]string{
			debate.PersonaSystem(debate.DefaultCriticPersona):   critique,
			debate.PersonaSystem(debate.DefaultProposerPersona): rebuttal,
			debate.PersonaSystem(debate.DefaultArbiterPersona):  verdict,
		},
	}
}

// TestAgentToolDebateBasic exercises debate mode end to end through the real
// `agent` tool Execute path (schema parsing, depth guard, swarm spawn) rather
// than calling internal/debate directly, proving the tool wiring itself works.
func TestAgentToolDebateBasic(t *testing.T) {
	b := debateRoleBackend(t,
		"Evidence: internal/auth/token.go:42 shows the token is read-only, contradicting the claimed impact.",
		"Agreed, revising the impact statement.",
		"VERDICT: REVISE\nCONFIDENCE: high\nREASON: round 1's cited evidence was accepted.",
	)
	at := NewAgentTool(b, nil)

	input, err := json.Marshal(map[string]any{
		"mode":       "debate",
		"claim":      "Token X allows full account takeover.",
		"max_rounds": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, execErr := at.Execute(context.Background(), input)
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	if !strings.Contains(res.Content, "=== Verdict ===") || !strings.Contains(res.Content, "REVISE") {
		t.Errorf("expected a formatted verdict in output, got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "[evidence cited]") {
		t.Errorf("expected the cited critique to be tagged, got:\n%s", res.Content)
	}
	if b.spawns != 3 {
		t.Errorf("spawns = %d, want 3 (critic, proposer, arbiter)", b.spawns)
	}
}

// TestAgentToolDebateCapturesSpawningWorkdir is the P25.8 regression for the
// agent tool's debate mode: every spawned role must carry the calling turn's
// workdir on its SpawnConfig, the same as the single-agent and workflow spawn
// sites.
func TestAgentToolDebateCapturesSpawningWorkdir(t *testing.T) {
	b := debateRoleBackend(t,
		"CONCEDE — no defensible flaw found.",
		"(should not be spawned)",
		"VERDICT: UPHOLD\nCONFIDENCE: high\nREASON: critic conceded.",
	)
	at := NewAgentTool(b, nil)

	input, err := json.Marshal(map[string]any{
		"mode":       "debate",
		"claim":      "Token X allows full account takeover.",
		"max_rounds": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithWorkdir(context.Background(), "/session/root")
	if _, execErr := at.Execute(ctx, input); execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if len(b.lastCfgs) == 0 {
		t.Fatal("expected at least one spawned role")
	}
	for _, cfg := range b.lastCfgs {
		if cfg.Workdir != "/session/root" {
			t.Errorf("role %q SpawnConfig.Workdir = %q, want /session/root", cfg.Name, cfg.Workdir)
		}
	}
}

// TestAgentToolDebateConcessionSkipsRebuttalSpawn proves an explicit CONCEDE
// avoids spawning a pointless rebuttal sub-agent.
func TestAgentToolDebateConcessionSkipsRebuttalSpawn(t *testing.T) {
	b := debateRoleBackend(t,
		"CONCEDE — no defensible flaw found.",
		"(should not be spawned)",
		"VERDICT: UPHOLD\nCONFIDENCE: high\nREASON: critic conceded.",
	)
	at := NewAgentTool(b, nil)

	input, _ := json.Marshal(map[string]any{
		"mode":       "debate",
		"claim":      "Claim under test.",
		"max_rounds": 2,
	})
	res, execErr := at.Execute(context.Background(), input)
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if b.spawns != 2 {
		t.Errorf("spawns = %d, want 2 (critic + arbiter only)", b.spawns)
	}
	if !strings.Contains(res.Content, "UPHOLD") {
		t.Errorf("expected UPHOLD verdict, got:\n%s", res.Content)
	}
}

// TestAgentToolDebateRequiresClaim mirrors TestAgentToolRequiresPrompt for the
// debate mode's required field.
func TestAgentToolDebateRequiresClaim(t *testing.T) {
	b := &fakeBackend{root: t.TempDir()}
	at := NewAgentTool(b, nil)
	input, _ := json.Marshal(map[string]any{"mode": "debate"})
	res, err := at.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "claim") {
		t.Errorf("expected a 'claim is required' error, got %+v", res)
	}
}

// TestAgentToolDebateDepthGuard mirrors TestAgentToolDepthGuard for debate mode.
func TestAgentToolDebateDepthGuard(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	at := NewAgentTool(b, nil)
	ctx := swarm.WithDepth(context.Background(), maxSpawnDepth)
	input, _ := json.Marshal(map[string]any{"mode": "debate", "claim": "x"})
	res, err := at.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "depth") {
		t.Errorf("expected a depth-guard error, got %+v", res)
	}
	if b.spawns != 0 {
		t.Errorf("must not spawn when over the depth guard, spawns=%d", b.spawns)
	}
}

// TestAgentToolDebateRespectsCostCaps proves P12.6's wiring: when the shared
// cost tracker (carried on ctx the same way D1's fan-out fix reads it) is
// already within the configured cap's headroom, the debate skips the critic
// round entirely and goes straight to arbitration, instead of spawning a
// round the engine's own budget gate would otherwise abort mid-critique.
func TestAgentToolDebateRespectsCostCaps(t *testing.T) {
	b := debateRoleBackend(t,
		"(should not be spawned)",
		"(should not be spawned)",
		"VERDICT: UPHOLD\nCONFIDENCE: low\nREASON: no budget for any round.",
	)
	at := NewAgentTool(b, nil, WithCostCaps(0, 1000))

	tracker := cost.NewTracker()
	tracker.AddTokens(provider.Usage{InputTokens: 950})
	ctx := swarm.WithCostTracker(context.Background(), tracker)

	input, _ := json.Marshal(map[string]any{
		"mode":       "debate",
		"claim":      "Claim under test.",
		"max_rounds": 2,
	})
	res, execErr := at.Execute(ctx, input)
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if b.spawns != 1 {
		t.Errorf("spawns = %d, want 1 (arbiter only, no critic round)", b.spawns)
	}
	if !strings.Contains(res.Content, "UPHOLD") {
		t.Errorf("expected the arbiter's verdict to still be produced, got:\n%s", res.Content)
	}
}
