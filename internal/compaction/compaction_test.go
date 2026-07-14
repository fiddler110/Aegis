package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

type summaryAdapter struct {
	summary string
	called  int
}

func (a *summaryAdapter) Name() string { return "summary" }
func (a *summaryAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.called++
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: a.summary}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

func text(role provider.Role, s string) provider.Message {
	return provider.Message{Role: role, Content: []provider.Block{provider.TextBlock{Text: s}}}
}

func TestNoCompactionUnderBudget(t *testing.T) {
	a := &summaryAdapter{summary: "x"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 1000000, KeepRecent: 2})
	msgs := []provider.Message{text(provider.RoleUser, "hi"), text(provider.RoleAssistant, "hello")}
	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil || changed {
		t.Fatalf("expected no compaction, changed=%v err=%v", changed, err)
	}
	if len(out) != 2 || a.called != 0 {
		t.Errorf("conversation altered unexpectedly (len=%d called=%d)", len(out), a.called)
	}
}

func TestCompactionSummarizesPrefix(t *testing.T) {
	a := &summaryAdapter{summary: "earlier we set up the project"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "msg one is fairly long here"),
		text(provider.RoleAssistant, "reply one is also long"),
		text(provider.RoleUser, "msg two continues"),
		text(provider.RoleAssistant, "reply two continues"),
		text(provider.RoleUser, "msg three"),
		text(provider.RoleAssistant, "final reply kept"),
	}
	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !changed {
		t.Fatal("expected compaction to occur")
	}
	if a.called != 1 {
		t.Errorf("summarizer called %d times, want 1", a.called)
	}
	first, ok := out[0].Content[0].(provider.TextBlock)
	if !ok || !strings.Contains(first.Text, "earlier we set up the project") {
		t.Errorf("first message should hold the summary, got %+v", out[0])
	}
	// The final assistant message must be preserved verbatim.
	last, ok := out[len(out)-1].Content[0].(provider.TextBlock)
	if !ok || last.Text != "final reply kept" {
		t.Errorf("recent message not preserved: %+v", out[len(out)-1])
	}
	if len(out) >= len(msgs) {
		t.Errorf("compaction did not shrink conversation: %d -> %d", len(msgs), len(out))
	}
}

// TestForceCompactIgnoresBudget is the P19.2 regression: a manual /compact
// must summarize a conversation that is nowhere near the configured budget,
// which Compact would leave untouched.
func TestForceCompactIgnoresBudget(t *testing.T) {
	a := &summaryAdapter{summary: "earlier we set up the project"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 1000000, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "msg one"),
		text(provider.RoleAssistant, "reply one"),
		text(provider.RoleUser, "msg two"),
		text(provider.RoleAssistant, "reply two"),
		text(provider.RoleUser, "msg three"),
		text(provider.RoleAssistant, "final reply kept"),
	}

	// Compact leaves it alone: nowhere near the (huge) budget.
	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil || changed {
		t.Fatalf("Compact: expected no-op under budget, changed=%v err=%v", changed, err)
	}

	// ForceCompact summarizes it anyway.
	out, changed, err = s.ForceCompact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("ForceCompact: %v", err)
	}
	if !changed {
		t.Fatal("ForceCompact: expected compaction to occur despite being under budget")
	}
	if a.called != 1 {
		t.Errorf("summarizer called %d times, want 1", a.called)
	}
	if len(out) >= len(msgs) {
		t.Errorf("ForceCompact did not shrink conversation: %d -> %d", len(msgs), len(out))
	}
}

// TestForceCompactTooShortIsNoop covers a conversation with no safe boundary
// to cut at (fewer messages than KeepRecent) — ForceCompact must report no
// change rather than fabricating a summary out of almost nothing.
func TestForceCompactTooShortIsNoop(t *testing.T) {
	a := &summaryAdapter{summary: "summary"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 1000000, KeepRecent: 8})
	msgs := []provider.Message{text(provider.RoleUser, "hi"), text(provider.RoleAssistant, "hello")}

	out, changed, err := s.ForceCompact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("ForceCompact: %v", err)
	}
	if changed {
		t.Error("expected no-op for a conversation shorter than KeepRecent")
	}
	if len(out) != len(msgs) || a.called != 0 {
		t.Errorf("conversation altered unexpectedly (len=%d called=%d)", len(out), a.called)
	}
}

