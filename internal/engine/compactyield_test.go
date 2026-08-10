package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// P62.7 behavioural tests. The fixture, the guard constructor and the
// measurement that motivated the numbers all live in compactthrash_test.go.

// runThrashTurns grows the fixture one agent turn at a time, runs the guard's
// per-turn check, and returns the turn numbers at which a compaction was
// actually applied (a notice was emitted, the conversation was rewritten and
// its cached token total invalidated). The *sequence* is what these tests
// assert: a count cannot tell when something fired, and P62.7 is entirely
// about when.
func runThrashTurns(t *testing.T, g *compactionGuard, f *thrashFixture, turns int) []int {
	t.Helper()
	var applied []int
	for turn := 1; turn <= turns; turn++ {
		f.grow(false)
		var notice bool
		g.beforeTurn(context.Background(), f.conv, func(ev Event) {
			if ev.Kind == KindNotice && strings.Contains(ev.Text, "compacted") {
				notice = true
			}
		}, false)
		if notice {
			applied = append(applied, turn)
		}
	}
	return applied
}

// TestLowYieldPruneStopsRecompactingEveryTurn is the P62.7 regression, on the
// same fixture the measurement runs — the only difference is that this guard
// talks to the compactor through the yield-reporting seam instead of through a
// wrapper that hides it.
//
// Pre-fix that fixture applied a compaction on eleven consecutive turns (5..15),
// each freeing 45 estimated tokens against a gap that grew from 1,462 to 4,332,
// before the twelfth finally reached the summarizer and did real work. The
// assertion is the exact sequence rather than the count: the whole item is about
// *when* compaction runs.
func TestLowYieldPruneStopsRecompactingEveryTurn(t *testing.T) {
	g := newGuardFor(t, realSummarizer())
	f := newThrashFixture(compactionTrigger(thrashWindow, thrashMaxTokens))

	applied := runThrashTurns(t, g, f, 20)

	want := []int{5, 10, 16}
	if fmt.Sprint(applied) != fmt.Sprint(want) {
		t.Errorf("compactions applied on turns %v, want %v", applied, want)
	}
	// The last of them has to be the real thing: suppression may delay a
	// summarization but must never prevent one.
	if len(f.conv.Messages) > 20 {
		t.Errorf("conversation still %d messages — the summarizer never ran, so suppression is not degrading gracefully", len(f.conv.Messages))
	}
}

// stubYieldCompactor reports a fixed yield and rewrites nothing, so the
// conversation stays over the trigger and what the test observes is the guard's
// decision rather than the fixture's arithmetic.
type stubYieldCompactor struct {
	freed      int
	summarized bool
	calls      int
}

func (c *stubYieldCompactor) Compact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	out, changed, _, _, err := c.CompactYield(ctx, system, msgs)
	return out, changed, err
}

func (c *stubYieldCompactor) CompactYield(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, bool, int, error) {
	c.calls++
	return msgs, true, c.summarized, c.freed, nil
}

// TestGoodYieldPruneStillCompactsEveryTurn: the minimum-yield rule may only
// suppress compactions that are not paying for themselves. One that frees more
// than its share of the gap is left alone, on every turn, exactly as before.
func TestGoodYieldPruneStillCompactsEveryTurn(t *testing.T) {
	comp := &stubYieldCompactor{freed: 100_000}
	g := newGuardFor(t, comp)
	f := newThrashFixture(compactionTrigger(thrashWindow, thrashMaxTokens))

	applied := runThrashTurns(t, g, f, 8)

	want := []int{1, 2, 3, 4, 5, 6, 7, 8}
	if fmt.Sprint(applied) != fmt.Sprint(want) {
		t.Errorf("compactions applied on turns %v, want every turn %v", applied, want)
	}
}

