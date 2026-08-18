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
	return func(_ context.Context, seat Seat, systemPrompt, _ string) (string, error) {
		// Cross-check the seat against the system prompt it arrived with. The
		// seat is what a caller resolves a per-role *model* from (P69.1), so a
		// seat that disagrees with the prompt would route the arbiter's call to
		// the critic's model with nothing else failing.
		if got := roleFor(t, systemPrompt); got != string(seat.Role) {
			t.Fatalf("seat.Role = %q but the system prompt is the %s's", seat.Role, got)
		}
		role := string(seat.Role)
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

// TestRunHedgedCritiqueIsNotMisreadAsConcession is the P43.1 regression: a
// critique that uses the word "concede" mid-sentence while actually raising a
// substantiated challenge must not be misdetected as a full concession — that
// would discard the challenge and wrongly tell the arbiter the critic backed
// down, per security-arbiter.md's "conceded round counts in the claim's
// favor" instruction.
func TestRunHedgedCritiqueIsNotMisreadAsConcession(t *testing.T) {
	hedged := "I won't concede this point — the claim is missing a rate limit check, see api.go:42."
	run := scriptedRun(t, map[string][]string{
		"critic": {hedged},
		"proposer": {
			"Rate limiting is enforced upstream by the gateway, not api.go.",
		},
		"arbiter": {
			"VERDICT: REVISE\nCONFIDENCE: medium\nREASON: The rebuttal doesn't fully address round 1.",
		},
	})

	tr, err := Run(context.Background(), "Claim under test.", Config{MaxRounds: 1}, run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tr.Rounds) != 1 {
		t.Fatalf("len(Rounds) = %d, want 1", len(tr.Rounds))
	}
	if tr.Rounds[0].Conceded {
		t.Fatalf("Rounds[0].Conceded = true, want false — hedged mid-text \"concede\" must not count as a concession")
	}
	if tr.Rounds[0].Rebuttal == "" {
		t.Fatalf("Rounds[0].Rebuttal is empty, want the proposer's rebuttal to have run (round was wrongly treated as conceded)")
	}
}

// TestConcedeRegexAnchoredToStart directly exercises isConcession against the
// two shapes concedeRe must tell apart: the compliant opening keyword the
// critic persona instructs, and the same word buried mid-sentence in a
// hedge/negation.
func TestConcedeRegexAnchoredToStart(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		expect bool
	}{
		{"compliant", "CONCEDE — I found no defensible flaw in this claim.", true},
		{"compliant lowercase", "concede, the claim holds.", true},
		{"markdown bold", "**CONCEDE** — no flaw found.", true},
		{"leading whitespace", "  \n CONCEDE.", true},
		{"negated mid-text", "I won't concede this point — see api.go:42.", false},
		{"concede later in sentence", "The proposer might concede the edge case, but the core flaw remains.", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConcession(tc.text); got != tc.expect {
				t.Errorf("isConcession(%q) = %v, want %v", tc.text, got, tc.expect)
			}
		})
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

// TestRunMaxRoundsHardCeiling covers P32.4: Config.MaxRounds must be clamped
// to MaxRoundsCeiling regardless of how large a caller (an HTTP request, or a
// model turn steered by prompt-injected content the claim was grounded in via
// WithFiles) requests — nothing upstream of withDefaults bounded it before
// this fix. Scripts exactly MaxRoundsCeiling non-conceding responses per role;
// if the requested 9999 rounds weren't clamped, scriptedRun would run out of
// responses and fail via its own t.Fatalf.
func TestRunMaxRoundsHardCeiling(t *testing.T) {
	byRole := map[string][]string{}
	for i := 0; i < MaxRoundsCeiling; i++ {
		byRole["critic"] = append(byRole["critic"], "Evidence: file.go:1 shows a gap.")
		byRole["proposer"] = append(byRole["proposer"], "Addressed.")
	}
	byRole["arbiter"] = []string{"VERDICT: UPHOLD\nCONFIDENCE: low\nREASON: exhausted rounds."}

	tr, err := Run(context.Background(), "Claim.", Config{MaxRounds: 9999}, scriptedRun(t, byRole))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tr.Rounds) != MaxRoundsCeiling {
		t.Fatalf("len(Rounds) = %d, want MaxRoundsCeiling=%d", len(tr.Rounds), MaxRoundsCeiling)
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
	run := func(_ context.Context, _ Seat, systemPrompt, _ string) (string, error) {
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
	run := func(_ context.Context, _ Seat, systemPrompt, _ string) (string, error) {
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

// TestRunReportsEachSeat pins the P69.1 contract every per-role model
// resolution rests on: each call arrives tagged with the role it is for and
// with the *resolved* persona name for that role — after Domain defaults and
// per-role overrides — since that name is the key a caller resolves a model
// from. A seat carrying the wrong persona name routes a role to another role's
// model, which changes nothing observable in the transcript and everything
// about which weights are resident.
func TestRunReportsEachSeat(t *testing.T) {
	for _, tc := range []struct {
		name             string
		cfg              Config
		proposer, critic string
		arbiter          string
	}{
		{
			name:     "security domain defaults",
			cfg:      Config{MaxRounds: 2},
			proposer: DefaultProposerPersona, critic: DefaultCriticPersona, arbiter: DefaultArbiterPersona,
		},
		{
			name:     "generic domain defaults",
			cfg:      Config{Domain: DomainGeneric, MaxRounds: 2},
			proposer: GenericProposerPersona, critic: GenericCriticPersona, arbiter: GenericArbiterPersona,
		},
		{
			name:     "per-role override beats the domain default",
			cfg:      Config{Domain: DomainGeneric, MaxRounds: 2, ArbiterPersona: DefaultArbiterPersona},
			proposer: GenericProposerPersona, critic: GenericCriticPersona, arbiter: DefaultArbiterPersona,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seats []Seat
			run := func(_ context.Context, seat Seat, _, _ string) (string, error) {
				seats = append(seats, seat)
				if seat.Role == RoleArbiter {
					return "VERDICT: UPHOLD\nCONFIDENCE: high\nREASON: fine.", nil
				}
				return "Evidence: main.go:1 — a challenge.", nil
			}
			if _, err := Run(context.Background(), "Claim.", tc.cfg, run); err != nil {
				t.Fatalf("Run: %v", err)
			}

			want := []Seat{
				{Role: RoleCritic, Persona: tc.critic},
				{Role: RoleProposer, Persona: tc.proposer},
				{Role: RoleCritic, Persona: tc.critic},
				{Role: RoleProposer, Persona: tc.proposer, Last: true},
				{Role: RoleArbiter, Persona: tc.arbiter},
			}
			if len(seats) != len(want) {
				t.Fatalf("got %d calls, want %d: %+v", len(seats), len(want), seats)
			}
			for i := range want {
				if seats[i] != want[i] {
					t.Errorf("call %d: seat = %+v, want %+v", i+1, seats[i], want[i])
				}
			}
		})
	}
}

// TestRunMarksLastOnlyOnTheFinalRebuttal proves Seat.Last is not set on an
// earlier round — a caller using it to evict a model before the arbiter loads
// would otherwise swap mid-debate and pay a cold reload for every remaining
// round.
func TestRunMarksLastOnlyOnTheFinalRebuttal(t *testing.T) {
	var lastAt []int
	i := 0
	run := func(_ context.Context, seat Seat, _, _ string) (string, error) {
		i++
		if seat.Last {
			lastAt = append(lastAt, i)
		}
		if seat.Role == RoleArbiter {
			return "VERDICT: UPHOLD\nCONFIDENCE: high\nREASON: fine.", nil
		}
		return "Evidence: main.go:1 — a challenge.", nil
	}
	if _, err := Run(context.Background(), "Claim.", Config{MaxRounds: 3}, run); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 3 rounds = 6 calls, then arbitration; the 6th is the final rebuttal.
	if len(lastAt) != 1 || lastAt[0] != 6 {
		t.Fatalf("Seat.Last set on call(s) %v, want only call 6 (the final rebuttal)", lastAt)
	}
}

// TestParseVerdictTakesTheFinalRuling is the P69.1 reasoning-model regression:
// a model that drafts a verdict mid-deliberation and then revises it must be
// read as having ruled the revision, not the draft.
//
// This is not hypothetical. phi4-mini-reasoning:3.8b — a natural fit for the
// arbiter seat, which needs no tools — reports capabilities ["tools",
// "completion"] with no "thinking", so Ollama does not split its reasoning
// trace into a separate field and the whole deliberation arrives as content.
func TestParseVerdictTakesTheFinalRuling(t *testing.T) {
	for _, tc := range []struct {
		name, text, outcome, confidence string
	}{
		{
			name: "reasoning trace drafts then revises",
			text: "<think>\nRound 1 cited api.go:42. Draft answer:\n" +
				"VERDICT: UPHOLD\nCONFIDENCE: low\n" +
				"No wait, the rebuttal never addresses the cited line. Revising.\n</think>\n" +
				"VERDICT: REJECT\nCONFIDENCE: high\nREASON: Round 1's evidence went unrebutted.",
			outcome: "REJECT", confidence: "high",
		},
		{
			name:    "single verdict is unaffected",
			text:    "VERDICT: UPHOLD\nCONFIDENCE: medium\nREASON: no substantiated challenge.",
			outcome: "UPHOLD", confidence: "medium",
		},
		{
			name:    "no verdict at all stays empty",
			text:    "I could not reach a conclusion on this claim.",
			outcome: "", confidence: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := parseVerdict(tc.text)
			if v.Outcome != tc.outcome {
				t.Errorf("Outcome = %q, want %q", v.Outcome, tc.outcome)
			}
			if v.Confidence != tc.confidence {
				t.Errorf("Confidence = %q, want %q", v.Confidence, tc.confidence)
			}
		})
	}
}
