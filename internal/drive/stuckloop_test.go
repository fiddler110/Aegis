package drive

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
)

// loopAbort builds the error the engine's loop guard returns, in the shape a
// drive actually receives it (the sentinel wrapped with the turn count).
func loopAbort() error {
	return fmt.Errorf("%w: identical tool calls repeated 5 turns — the corrective prompt did not break the cycle", engine.ErrLoopDetected)
}

// TestRecoverReasoningLoop is the P57.1 verdict guard: a loop-guard abort is
// resumable at the phase level (the fresh context is what drops the wrong theory
// the model was looping on), bounded by its own budget so a model that loops
// every attempt still terminates, and it never escalates the serving window —
// the window is not what failed. A non-loop error is untouched.
func TestRecoverReasoningLoop(t *testing.T) {
	escalated := false
	st := newTestPhaseState(func() (int, bool) { escalated = true; return 131072, true })

	// A non-loop error is not handled here and must not consume a reset.
	resets := 0
	if got := st.recoverReasoningLoop(errors.New("boom"), "findings re-entry", &resets); got != overflowNotHandled {
		t.Errorf("non-loop error = %v, want overflowNotHandled", got)
	}
	if resets != 0 {
		t.Errorf("a non-loop error must not consume a reset; resets = %d", resets)
	}

	// Within budget: retry with the escalated prompt (loopRetry, not the plain
	// overflowRetry — the reset alone is what failed to help the 2026-08-03 run).
	for i := 1; i <= maxReasoningLoopResets; i++ {
		if got := st.recoverReasoningLoop(loopAbort(), "findings re-entry", &resets); got != loopRetry {
			t.Fatalf("loop reset %d = %v, want loopRetry", i, got)
		}
	}
	// One past the budget: stop cleanly rather than re-entering forever.
	if got := st.recoverReasoningLoop(loopAbort(), "findings re-entry", &resets); got != overflowStop {
		t.Errorf("loop past budget = %v, want overflowStop", got)
	}
	if escalated {
		t.Error("a reasoning loop must not escalate the serving window — the context window is not the failure")
	}
}

// TestLoopAbortIsClassifiable pins the seam P57.1 depends on: the engine's loop
// abort must be matchable with errors.Is rather than by message text, and it
// must not be confused with the other two aborts the drive treats differently.
func TestLoopAbortIsClassifiable(t *testing.T) {
	err := loopAbort()
	if !errors.Is(err, engine.ErrLoopDetected) {
		t.Fatal("a loop abort must wrap engine.ErrLoopDetected")
	}
	if errors.Is(err, engine.ErrToolFailureLimit) || errors.Is(err, engine.ErrWallClockLimit) {
		t.Error("a loop abort must not match the tool-failure or wall-clock sentinels")
	}
	// And the reverse: a tool-failure stall must not be recovered as a loop, or
	// the two budgets would spend each other.
	stall := fmt.Errorf("%w (6 in a row): edit_file keeps failing", engine.ErrToolFailureLimit)
	st := newTestPhaseState(nil)
	resets := 0
	if got := st.recoverReasoningLoop(stall, "findings phase", &resets); got != overflowNotHandled {
		t.Errorf("tool-failure stall classified as a loop (%v) — the budgets must stay separate", got)
	}
}

// TestStuckLoopDirective guards the P57.1 escalation text. The whole point is
// the shift from "figure out what's wrong" to "here is what's wrong", so it must
// say the report is authoritative, forbid re-deriving it, and name the specific
// trap the 2026-08-03 run fell into (an invented identifier-numbering scheme).
func TestStuckLoopDirective(t *testing.T) {
	withReport := StuckLoopDirective(true)
	for _, want := range []string{
		"STOP RE-DERIVING",
		"verification report below is the FINDING",
		"ground truth",
		"padding",   // the invented T0-vs-T01 offset
		"edit_file", // it must still end in an action
		"leave it and fix the others",
	} {
		if !strings.Contains(withReport, want) {
			t.Errorf("stuck directive missing %q; got:\n%s", want, withReport)
		}
	}

	// A content phase has no verifier report in its prompt, so pointing at one
	// would invite the model to invent the very thing the directive removes.
	noReport := StuckLoopDirective(false)
	if strings.Contains(noReport, "verification report below") {
		t.Error("the content-phase directive must not cite a report its prompt does not carry")
	}
	if !strings.Contains(noReport, "ground truth") {
		t.Error("the content-phase directive must still name its own ground truth (the PENDING file list)")
	}

	// It must not duplicate the anti-narration nudge: the stuck model was
	// calling tools every turn, which is the opposite failure.
	if strings.Contains(withReport, "STOP NARRATING") {
		t.Error("the stuck directive must not repeat ActNowNudge's anti-narration text")
	}
}