// TestSummarizationIsNeverSuppressed: a compaction that reached the LLM
// summarizer is worth its price by construction, however the yield arithmetic
// comes out — it is the pass that keeps a run from overflowing at all.
func TestSummarizationIsNeverSuppressed(t *testing.T) {
	comp := &stubYieldCompactor{freed: 0, summarized: true}
	g := newGuardFor(t, comp)
	f := newThrashFixture(compactionTrigger(thrashWindow, thrashMaxTokens))

	applied := runThrashTurns(t, g, f, 6)

	want := []int{1, 2, 3, 4, 5, 6}
	if fmt.Sprint(applied) != fmt.Sprint(want) {
		t.Errorf("compactions applied on turns %v, want every turn %v", applied, want)
	}
}

// TestGrowthPastRecordedThresholdReEnablesCompaction: suppression is a bet that
// re-running the pre-pass over nearly the same conversation will yield nearly
// the same nothing. Once the conversation has grown by the gap the bet was made
// against, the bet is off — and until then not even Compact is called, which is
// what makes the deferral free.
func TestGrowthPastRecordedThresholdReEnablesCompaction(t *testing.T) {
	comp := &stubYieldCompactor{freed: 1}
	g := newGuardFor(t, comp)
	f := newThrashFixture(compactionTrigger(thrashWindow, thrashMaxTokens))

	// Run until a low-yield prune records a retry threshold more than one turn
	// of growth away. The first crossing of the trigger is always a near miss —
	// the conversation crosses by whatever one turn adds — so it defers by about
	// one turn and cannot show the difference between deferring and not.
	var retryAt int
	for turn := 1; turn <= 30 && retryAt == 0; turn++ {
		runThrashTurns(t, g, f, 1)
		if g.retryEstimate-g.estimate(f.conv) > 2*thrashTurnTokens {
			retryAt = g.retryEstimate
		}
	}
	if retryAt == 0 {
		t.Fatal("no low-yield prune ever recorded a retry threshold worth more than two turns of growth")
	}

	callsBefore := comp.calls
	if applied := runThrashTurns(t, g, f, 1); applied != nil {
		t.Errorf("compaction applied (%v) on the turn after a low-yield prune, while suppressed", applied)
	}
	if comp.calls != callsBefore {
		t.Errorf("Compact called %d times while suppressed, want 0", comp.calls-callsBefore)
	}
	if g.estimate(f.conv) >= retryAt {
		t.Fatalf("the fixture grew past the retry threshold (%d) in a single turn — it cannot demonstrate suppression", retryAt)
	}

	for g.estimate(f.conv) < retryAt {
		f.grow(false)
	}
	var applied bool
	g.beforeTurn(context.Background(), f.conv, func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "compacted") {
			applied = true
		}
	}, false)
	if !applied {
		t.Errorf("no compaction once the conversation grew past the recorded threshold %d (estimate %d)", retryAt, g.estimate(f.conv))
	}
}

// TestMinimumYieldBoundary pins the rule itself: which side of
// minPruneYieldFraction a yield falls on, and how far the resulting deferral
// reaches. The sequence tests above show the rule working end to end but would
// survive a nudge of the fraction to an adjacent value, because the yields they
// see (45 tokens against a 1,462-token gap; 100,000 against anything) are
// nowhere near the boundary. This is the test that fails when the constant
// moves.
func TestMinimumYieldBoundary(t *testing.T) {
	const (
		trigger = 10_000
		gap     = 1_000
		est     = trigger + gap
	)
	for _, tc := range []struct {
		name        string
		oc          compactOutcome
		wantRetryAt int
	}{
		// A quarter of the gap is the first yield that is good enough.
		{"just under the fraction", compactOutcome{applied: true, yieldKnown: true, freedTokens: gap/4 - 1}, est + gap},
		{"exactly the fraction", compactOutcome{applied: true, yieldKnown: true, freedTokens: gap / 4}, 0},
		// Half a gap is well clear of it, and would be *suppressed* if the
		// fraction were raised to 0.5.
		{"half the gap", compactOutcome{applied: true, yieldKnown: true, freedTokens: gap / 2}, 0},
		// A tenth is well under it, and would be *allowed* if the fraction were
		// lowered to 0.05.
		{"a tenth of the gap", compactOutcome{applied: true, yieldKnown: true, freedTokens: gap / 10}, est + gap},
		{"nothing freed", compactOutcome{applied: true, yieldKnown: true, freedTokens: 0}, est + gap},
		// Never suppressed, whatever the arithmetic says.
		{"summarization", compactOutcome{applied: true, yieldKnown: true, summarized: true}, 0},
		{"nothing applied", compactOutcome{yieldKnown: true}, 0},
		{"yield unknown", compactOutcome{applied: true}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGuardFor(t, &stubYieldCompactor{})
			g.recordYield(tc.oc, est, trigger)
			if g.retryEstimate != tc.wantRetryAt {
				t.Errorf("retryEstimate = %d, want %d (freed %d of a %d-token gap)",
					g.retryEstimate, tc.wantRetryAt, tc.oc.freedTokens, gap)
			}
		})
	}
}

