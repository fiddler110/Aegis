package compaction

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
)

// prunableConversation builds a conversation with exactly one prunable item (a
// superseded read_file result) whose tail is chosen so boundary() returns 0.
// That keeps every case below about the prune gate alone: the LLM summarizer
// can never be reached, so an unexpected adapter call is unambiguously a bug
// rather than a side effect of the fixture's size.
func prunableConversation() []provider.Message {
	big := strings.Repeat("package foo // stale content that will be superseded\n", 200)
	return []provider.Message{
		toolUse("a", "read_file", json.RawMessage(`{"path":"foo.go"}`)),
		toolResult("a", big),
		toolUse("b", "read_file", json.RawMessage(`{"path":"foo.go"}`)),
		toolResult("b", "package foo // the current content\n"),
		text(provider.RoleUser, "thanks"),
		text(provider.RoleUser, "one more thing"),
	}
}

// staleResult returns the tool_result content of the superseded first read.
func staleResult(t *testing.T, msgs []provider.Message) string {
	t.Helper()
	tr, ok := msgs[1].Content[0].(provider.ToolResultBlock)
	if !ok {
		t.Fatalf("message 1 is not a tool result: %T", msgs[1].Content[0])
	}
	return tr.Content
}

// TestPrunePassUnconditionalByDefault is the no-change guarantee for cloud
// providers: with PreservePrefixCache unset, Compact's output is exactly what
// running the deterministic pass by hand produces, even with the conversation
// nowhere near the context window.
func TestPrunePassUnconditionalByDefault(t *testing.T) {
	msgs := prunableConversation()
	est := EstimateTokens("", msgs)
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{Adapter: a, Model: "m", ContextWindow: est * 10, KeepRecent: 2})

	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !changed {
		t.Fatal("expected the unconditional prune pass to report a change")
	}
	if a.called != 0 {
		t.Errorf("summarizer must not run this far from the window, called=%d", a.called)
	}
	want, prunedChars := pruneStaleToolResults(prunableConversation(), 2)
	if prunedChars == 0 {
		t.Fatal("fixture is not prunable")
	}
	if !reflect.DeepEqual(out, want) {
		t.Error("Compact output diverged from the plain prune pass with the option off")
	}
}

// TestPrunePassSkippedWithHeadroom is the P61.x fix: on a prefix-caching local
// backend with room to spare, the pass does not run, so no message in the
// middle of the conversation is rewritten and the server's KV cache survives.
func TestPrunePassSkippedWithHeadroom(t *testing.T) {
	msgs := prunableConversation()
	est := EstimateTokens("", msgs)
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{
		Adapter: a, Model: "m", KeepRecent: 2,
		ContextWindow:       est * 10, // ~10% full
		PreservePrefixCache: true,
	})

	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if changed {
		t.Error("expected no change: pruning here would cost a full prefill recompute for nothing")
	}
	if a.called != 0 {
		t.Errorf("summarizer must not run, called=%d", a.called)
	}
	if !reflect.DeepEqual(out, prunableConversation()) {
		t.Error("messages were rewritten despite ample headroom")
	}
	if got := staleResult(t, out); strings.Contains(got, "pruned") {
		t.Errorf("stale read was pruned despite headroom: %q", got)
	}
}

// TestPrunePassRunsNearWindow is the other half: the gate defers the pass, it
// does not remove it. Once the space the pass frees is worth the recompute, it
// still fires — the drive that motivated this change did hit a real context
// overflow later on, and must not be starved of headroom.
func TestPrunePassRunsNearWindow(t *testing.T) {
	msgs := prunableConversation()
	est := EstimateTokens("", msgs)
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{
		Adapter: a, Model: "m", KeepRecent: 2,
		// ~91% full, past the pre-pass gate, which since P66.14 sits 5% of the
		// window below the shared compaction trigger (85% here, with no
		// completion budget configured) rather than at a flat 25% free.
		ContextWindow:       est * 11 / 10,
		PreservePrefixCache: true,
	})

	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !changed {
		t.Fatal("expected the prune pass to run this close to the window")
	}
	if got := staleResult(t, out); !strings.Contains(got, "pruned") {
		t.Errorf("expected the superseded read to be pruned, got %q", got)
	}
}

// TestForceCompactAlwaysPrunes: a user-triggered compaction has asked for space
// explicitly, so the headroom gate never applies to it.
func TestForceCompactAlwaysPrunes(t *testing.T) {
	msgs := prunableConversation()
	est := EstimateTokens("", msgs)
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{
		Adapter: a, Model: "m", KeepRecent: 2,
		ContextWindow:       est * 10, // the same ample headroom that skips above
		PreservePrefixCache: true,
	})

	out, changed, err := s.ForceCompact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("ForceCompact: %v", err)
	}
	if !changed {
		t.Fatal("expected a forced compaction to prune regardless of headroom")
	}
	if got := staleResult(t, out); !strings.Contains(got, "pruned") {
		t.Errorf("expected the superseded read to be pruned, got %q", got)
	}
}

