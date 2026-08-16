package compaction

import (
	"context"
	"testing"

	"github.com/fiddler110/aegis/internal/tokenest"
)

// TestShouldCompactUsesTheSharedTrigger is LLM-02's regression at this end of the
// seam.
//
// The flat 20%-free rule this replaced never saw maxTokens, so on a stock
// 4,096-token Ollama window the engine asked for a compaction at 2,048 estimated
// tokens and this package refused until 3,277 — meaning summarization finally ran
// with 819 tokens left for a completion the request had asked 32,768 for. The
// case is written at exactly that pair, because it is the shipped default one and
// the one the defect was measured on.
func TestShouldCompactUsesTheSharedTrigger(t *testing.T) {
	for _, tc := range []struct {
		name      string
		window    int
		maxTokens int
	}{
		{"the shipped default pair on a stock Ollama window", 4096, 32768},
		{"a mid-sized local window", 24576, 8192},
		{"a cloud-sized window with a modest cap", 131072, 8192},
		{"no completion budget configured", 32768, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{
				Adapter: &summaryAdapter{}, Model: "m",
				ContextWindow: tc.window,
				MaxTokens:     tc.maxTokens,
			})
			trigger := tokenest.CompactionTrigger(tc.window, tc.maxTokens)
			if s.shouldCompact(budget{}, trigger) {
				t.Errorf("compacted at the trigger %d exactly; the gate is strictly above it", trigger)
			}
			if !s.shouldCompact(budget{}, trigger+1) {
				t.Errorf("did not compact one token past the trigger %d", trigger)
			}
			// Where the pre-fix rule sat, so the direction of the change is on the
			// record rather than inferred. A flat 80% of the window is *later*
			// than the shared trigger on every window small enough for the
			// completion reservation to bind — which is the LLM-02 case, and the
			// only one where the summarizer used to refuse a compaction the engine
			// had asked for.
			//
			// Above that (a cloud-sized window with a modest cap) the shared
			// trigger is the 85% ceiling, so the summarizer now gates marginally
			// *later* than its old 80%. That is deliberate: the engine's gate was
			// already 85% there, and unifying means one of the two moves. The
			// engine's is the one sized against the completion, so it wins.
			flat := tc.window * 80 / 100
			switch {
			case trigger < flat:
				t.Logf("shared trigger %d is earlier than the old flat gate %d (the LLM-02 case)", trigger, flat)
			default:
				t.Logf("shared trigger %d is the 85%% ceiling, above the old flat gate %d", trigger, flat)
			}
		})
	}
}

// TestCallerSuppliedTriggerWins: the engine hands its own trigger down so the two
// gates cannot drift (P66.14). A Summarizer that recomputed one for itself would
// be back in the LLM-02 shape the moment the two configurations differ — which
// they do whenever the daemon's compaction model is not the run's model (P52.1).
func TestCallerSuppliedTriggerWins(t *testing.T) {
	const window = 24_576
	s := New(Options{
		Adapter: &summaryAdapter{}, Model: "m",
		ContextWindow: window,
		MaxTokens:     8_192,
	})
	own := tokenest.CompactionTrigger(window, 8_192)
	const callers = 9_000
	if callers >= own {
		t.Fatalf("fixture is not discriminating: the caller's trigger %d is not below the Summarizer's own %d", callers, own)
	}

	b := budgetFrom(WithTokenBudget(context.Background(), 0, 0, callers))
	if s.shouldCompact(b, callers) {
		t.Errorf("compacted at the caller's trigger %d exactly", callers)
	}
	if !s.shouldCompact(b, callers+1) {
		t.Errorf("did not compact one token past the caller's trigger %d — the Summarizer used its own %d", callers, own)
	}
}

// TestBudgetDegradesToThePreCorrectionBehaviour: every field of the per-call
// budget is optional, and a caller supplying none must read exactly the estimate
// this package produced before any of this existed. That is what keeps a
// caller-supplied Compactor, a test double, and ForceCompact from depending on
// the engine having decorated their context.
func TestBudgetDegradesToThePreCorrectionBehaviour(t *testing.T) {
	msgs := prunableConversation()
	s := New(Options{Adapter: &summaryAdapter{}, Model: "m", ContextWindow: 100_000})

	if got, want := s.estimate(budget{}, "sys", msgs), EstimateTokens("sys", msgs); got != want {
		t.Errorf("estimate with no budget = %d, want the raw heuristic %d", got, want)
	}
	// An undecorated context yields the same zero budget, so the two paths agree.
	if got := budgetFrom(context.Background()); got != (budget{}) {
		t.Errorf("budgetFrom on an undecorated context = %+v, want the zero budget", got)
	}
	// And a decoration that says nothing must not allocate a context value that
	// then reads back as a real budget.
	ctx := WithTokenBudget(context.Background(), 0, 0, 0)
	if got := budgetFrom(ctx); got != (budget{}) {
		t.Errorf("budgetFrom after an empty decoration = %+v, want the zero budget", got)
	}
}

// TestEstimateAppliesTheCorrection: the overhead is additive and the scale
// multiplicative, in that order — scaling the raw estimate and then adding the
// overhead would leave the schemas uncorrected, which is a quiet undercount of
// exactly the content P62.4 found missing.
func TestEstimateAppliesTheCorrection(t *testing.T) {
	msgs := prunableConversation()
	s := New(Options{Adapter: &summaryAdapter{}, Model: "m", ContextWindow: 100_000})
	raw := EstimateTokens("sys", msgs)

	b := budgetFrom(WithTokenBudget(context.Background(), 500, 1.5, 0))
	got := s.estimate(b, "sys", msgs)
	if want := int(float64(raw+500) * 1.5); got != want && got != want+1 {
		t.Errorf("corrected estimate = %d, want %d (rounded up) — (raw + overhead) x scale", got, want)
	}
	if wrong := int(float64(raw)*1.5) + 500; got == wrong {
		t.Errorf("estimate = %d, which is scale applied before the overhead — the schemas are uncorrected", got)
	}
}