// TestGoodYieldClearsAnExistingSuppression: the rule is not a latch. A
// compaction that pays for itself lifts a deferral recorded by an earlier one
// that did not.
func TestGoodYieldClearsAnExistingSuppression(t *testing.T) {
	g := newGuardFor(t, &stubYieldCompactor{})
	g.retryEstimate = 99_999
	g.recordYield(compactOutcome{applied: true, yieldKnown: true, freedTokens: 500}, 11_000, 10_000)
	if g.retryEstimate != 0 {
		t.Errorf("retryEstimate = %d after a compaction that freed half the gap, want 0", g.retryEstimate)
	}
}

// silentCompactor speaks the yield seam but never finds anything to do — the
// "nothing left to compact" case the 95% notice exists for.
type silentCompactor struct{}

func (silentCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	return msgs, false, nil
}

func (silentCompactor) CompactYield(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, bool, int, error) {
	return msgs, false, false, 0, nil
}

// TestContextFullNoticeSurvivesSuppression re-arms the P62.4 guard against
// P62.7's new early return. The one warning designed to fire when compaction
// *cannot* help must not be silenced by a rule about compaction not being
// *worth* it — so suppression stops at compactionSuppressCeiling, which 95% of
// any window is always above.
func TestContextFullNoticeSurvivesSuppression(t *testing.T) {
	g := newGuardFor(t, silentCompactor{})
	f := newThrashFixture(compactionTrigger(thrashWindow, thrashMaxTokens))

	// Pin suppression on, as a low-yield prune would, with a threshold so high
	// that nothing but the ceiling could clear it.
	g.retryEstimate = 1 << 30

	for g.estimate(f.conv) < thrashWindow*95/100 {
		f.grow(false)
	}
	var notice string
	g.beforeTurn(context.Background(), f.conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notice = ev.Text
		}
	}, false)
	if !strings.Contains(notice, "nothing left to compact") {
		t.Errorf("notice = %q, want the context-full warning", notice)
	}
}

// TestSuppressionCeilingIsBelowNinetyFivePercent states the invariant the test
// above leans on, for every window/max-tokens pair the trigger supports rather
// than just the fixture's: the ceiling that ends suppression is always reached
// before 95% of the window, so the notice path can never be starved.
func TestSuppressionCeilingIsBelowNinetyFivePercent(t *testing.T) {
	for _, win := range []int{2048, 4096, 8192, 24576, 32768, 131072, 262144} {
		for _, maxTokens := range []int{0, 512, 4096, 32768, 131072} {
			trigger := compactionTrigger(win, maxTokens)
			if ceiling := compactionSuppressCeiling(win, trigger); ceiling >= win*95/100 {
				t.Errorf("window=%d maxTokens=%d: suppression ceiling %d is at or above 95%% of the window (%d)",
					win, maxTokens, ceiling, win*95/100)
			}
		}
	}
}
