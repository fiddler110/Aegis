package debate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/provider"
)

// scriptedRun returns a RunFunc that returns the next response in order,
// keyed by which persona system prompt is being invoked (so a test can script
// critic/proposer/arbiter turns independently without caring about exact call
// count).
func scriptedRun(t *testing.T, byRole map[string][]string) RunFunc {
	t.Helper()
	idx := map[string]int{}
	return func(_ context.Context, systemPrompt, _ string) (string, error) {
		role := roleFor(t, systemPrompt)
		responses := byRole[role]
		i := idx[role]
		if i >= len(responses) {
			t.Fatalf("scriptedRun: no more scripted responses for role %q (call %d)", role, i+1)
		}
		idx[role] = i + 1
		return responses[i], nil
	}
}

func roleFor(t *testing.T, systemPrompt string) string {
	t.Helper()
	switch systemPrompt {
	case PersonaSystem(DefaultCriticPersona):
		return "critic"
	case PersonaSystem(DefaultProposerPersona):
		return "proposer"
	case PersonaSystem(DefaultArbiterPersona):
		return "arbiter"
	default:
		t.Fatalf("roleFor: unrecognized system prompt")
		return ""
	}
}

// TestRunValidRebuttalChangesVerdict proves the mechanism is not a no-op: a
// critic that raises a substantiated challenge, met with a rebuttal the
// arbiter is scripted to find convincing, still produces the arbiter's actual
// verdict (not a hardcoded uphold) — the debate loop is really passing the
// transcript through to arbitration, not discarding rounds.
func TestRunValidRebuttalChangesVerdict(t *testing.T) {
	run := scriptedRun(t, map[string][]string{
		"critic": {
			"The claim overstates severity. Evidence: internal/auth/token.go:42 shows the token is scoped to read-only, contradicting the claimed full-account-takeover impact.",
		},
		"proposer": {
			"Fair — internal/auth/token.go:42 does scope the token to read-only. Revising impact from full-account-takeover to read-only data exposure.",
		},
		"arbiter": {
			"VERDICT: REVISE\nCONFIDENCE: high\nREASON: Round 1's evidence-cited challenge was accepted by the proposer's own rebuttal.",
		},
	})

	tr, err := Run(context.Background(), "Auth token X allows full account takeover.", Config{MaxRounds: 1}, run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if tr.Verdict.Outcome != "REVISE" {
		t.Fatalf("Verdict.Outcome = %q, want REVISE", tr.Verdict.Outcome)
	}
	if tr.Verdict.Confidence != "high" {
		t.Fatalf("Verdict.Confidence = %q, want high", tr.Verdict.Confidence)
	}
	if len(tr.Rounds) != 1 {
		t.Fatalf("len(Rounds) = %d, want 1", len(tr.Rounds))
	}
	if !tr.Rounds[0].Evidence {
		t.Fatalf("Rounds[0].Evidence = false, want true (challenge cited a file:line)")
	}
}

// TestRunUnsubstantiatedCritiqueIsTagged proves P12.3: a critique with no
// cited evidence is mechanically detected and tagged [unsubstantiated] in the
// transcript fed to the arbiter, rather than silently accepted as a real
// rebuttal on the same footing as a grounded one.
func TestRunUnsubstantiatedCritiqueIsTagged(t *testing.T) {
	run := scriptedRun(t, map[string][]string{
		"critic": {
			"This seems risky and probably has other issues too.",
		},
		"proposer": {
			"Can you be specific about what's wrong?",
		},
		"arbiter": {
			"VERDICT: UPHOLD\nCONFIDENCE: medium\nREASON: Round 1's critique cited no evidence and is discounted.",
		},
	})

	tr, err := Run(context.Background(), "Claim under test.", Config{MaxRounds: 1}, run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if tr.Rounds[0].Evidence {
		t.Fatalf("Rounds[0].Evidence = true, want false for an uncited critique")
	}
	formatted := tr.Format()
	if !strings.Contains(formatted, "[unsubstantiated]") {
		t.Fatalf("Format() output missing [unsubstantiated] tag:\n%s", formatted)
	}
	if tr.Verdict.Outcome != "UPHOLD" {
		t.Fatalf("Verdict.Outcome = %q, want UPHOLD", tr.Verdict.Outcome)
	}
}

// TestRunConcessionSkipsRebuttalAndFurtherRounds proves an explicit CONCEDE
// stops the round loop immediately rather than forcing a pointless rebuttal
// or additional rounds.
func TestRunConcessionSkipsRebuttalAndFurtherRounds(t *testing.T) {
	run := scriptedRun(t, map[string][]string{
		"critic": {
			"CONCEDE — I found no defensible flaw in this claim.",
		},
		"arbiter": {
			"VERDICT: UPHOLD\nCONFIDENCE: high\nREASON: The critic conceded in round 1.",
		},
	})

	tr, err := Run(context.Background(), "Claim under test.", Config{MaxRounds: 2}, run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tr.Rounds) != 1 {
		t.Fatalf("len(Rounds) = %d, want 1 (concession should stop the loop)", len(tr.Rounds))
	}
	if !tr.Rounds[0].Conceded {
		t.Fatalf("Rounds[0].Conceded = false, want true")
	}
	if tr.Rounds[0].Rebuttal != "" {
		t.Fatalf("Rounds[0].Rebuttal = %q, want empty after a concession", tr.Rounds[0].Rebuttal)
	}
}

// TestRunMaxRoundsDefault proves the hard default (P12.6) applies when the
// caller doesn't specify one.
func TestRunMaxRoundsDefault(t *testing.T) {
	// A critic script of exactly DefaultMaxRounds non-conceding entries: if the
	// default were anything other than DefaultMaxRounds, this either runs out
	// of scripted responses (t.Fatalf inside scriptedRun) or stops early.
	nonConceding := scriptedRun(t, map[string][]string{
		"critic": {
			"Evidence: file.go:1 shows a gap.",
			"Evidence: file.go:2 shows another gap.",
		},
		"proposer": {
			"Addressed in round 1.",
			"Addressed in round 2.",
		},
		"arbiter": {
			"VERDICT: UPHOLD\nCONFIDENCE: low\nREASON: exhausted rounds.",
		},
	})
	tr, err := Run(context.Background(), "Claim.", Config{}, nonConceding)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tr.Rounds) != DefaultMaxRounds {
		t.Fatalf("len(Rounds) = %d, want DefaultMaxRounds=%d", len(tr.Rounds), DefaultMaxRounds)
	}
}

// TestRunBudgetExhaustedSkipsToArbitration proves P12.6: once the shared
// tracker is within budgetHeadroomFraction of the cap, Run stops starting new
// rounds and goes straight to arbitration on whatever transcript exists so
// far, instead of letting the next role call abort mid-critique with no
// verdict produced.
func TestRunBudgetExhaustedSkipsToArbitration(t *testing.T) {
	tracker := cost.NewTracker()
	tracker.AddTokens(provider.Usage{InputTokens: 950})

	criticCalls := 0
	run := func(_ context.Context, systemPrompt, _ string) (string, error) {
		if systemPrompt == PersonaSystem(DefaultCriticPersona) {
			criticCalls++
			return "should not be reached", nil
		}
		// Arbiter call.
		return "VERDICT: UPHOLD\nCONFIDENCE: low\nREASON: budget exhausted before any round ran.", nil
	}

	tr, err := Run(context.Background(), "Claim.", Config{
		MaxRounds: 2,
		Tracker:   tracker,
		MaxTokens: 1000, // 90% headroom = 900; already at 950
	}, run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if criticCalls != 0 {
		t.Fatalf("criticCalls = %d, want 0 (budget should have been exhausted before round 1)", criticCalls)
	}
	if len(tr.Rounds) != 1 || !tr.Rounds[0].BudgetStop {
		t.Fatalf("Rounds = %+v, want a single BudgetStop round", tr.Rounds)
	}
	if tr.Verdict.Outcome != "UPHOLD" {
		t.Fatalf("Verdict.Outcome = %q, want UPHOLD (arbitration must still run)", tr.Verdict.Outcome)
	}
}

// TestRunPropagatesRoleError proves a role-call failure (e.g. the underlying
// spawn erroring) surfaces to the caller instead of silently producing an
// empty/misleading verdict.
func TestRunPropagatesRoleError(t *testing.T) {
	run := func(_ context.Context, systemPrompt, _ string) (string, error) {
		if systemPrompt == PersonaSystem(DefaultCriticPersona) {
			return "", errors.New("spawn failed")
		}
		t.Fatalf("unexpected call past the failing critic role")
		return "", nil
	}
	_, err := Run(context.Background(), "Claim.", Config{MaxRounds: 1}, run)
	if err == nil {
		t.Fatalf("Run: want error, got nil")
	}
	if !strings.Contains(err.Error(), "spawn failed") {
		t.Fatalf("Run error = %v, want it to wrap the underlying spawn error", err)
	}
}
