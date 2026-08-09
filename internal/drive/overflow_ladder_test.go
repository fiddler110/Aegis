package drive

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// overflowAdapter fails every turn with a context-overflow error and records
// what the model was asked on each attempt.
//
// Recording the *request* is the point. Every existing test of this ladder
// checks a piece in isolation — the directive escalates, freshPhaseConv carries
// one, the budget constant is bounded, the stop notice reads correctly — and
// each of those can pass while the loop wiring them together does something
// else entirely. What the model actually sees on retry N is the only place the
// whole mechanism is observable.
type overflowAdapter struct {
	seen []string // the full user-visible seed text of each request, in order
	// failFor bounds how many turns overflow; later turns end cleanly. Zero
	// means every turn overflows.
	failFor int
	calls   int
}

func (o *overflowAdapter) Name() string { return "overflow" }

func (o *overflowAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	var b strings.Builder
	for _, m := range req.Messages {
		for _, blk := range m.Content {
			if t, ok := blk.(provider.TextBlock); ok {
				b.WriteString(t.Text)
				b.WriteString("\n")
			}
		}
	}
	o.seen = append(o.seen, b.String())
	o.calls++

	if o.failFor == 0 || o.calls <= o.failFor {
		// The hard-reject shape of an overflow. IsContextOverflowError reads
		// APIError.Message through the shared classifier ladder, so this is the
		// same classification a real Ollama/OpenAI rejection produces.
		return nil, &provider.APIError{
			Provider: "overflow",
			Message:  "input is too large for the model's context window",
		}
	}
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: "ok"}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

