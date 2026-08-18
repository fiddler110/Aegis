package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func debateInput(t *testing.T) []byte {
	t.Helper()
	in, err := json.Marshal(map[string]any{
		"mode":       "debate",
		"claim":      "Token X allows full account takeover.",
		"max_rounds": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// A resident set that cannot fit is a reason not to start, not a reason to start
// and let Ollama discover the overcommit by spilling the arbiter to system RAM.
// The refusal must arrive before any role is spawned.
func TestDebateRefusesWhenTheResidentSetDoesNotFit(t *testing.T) {
	b := debateRoleBackend(t, "critique", "rebuttal", "VERDICT: UPHOLD\nCONFIDENCE: high")
	at := NewAgentTool(b, nil,
		WithResidentSetClaim(func(context.Context, []string) (func(), error) {
			return nil, errors.New("cannot fit 2 models in the 8.00 GiB memory budget")
		}),
	)

	res, err := at.Execute(context.Background(), debateInput(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError {
		t.Fatal("a debate whose models do not fit reported success")
	}
	if !strings.Contains(res.Content, "cannot fit") {
		t.Errorf("result %q does not carry the planner's reason", res.Content)
	}
	if b.spawns != 0 {
		t.Errorf("spawned %d roles despite the refusal; the refusal must precede any model turn", b.spawns)
	}
}

// The claim is held for the debate and released after it, whatever the outcome —
// a claim that leaked would keep every later debate queued behind a debate that
// already finished.
func TestDebateReleasesTheResidentSetClaim(t *testing.T) {
	b := debateRoleBackend(t,
		"Evidence: internal/auth/token.go:42 shows the token is read-only.",
		"Agreed, revising.",
		"VERDICT: REVISE\nCONFIDENCE: high\nREASON: evidence accepted.",
	)
	var claimed, released atomic.Int32
	var sawModels []string
	at := NewAgentTool(b, nil,
		WithDebateSeatModel(func(persona string) string {
			if strings.Contains(persona, "arbiter") {
				return "arbiter-model"
			}
			return "debater-model"
		}),
		WithResidentSetClaim(func(_ context.Context, models []string) (func(), error) {
			claimed.Add(1)
			sawModels = append([]string(nil), models...)
			return func() { released.Add(1) }, nil
		}),
	)

	res, err := at.Execute(context.Background(), debateInput(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("debate failed: %s", res.Content)
	}
	if claimed.Load() != 1 || released.Load() != 1 {
		t.Errorf("claimed %d / released %d, want 1 / 1", claimed.Load(), released.Load())
	}
	// All three seats are offered, duplicates included: two of them share a
	// model, and collapsing that is the planner's job, not the caller's — a
	// caller that deduplicates would hide the shared-runner fact the planner
	// needs to not double-count the weights.
	if len(sawModels) != 3 {
		t.Fatalf("claimed %v, want all three seats", sawModels)
	}
	if sawModels[0] != "debater-model" || sawModels[1] != "debater-model" || sawModels[2] != "arbiter-model" {
		t.Errorf("claimed %v, want proposer/critic on the debater model and the arbiter on its own", sawModels)
	}
}

// Every embedder that does not wire a claim — tests, non-daemon hosts — must be
// unaffected, which is the same posture an unset provider.vram_budget_gb has.
func TestDebateWithoutAResidentSetClaimIsUnaffected(t *testing.T) {
	b := debateRoleBackend(t, "critique", "rebuttal", "VERDICT: UPHOLD\nCONFIDENCE: high")
	at := NewAgentTool(b, nil)
	res, err := at.Execute(context.Background(), debateInput(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("debate failed with no claim wired: %s", res.Content)
	}
	var _ tool.Result = res
}