// TestFallbackCompactShrinksWithoutLLM is the P28.4 regression: when the LLM
// summarizer is unusable, FallbackCompact must still shrink the conversation
// deterministically — no adapter call, no chance of empty output — while
// preserving the same keepRecent-tail/tool-pairing invariants as Compact.
func TestFallbackCompactShrinksWithoutLLM(t *testing.T) {
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "msg one is fairly long here"),
		text(provider.RoleAssistant, "reply one is also long"),
		text(provider.RoleUser, "msg two continues"),
		text(provider.RoleAssistant, "reply two continues"),
		text(provider.RoleUser, "msg three"),
		text(provider.RoleAssistant, "final reply kept"),
	}
	out, changed := s.FallbackCompact(msgs)
	if !changed {
		t.Fatal("expected fallback compaction to occur")
	}
	if a.called != 0 {
		t.Errorf("FallbackCompact must never call the adapter, called=%d", a.called)
	}
	if len(out) >= len(msgs) {
		t.Errorf("fallback compaction did not shrink conversation: %d -> %d", len(msgs), len(out))
	}
	first, ok := out[0].Content[0].(provider.TextBlock)
	if !ok || first.Text == "" {
		t.Fatalf("first message should hold a non-empty deterministic note, got %+v", out[0])
	}
	if !strings.Contains(first.Text, "deterministic fallback") {
		t.Errorf("note should identify itself as a fallback, got %q", first.Text)
	}
	// The final assistant message must still be preserved verbatim.
	last, ok := out[len(out)-1].Content[0].(provider.TextBlock)
	if !ok || last.Text != "final reply kept" {
		t.Errorf("recent message not preserved: %+v", out[len(out)-1])
	}
}

// TestFallbackCompactPreservesToolPair mirrors
// TestCompactionPreservesToolPair for the deterministic fallback path.
func TestFallbackCompactPreservesToolPair(t *testing.T) {
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "do something big and long here to exceed"),
		text(provider.RoleAssistant, "ok working on it now in detail"),
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "t1", Name: "grep", Input: json.RawMessage(`{"pattern":"x"}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "t1", Content: "match found here"},
		}},
	}
	out, changed := s.FallbackCompact(msgs)
	if !changed {
		t.Fatal("expected fallback compaction to occur")
	}
	if out[1].Role != provider.RoleAssistant {
		t.Fatalf("kept suffix should start with assistant, got %s", out[1].Role)
	}
	if _, ok := out[1].Content[0].(provider.ToolUseBlock); !ok {
		t.Errorf("expected tool_use to be preserved, got %T", out[1].Content[0])
	}
	if _, ok := out[2].Content[0].(provider.ToolResultBlock); !ok {
		t.Errorf("expected tool_result to follow tool_use, got %T", out[2].Content[0])
	}
}

// TestFallbackCompactTooShortIsNoop mirrors TestForceCompactTooShortIsNoop:
// nothing safe to cut must report changed=false, not a fabricated note.
func TestFallbackCompactTooShortIsNoop(t *testing.T) {
	a := &summaryAdapter{summary: "unused"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 1000000, KeepRecent: 8})
	msgs := []provider.Message{text(provider.RoleUser, "hi"), text(provider.RoleAssistant, "hello")}

	out, changed := s.FallbackCompact(msgs)
	if changed {
		t.Error("expected no-op for a conversation shorter than KeepRecent")
	}
	if len(out) != len(msgs) || a.called != 0 {
		t.Errorf("conversation altered unexpectedly (len=%d called=%d)", len(out), a.called)
	}
}

func TestCompactionPreservesToolPair(t *testing.T) {
	a := &summaryAdapter{summary: "summary"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "do something big and long here to exceed"),
		text(provider.RoleAssistant, "ok working on it now in detail"),
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "t1", Name: "grep", Input: json.RawMessage(`{"pattern":"x"}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "t1", Content: "match found here"},
		}},
	}
	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil || !changed {
		t.Fatalf("expected compaction, changed=%v err=%v", changed, err)
	}
	// The kept suffix must begin with the assistant tool_use, keeping the pair
	// (tool_use, tool_result) intact and adjacent.
	if out[1].Role != provider.RoleAssistant {
		t.Fatalf("kept suffix should start with assistant, got %s", out[1].Role)
	}
	if _, ok := out[1].Content[0].(provider.ToolUseBlock); !ok {
		t.Errorf("expected tool_use to be preserved, got %T", out[1].Content[0])
	}
	if _, ok := out[2].Content[0].(provider.ToolResultBlock); !ok {
		t.Errorf("expected tool_result to follow tool_use, got %T", out[2].Content[0])
	}
}
