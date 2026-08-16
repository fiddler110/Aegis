package tokenest

import "testing"

// TestCompactionTriggerIsTheEngineFormula pins the values engine.compactionTrigger
// produced before P66.14 moved the formula here. Moving a threshold is allowed;
// moving it silently while a rename hides the change is not — and the whole point
// of the move is that two packages now read these numbers.
func TestCompactionTriggerIsTheEngineFormula(t *testing.T) {
	for _, tc := range []struct {
		name      string
		window    int
		maxTokens int
		want      int
	}{
		// The shipped default pair on a stock Ollama install: half the window is
		// the floor, and this is the case the summarizer used to refuse until
		// 3,277 (LLM-02).
		{"shipped default on a stock window", 4096, 32768, 2048},
		{"large window, modest cap", 131072, 8192, 111411},
		{"cap equals window", 32768, 32768, 16384},
		{"sizing binds", 16384, 4096, 11469},
		{"no cap configured falls back to the ceiling", 8192, 0, 6963},
		{"no window means no trigger", 0, 32768, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompactionTrigger(tc.window, tc.maxTokens); got != tc.want {
				t.Errorf("CompactionTrigger(%d, %d) = %d, want %d", tc.window, tc.maxTokens, got, tc.want)
			}
		})
	}
}

// TestCompactionTriggerNeverExceedsTheCeiling is the regression guard the engine
// carried: the sizing may only ever compact *earlier* than the flat 85%, never
// later. Compacting later would be a new way to overrun a window, which is the
// failure the whole proactive-compaction path exists to prevent.
func TestCompactionTriggerNeverExceedsTheCeiling(t *testing.T) {
	for _, window := range []int{2048, 4096, 8192, 32768, 131072, 262144, 1_000_000} {
		for _, maxTokens := range []int{0, 512, 4096, 32768, 131072} {
			got := CompactionTrigger(window, maxTokens)
			if ceiling := window * 85 / 100; got > ceiling {
				t.Errorf("window=%d maxTokens=%d: trigger %d exceeds the %d ceiling", window, maxTokens, got, ceiling)
			}
			if got >= window {
				t.Errorf("window=%d maxTokens=%d: trigger %d must stay inside the window", window, maxTokens, got)
			}
		}
	}
}

// TestPruneTriggerStaysAheadOfTheCompactionTrigger: the pre-pass gate's only
// contract is its *order* relative to compaction — it exists to get a chance to
// bring the conversation back under budget before an LLM summarization call is
// reached, so a lead of zero (or a negative one) would silently retire the
// pre-pass rather than merely retune it.
//
// Both bounds are asserted, because either failure mode is quiet: a gate that
// coincides with the trigger never spares a summarizer call, and one that sits
// arbitrarily far below it prunes on a conversation with plenty of room, which is
// the prefill cost the gate was introduced to avoid.
func TestPruneTriggerStaysAheadOfTheCompactionTrigger(t *testing.T) {
	for _, window := range []int{4096, 8192, 24576, 131072, 262144, 1_000_000} {
		for _, maxTokens := range []int{0, 512, 8192, 32768} {
			trigger := CompactionTrigger(window, maxTokens)
			prune := CompactionPruneTrigger(window, maxTokens)
			if prune >= trigger {
				t.Errorf("window=%d maxTokens=%d: prune gate %d is not ahead of the trigger %d",
					window, maxTokens, prune, trigger)
			}
			if lead := trigger - prune; lead > trigger/2 {
				t.Errorf("window=%d maxTokens=%d: prune gate %d leads the trigger %d by %d, more than half of it",
					window, maxTokens, prune, trigger, lead)
			}
		}
	}
}

// TestPruneLeadSwitchesRegimeAtTheLargeWindow: below the threshold the lead is a
// share of the window, above it a fixed count. Asserted at the boundary itself,
// because a `>=` written as `>` (or the reverse) is exactly the kind of edge a
// round-numbered fixture cannot see.
func TestPruneLeadSwitchesRegimeAtTheLargeWindow(t *testing.T) {
	if got, want := CompactionPruneLead(LargeContextWindow), LargeContextWindow*5/100; got != want {
		t.Errorf("lead at the threshold = %d, want the ratio %d (the threshold itself is not 'large')", got, want)
	}
	if got, want := CompactionPruneLead(LargeContextWindow+1), largeContextPruneLead; got != want {
		t.Errorf("lead one past the threshold = %d, want the absolute %d", got, want)
	}
	if got := CompactionPruneLead(0); got != 0 {
		t.Errorf("lead with no window = %d, want 0", got)
	}
}