// TestPhase6TurnPrompt_StuckPrefix is the wiring guard for the phase-6 half: the
// directive leads the message (before the orientation preamble, so it is read
// first), and it appears only after a loop abort — an ordinary fix turn must not
// pay for it.
func TestPhase6TurnPrompt_StuckPrefix(t *testing.T) {
	const runDir = "/ws/.aegis/security/threat-model/stride-app-2026-08-03-1200"
	const skillDir = "/ws/.aegis/builtin-skills/threat-modeling"
	fix := VerifyFixPrompt(sampleVerifyReport)

	normal := phase6TurnPrompt(runDir, skillDir, fix, false)
	if strings.Contains(normal, "STOP RE-DERIVING") {
		t.Error("an ordinary phase-6 turn must not carry the stuck directive")
	}

	stuck := phase6TurnPrompt(runDir, skillDir, fix, true)
	if !strings.HasPrefix(stuck, StuckLoopDirective(true)) {
		t.Errorf("the stuck directive must lead the phase-6 message; got:\n%s", stuck)
	}
	// The turn's real instruction and orientation must survive the prefix.
	for _, want := range []string{runDir, "finding-bodies-nonempty", "edit_file"} {
		if !strings.Contains(stuck, want) {
			t.Errorf("escalated phase-6 prompt lost %q; got:\n%s", want, stuck)
		}
	}
}

// TestReentryConv_StuckPrefix is the same wiring guard for the P47.9 re-entry —
// the path the 2026-08-03 abort actually died on. The directive must reach the
// fresh re-entry context alongside, not instead of, the verifier evidence.
func TestReentryConv_StuckPrefix(t *testing.T) {
	st := &State{System: "SYSTEM PROMPT", SkillDir: "/ws/.aegis/builtin-skills/threat-modeling", Cwd: "/ws"}
	findings, _ := phaseByName(ThreatModelPhases, "findings")

	stuck := convSeedText(t, st.hollowReentryConv(findings, "/ws/run", sampleVerifyReport, StuckLoopDirective(true)))
	if !strings.HasPrefix(stuck, StuckLoopDirective(true)) {
		t.Errorf("the stuck directive must lead the re-entry message; got:\n%s", stuck)
	}
	if !strings.Contains(stuck, `FIND-01 "Evidence" section is empty`) {
		t.Error("the escalated re-entry must still carry the verifier evidence it is told to trust")
	}

	plain := convSeedText(t, st.hollowReentryConv(findings, "/ws/run", sampleVerifyReport, ""))
	if strings.Contains(plain, "STOP RE-DERIVING") {
		t.Error("a first re-entry must not carry the stuck directive")
	}
}

// TestFreshPhaseConv_StuckPrefix covers the content-phase path, which carries a
// PENDING list rather than a report and so must get the no-report variant.
func TestFreshPhaseConv_StuckPrefix(t *testing.T) {
	st := &State{System: "SYSTEM PROMPT", TaskPrompt: "tm this", SkillDir: "/ws/.aegis/builtin-skills/threat-modeling", Cwd: "/ws"}
	analysis := ThreatModelPhases[2]

	got := convSeedText(t, st.freshPhaseConv(analysis, "/ws/run", []string{"2-stride-analysis.md"}, StuckLoopDirective(false)))
	if !strings.HasPrefix(got, StuckLoopDirective(false)) {
		t.Errorf("the stuck directive must survive the per-turn reset onto the fresh phase context; got:\n%s", got)
	}
	if strings.Contains(got, "verification report below") {
		t.Error("a content phase must get the no-report directive variant")
	}
	if !strings.Contains(got, "still contain") {
		t.Error("the phase continuation prompt must still follow the directive")
	}
}