// overflowLadderState builds a drive whose findings phase can never complete —
// its one file keeps its PENDING marker — against an engine that overflows.
func overflowLadderState(t *testing.T, ad *overflowAdapter) (*State, string) {
	t.Helper()

	cwd := t.TempDir()
	runDir := filepath.Join(cwd, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The marker is what keeps the phase incomplete, so the loop keeps retrying
	// rather than exiting on success. It is also what the reset re-reads from
	// disk, which is the mechanism the ladder rides on.
	if err := os.WriteFile(filepath.Join(runDir, "3-findings.md"),
		[]byte("# Findings\n\n<!-- PENDING: findings -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	eng, err := engine.New(engine.Options{
		Adapter: ad, Tools: tool.NewRegistry(), Model: "test", MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	var toolCalls, mutations int
	st := &State{
		Engine:        eng,
		System:        "SYSTEM PROMPT",
		OnEvent:       func(engine.Event) {},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ErrOut:        &strings.Builder{},
		Cwd:           cwd,
		SkillName:     "threat-modeling",
		SkillDir:      filepath.Join(cwd, ".aegis", "builtin-skills", "threat-modeling"),
		TaskPrompt:    "threat model this repo",
		MaxTurns:      50, // well above the reset budget, so the budget is what stops it
		RunDir:        func(string) string { return runDir },
		IterToolCalls: &toolCalls,
		IterMutations: &mutations,
	}
	return st, runDir
}

// TestOverflowLadderClimbsThenStops is P62.5: the ladder driven end to end
// through drive.Run, asserting the *sequence* the model is retried with.
//
// P62.3 shipped OverflowEscalationDirective and maxPhaseOverflowResets with no
// live evidence they ever fire, and their unit tests cover each part in
// isolation. The gap that leaves is specific: nothing checked that reset N
// actually carries rung N. A loop that passed "" on every reset, or the same
// rung every time, would keep every one of those tests green — and passing ""
// on reset is the exact bug P62.3 was filed to fix, so it is a live possibility
// rather than a hypothetical.
//
// Sequence, not count, deliberately: this is the P63.9 lesson that a count
// assertion cannot tell *when* something fired, and three of six mutations
// survived a suite that only counted.
func TestOverflowLadderClimbsThenStops(t *testing.T) {
	ad := &overflowAdapter{}
	st, _ := overflowLadderState(t, ad)
	findings, ok := phaseByName(ThreatModelPhases, "findings")
	if !ok {
		t.Fatal("findings phase missing from ThreatModelPhases")
	}

	// A resumable stop returns nil, matching the generic drive's contract — the
	// caller's tail logic runs either way.
	if err := Run(context.Background(), st, []Phase{findings}); err != nil {
		t.Fatalf("Run returned %v, want nil (a bounded overflow stop is resumable, not fatal)", err)
	}

	// One initial attempt plus exactly maxPhaseOverflowResets retries. The
	// (max+1)'th overflow is what trips the budget, and it must not buy another
	// turn.
	wantAttempts := maxPhaseOverflowResets + 1
	if len(ad.seen) != wantAttempts {
		t.Fatalf("the model was asked %d times, want %d (1 initial + %d resets); "+
			"seeing more means the budget did not bind, fewer means a reset was skipped",
			len(ad.seen), wantAttempts, maxPhaseOverflowResets)
	}

	// Attempt 0 is the phase's own prompt and must not carry a directive: a
	// first attempt paying for an escalation nobody has earned would mean the
	// ladder starts a rung too high.
	if strings.Contains(ad.seen[0], "TOO LARGE FOR ONE TURN") {
		t.Error("the first attempt carried an escalation directive; there would be nothing left to escalate to")
	}

	// Each retry must carry its own rung, in order.
	for reset := 1; reset <= maxPhaseOverflowResets; reset++ {
		got := ad.seen[reset]
		want := OverflowEscalationDirective(reset, maxPhaseOverflowResets)
		if !strings.Contains(got, want) {
			t.Errorf("retry %d does not carry rung %d of the ladder.\nwant to contain:\n%s\n\ngot:\n%s",
				reset, reset, want, got)
		}
		// And it must still name the file it is resuming, or the reset has
		// thrown away the work rather than continuing it.
		if !strings.Contains(got, "3-findings.md") {
			t.Errorf("retry %d lost the PENDING file it is resuming:\n%s", reset, got)
		}
	}

	// The rungs must differ from each other in the run itself, not just when
	// called directly — an off-by-one that passed the same reset number every
	// time would satisfy every check above except this one.
	if ad.seen[1] == ad.seen[2] {
		t.Error("retries 1 and 2 were identical: the loop is not advancing the reset counter, " +
			"which is the failure P62.3 exists to fix (five identical truncations, zero findings written)")
	}

	// The stop must be attributed to phase size, not to --max-turns; the two
	// call for different remedies and MaxTurns here is nowhere near exhausted.
	notice := st.ErrOut.(*strings.Builder).String()
	if !strings.Contains(notice, "overflowed the context") {
		t.Errorf("no phase-overflow stop notice was emitted; got:\n%s", notice)
	}
	if strings.Contains(notice, "--max-turns") {
		t.Errorf("the stop was blamed on --max-turns, but %d of %d turns were used:\n%s",
			len(ad.seen), st.MaxTurns, notice)
	}
}

// TestOverflowLadderResetsBudgetOnRecovery: the budget is per phase and is spent
// only by overflows. A phase that overflows a couple of times and then makes
// progress must not carry those resets forward as though it were still failing —
// otherwise a long phase with intermittent overflows is stopped by history
// rather than by its current behavior.
func TestOverflowLadderResetsBudgetOnRecovery(t *testing.T) {
	// Overflow twice, then succeed. The phase file keeps its PENDING marker, so
	// the drive stops on the no-progress guard rather than on the overflow
	// budget — which is the distinction being asserted.
	ad := &overflowAdapter{failFor: 2}
	st, _ := overflowLadderState(t, ad)
	findings, _ := phaseByName(ThreatModelPhases, "findings")

	if err := Run(context.Background(), st, []Phase{findings}); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}

	notice := st.ErrOut.(*strings.Builder).String()
	if strings.Contains(notice, "overflowed the context") {
		t.Errorf("the phase stopped on the overflow budget after recovering from its overflows; "+
			"the budget should only be spent by overflows that are still happening:\n%s", notice)
	}
	if len(ad.seen) < 3 {
		t.Errorf("the drive gave up after %d attempts without ever getting past the overflows", len(ad.seen))
	}
}
