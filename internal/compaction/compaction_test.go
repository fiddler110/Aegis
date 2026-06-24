package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/scottymacleod/agentharness/internal/provider"
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