// TestShouldPruneGate pins the pre-pass gate against the shared trigger it
// trails (P66.14). The gate is one step ahead of compaction — 5% of the window,
// or 20k tokens on a large one — so every expectation here is
// tokenest.CompactionTrigger minus that lead, and the boundary cases are written
// as trigger±1 rather than as round numbers, which is what makes them able to
// tell adjacent thresholds apart.
//
// The numbers moved when the trigger did. Before P66.14 this gate read a flat
// 25%-free rule with no knowledge of maxTokens, which on a small window sat
// *later* than the engine's own compaction trigger — i.e. the pre-pass could not
// run before the summarizer it exists to spare was already being asked for.
func TestShouldPruneGate(t *testing.T) {
	// pruneAt is the gate the cases below are written against, restated from the
	// shared functions so a mutation to either is visible here as a failure
	// rather than as a silently-moved expectation.
	pruneAt := func(window, maxTokens int) int {
		return tokenest.CompactionTrigger(window, maxTokens) - tokenest.CompactionPruneLead(window)
	}

	cases := []struct {
		name      string
		preserve  bool
		window    int
		maxTokens int
		maxBudget int
		estimated int
		want      bool
	}{
		{"off always prunes far from window", false, 100_000, 0, 0, 1_000, true},
		{"off always prunes with no window", false, 0, 0, 120_000, 1_000, true},
		{"on skips with headroom", true, 100_000, 0, 0, 50_000, false},
		{"on prunes one token past the gate", true, 100_000, 0, 0, pruneAt(100_000, 0) + 1, true},
		{"on holds off one token below it", true, 100_000, 0, 0, pruneAt(100_000, 0), false},
		{"on skips on a large window above the lead", true, 400_000, 0, 0, 300_000, false},
		{"on prunes on a large window past the gate", true, 400_000, 0, 0, pruneAt(400_000, 0) + 1, true},
		// The maxTokens half, which is the LLM-02 fix reaching this gate: a
		// completion budget large enough to move the trigger moves the pre-pass
		// with it, rather than leaving the pre-pass stranded behind a threshold
		// the engine no longer uses.
		{"a completion budget moves the gate earlier", true, 100_000, 32_768, 0, pruneAt(100_000, 32_768) + 1, true},
		{"and still holds off below the moved gate", true, 100_000, 32_768, 0, pruneAt(100_000, 32_768), false},
		{"which is genuinely earlier than with no budget", true, 100_000, 32_768, 0, pruneAt(100_000, 0), true},
		// No window to measure against: keep the unconditional behaviour rather
		// than guess, since guessing wrong here costs an overflow.
		{"on prunes when the window is unknown", true, 0, 0, 120_000, 1_000, true},
		{"on prunes when compaction is disabled entirely", true, 0, 0, 0, 1_000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{
				Adapter: &summaryAdapter{}, Model: "m",
				ContextWindow:       tc.window,
				MaxTokens:           tc.maxTokens,
				MaxBudget:           tc.maxBudget,
				PreservePrefixCache: tc.preserve,
			})
			if got := s.shouldPrune(budget{}, tc.estimated); got != tc.want {
				t.Errorf("shouldPrune(%d) = %v, want %v (gate at %d)",
					tc.estimated, got, tc.want, pruneAt(tc.window, tc.maxTokens))
			}
		})
	}
}

// TestCallerSuppliedTriggerCarriesThePruneGate: when the caller hands its own
// trigger down (engine.BudgetedCompactor), the pre-pass gate is taken off *that*
// number, so the two keep their order whatever threshold the caller is running.
func TestCallerSuppliedTriggerCarriesThePruneGate(t *testing.T) {
	const window = 100_000
	s := New(Options{
		Adapter: &summaryAdapter{}, Model: "m",
		ContextWindow:       window,
		PreservePrefixCache: true,
	})
	// A trigger far below anything this Summarizer would compute for itself.
	b := budgetFrom(WithTokenBudget(context.Background(), 0, 0, 40_000))
	gate := 40_000 - tokenest.CompactionPruneLead(window)
	if s.shouldPrune(b, gate) {
		t.Errorf("pruned at %d, which is not yet past the caller's gate %d", gate, gate)
	}
	if !s.shouldPrune(b, gate+1) {
		t.Errorf("did not prune at %d, one past the caller's gate", gate+1)
	}
	// And the Summarizer's own rule must not be what answered: its gate is far
	// higher, so an estimate between the two proves which number was used.
	if own := tokenest.CompactionPruneTrigger(window, 0); own <= gate {
		t.Fatalf("fixture is not discriminating: the Summarizer's own gate %d is not above the caller's %d", own, gate)
	}
}
