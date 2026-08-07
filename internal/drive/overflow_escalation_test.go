package drive

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestOverflowEscalationDirective_NamesTheRealFailure pins the substance of the
// directive, not just its presence. A 2026-08-07 live run against an external repo failed because
// a bare reset re-derived an identical oversized plan; the directive only earns
// its place if it tells the model the SIZE of one response was the problem and
// forbids re-announcing a whole-file plan. A future edit that softens it back
// into a generic "try again" should fail here.
func TestOverflowEscalationDirective_NamesTheRealFailure(t *testing.T) {
	d := strings.ToLower(OverflowEscalationDirective(1, maxPhaseOverflowResets))

	for _, want := range []string{
		"too large for one turn", // the cause, stated as size not count
		"cut off mid-tool-call",  // what the model actually observed
		"not the number of files left",
		"do not restate a plan for the whole file",
		"single next outstanding item",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("escalation directive must contain %q; got:\n%s", want, d)
		}
	}
}

// TestOverflowEscalationDirective_Escalates is the point of the whole change: a
// reset that says the same thing every time is what produced five identical
// truncations. Each level must add a strictly harder bound.
func TestOverflowEscalationDirective_Escalates(t *testing.T) {
	first := OverflowEscalationDirective(1, maxPhaseOverflowResets)
	second := OverflowEscalationDirective(2, maxPhaseOverflowResets)
	last := OverflowEscalationDirective(maxPhaseOverflowResets, maxPhaseOverflowResets)

	if first == second {
		t.Error("reset 2 must escalate beyond reset 1, otherwise the retry is identical — the exact bug this fixes")
	}
	if !strings.Contains(strings.ToLower(second), "exactly one") {
		t.Errorf("reset 2 must bound the turn to a single edit_file call; got:\n%s", second)
	}
	if strings.Contains(strings.ToLower(first), "exactly one") {
		t.Error("reset 1 should not already impose the one-edit bound; there would be nothing left to escalate to")
	}
	if !strings.Contains(strings.ToLower(last), "last reset") {
		t.Errorf("the final reset must warn that the phase stops next time; got:\n%s", last)
	}
	if len(second) <= len(first) || len(last) <= len(second) {
		t.Errorf("directive must grow with the reset count: %d, %d, %d", len(first), len(second), len(last))
	}
}

// TestFreshPhaseConv_CarriesEscalationDirective proves the directive actually
// reaches the model: freshPhaseConv prepends the nudge ahead of the continuation
// prompt, so a reset conversation must carry both. Before this change the
// overflow path passed "" here, which is why every retry was identical.
func TestFreshPhaseConv_CarriesEscalationDirective(t *testing.T) {
	st := &State{
		System:     "SYSTEM PROMPT",
		ErrOut:     io.Discard,
		Cwd:        "/ws",
		SkillDir:   "/ws/.aegis/builtin-skills/threat-modeling",
		TaskPrompt: "threat model this repo",
		MaxTurns:   50,
	}
	ph := ThreatModelPhases[3] // findings — the phase that looped live
	nudge := OverflowEscalationDirective(2, maxPhaseOverflowResets)

	conv := st.freshPhaseConv(ph, "/ws/.aegis/security/threat-model/run", []string{"3-findings.md"}, nudge)
	seed := convSeedText(t, conv)

	if !strings.Contains(seed, "TOO LARGE FOR ONE TURN") {
		t.Errorf("reset conversation must carry the escalation directive; got:\n%s", seed)
	}
	if !strings.Contains(seed, "3-findings.md") {
		t.Error("reset conversation must still name the PENDING file it is resuming")
	}
	if idx, jdx := strings.Index(seed, "TOO LARGE"), strings.Index(seed, "3-findings.md"); idx > jdx {
		t.Error("escalation directive must come first, before the continuation prompt — it is what reframes the retry")
	}
}

// TestPhaseOverflowBudgetIsBounded guards the second half of the fix. The
// content-phase overflow path previously had no reset counter at all: it was
// bounded only by --max-turns, so a non-convergent phase consumed the entire
// budget (75 turns and 50 minutes, live) before stopping with a misleading
// "hit --max-turns" reason. The cap must be small enough to fail fast.
func TestPhaseOverflowBudgetIsBounded(t *testing.T) {
	if maxPhaseOverflowResets <= 0 {
		t.Fatal("content-phase overflow resets must be bounded, or a non-convergent phase loops until --max-turns")
	}
	if maxPhaseOverflowResets > 5 {
		t.Errorf("maxPhaseOverflowResets = %d is too generous: each reset costs a full re-read and re-fill attempt",
			maxPhaseOverflowResets)
	}
	// The directive's hardest bound must be reachable within the budget,
	// otherwise the escalation ladder has a rung the drive never climbs.
	if got := OverflowEscalationDirective(maxPhaseOverflowResets, maxPhaseOverflowResets); !strings.Contains(strings.ToLower(got), "exactly one") {
		t.Error("the final reset must have reached the one-edit-per-turn bound")
	}
}

// TestStopPhaseOverflowExplainsItself checks the terminal notice distinguishes
// itself from stopMaxTurns. "Re-run to resume" is the right advice for running
// out of turns and the wrong advice here — a phase too large for the window
// fails identically on re-run unless something changes.
func TestStopPhaseOverflowExplainsItself(t *testing.T) {
	var out strings.Builder
	st := &State{ErrOut: &out, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), MaxTurns: 150}
	st.stopPhaseOverflow(ThreatModelPhases[3], []string{"3-findings.md"})

	got := out.String()
	for _, want := range []string{"overflowed the context", "one edit per turn", "3-findings.md", "resumable"} {
		if !strings.Contains(got, want) {
			t.Errorf("stopPhaseOverflow notice must contain %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "--max-turns") {
		t.Error("stopPhaseOverflow must not blame --max-turns; the cause is phase size, and the remedy is different")
	}
	if !strings.Contains(got, "larger context window") {
		t.Error("notice should name a lever that actually changes the outcome")
	}
}
