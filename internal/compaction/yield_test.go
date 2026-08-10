package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// yieldFixture is a conversation whose prefix holds one superseded
// read of the same path — the deterministic pre-pass will blank the first
// result and nothing else, which is the small-yield shape P62.7 is about.
func yieldFixture(payload string) []provider.Message {
	return []provider.Message{
		toolUse("t1", "read_file", json.RawMessage(`{"path":"a.go"}`)),
		toolResult("t1", payload),
		text(provider.RoleAssistant, "thinking about it"),
		text(provider.RoleUser, "carry on"),
		toolUse("t2", "read_file", json.RawMessage(`{"path":"a.go"}`)),
		toolResult("t2", payload),
		text(provider.RoleAssistant, "done"),
	}
}

// TestCompactYieldReportsPruneYield: the pre-pass's yield reaches the caller as
// a number rather than as a bool. This is the (c) half of P62.7 — the engine
// cannot apply a minimum-yield rule to `changed`.
func TestCompactYieldReportsPruneYield(t *testing.T) {
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 1_000_000, KeepRecent: 4})
	msgs := yieldFixture(strings.Repeat("package main // a line of file content\n", 40))

	before := s.estimate("", msgs)
	out, changed, summarized, freed, err := s.CompactYield(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("CompactYield: %v", err)
	}
	if !changed {
		t.Fatal("expected the pre-pass to prune the superseded read")
	}
	if summarized {
		t.Error("summarized = true, but the budget is nowhere near reached")
	}
	if a.called != 0 {
		t.Errorf("summarizer called %d times on a prune-only compaction", a.called)
	}
	if want := before - s.estimate("", out); freed != want {
		t.Errorf("freedTokens = %d, want %d (the estimate actually dropped by that much)", freed, want)
	}
	if freed <= 0 {
		t.Errorf("freedTokens = %d, want a positive yield for a prune that changed the conversation", freed)
	}
}

// TestCompactYieldReportsNothingWhenNothingPruned: no change, no yield. A zero
// here and a zero from a low-yield prune are the same to the caller only
// because both are equally not worth a rewrite.
func TestCompactYieldReportsNothingWhenNothingPruned(t *testing.T) {
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 1_000_000, KeepRecent: 4})
	msgs := []provider.Message{
		text(provider.RoleUser, "hello there"),
		text(provider.RoleAssistant, "hi"),
	}
	_, changed, summarized, freed, err := s.CompactYield(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("CompactYield: %v", err)
	}
	if changed || summarized || freed != 0 {
		t.Errorf("changed=%v summarized=%v freed=%d, want false/false/0", changed, summarized, freed)
	}
}

// TestCompactYieldMarksSummarization: a compaction that reached the LLM says so,
// and reports a yield of a wholly different order — which is what lets the
// engine leave it alone.
func TestCompactYieldMarksSummarization(t *testing.T) {
	a := &summaryAdapter{summary: "earlier we set up the project"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})
	msgs := yieldFixture(strings.Repeat("package main // a line of file content\n", 40))

	before := s.estimate("", msgs)
	out, changed, summarized, freed, err := s.CompactYield(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("CompactYield: %v", err)
	}
	if !changed || !summarized {
		t.Fatalf("changed=%v summarized=%v, want both true", changed, summarized)
	}
	if want := before - s.estimate("", out); freed != want {
		t.Errorf("freedTokens = %d, want %d", freed, want)
	}
	if freed < 100 {
		t.Errorf("freedTokens = %d, want a summarization-sized yield", freed)
	}
}

// TestCompactMatchesCompactYield: the two entry points are one implementation.
// Compact is the wider contract (engine.Compactor) and must keep behaving
// exactly as it did before the seam was widened.
func TestCompactMatchesCompactYield(t *testing.T) {
	payload := strings.Repeat("package main // a line of file content\n", 40)
	for _, tc := range []struct {
		name   string
		budget int
	}{
		{"prune only", 1_000_000},
		{"summarized", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain := New(Options{Adapter: &summaryAdapter{summary: "s"}, Model: "m", MaxBudget: tc.budget, KeepRecent: 2})
			rich := New(Options{Adapter: &summaryAdapter{summary: "s"}, Model: "m", MaxBudget: tc.budget, KeepRecent: 2})

			outA, changedA, err := plain.Compact(context.Background(), "", yieldFixture(payload))
			if err != nil {
				t.Fatalf("Compact: %v", err)
			}
			outB, changedB, _, _, err := rich.CompactYield(context.Background(), "", yieldFixture(payload))
			if err != nil {
				t.Fatalf("CompactYield: %v", err)
			}
			if changedA != changedB || len(outA) != len(outB) {
				t.Fatalf("Compact -> (%v, %d msgs), CompactYield -> (%v, %d msgs)", changedA, len(outA), changedB, len(outB))
			}
			if plain.estimate("", outA) != rich.estimate("", outB) {
				t.Errorf("the two entry points produced differently-sized conversations")
			}
		})
	}
}
