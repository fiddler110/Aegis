package compaction

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
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
		ContextWindow:       est * 5 / 4, // ~80% full: under the 25%-free gate
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

func TestShouldPruneGate(t *testing.T) {
	cases := []struct {
		name      string
		preserve  bool
		window    int
		maxBudget int
		estimated int
		want      bool
	}{
		{"off always prunes far from window", false, 100_000, 0, 1_000, true},
		{"off always prunes with no window", false, 0, 120_000, 1_000, true},
		{"on skips with headroom", true, 100_000, 0, 50_000, false},
		{"on prunes at 75% full", true, 100_000, 0, 75_001, true},
		{"on holds off just under 75%", true, 100_000, 0, 74_999, false},
		{"on skips on a large window with 40k free", true, 400_000, 0, 350_000, false},
		{"on prunes on a large window under 40k free", true, 400_000, 0, 365_000, true},
		// No window to measure against: keep the unconditional behaviour rather
		// than guess, since guessing wrong here costs an overflow.
		{"on prunes when the window is unknown", true, 0, 120_000, 1_000, true},
		{"on prunes when compaction is disabled entirely", true, 0, 0, 1_000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{
				Adapter: &summaryAdapter{}, Model: "m",
				ContextWindow:       tc.window,
				MaxBudget:           tc.maxBudget,
				PreservePrefixCache: tc.preserve,
			})
			if got := s.shouldPrune(tc.estimated); got != tc.want {
				t.Errorf("shouldPrune(%d) = %v, want %v", tc.estimated, got, tc.want)
			}
		})
	}
}
