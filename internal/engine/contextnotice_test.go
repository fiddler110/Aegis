package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// noticeCompactor declines the run-start compaction pass, then compacts on
// the proactive per-turn check — so tests exercise the mid-run notice path.
type noticeCompactor struct{ calls int }

func (c *noticeCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	c.calls++
	if c.calls == 1 {
		return msgs, false, nil
	}
	return msgs[len(msgs)-1:], true, nil
}

func bigConversation() *Conversation {
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: strings.Repeat("filler words here ", 40)}, // ~180 estimated tokens
	}})
	return conv
}

// TestProactiveCompactionEmitsNotice: crossing the 85% threshold with a
// working compactor surfaces a KindNotice so the user sees the compaction
// instead of it being a silent log line.
func TestProactiveCompactionEmitsNotice(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "ok"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
	comp := &noticeCompactor{}
	eng, err := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Compactor: comp, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Errorf("notices = %v, want one compaction notice", notices)
	}
}

// failingFallbackCompactor always fails Compact (simulating a local-model
// summarizer that reliably returns empty output — the P28.4 live-eval
// failure) but implements FallbackCompact, so the engine can fall back to a
// deterministic pass after two consecutive failures.
type failingFallbackCompactor struct {
	compactCalls  int
	fallbackCalls int
}

func (c *failingFallbackCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	c.compactCalls++
	return msgs, false, errors.New("summarizer returned empty output")
}

func (c *failingFallbackCompactor) FallbackCompact(msgs []provider.Message) ([]provider.Message, bool) {
	c.fallbackCalls++
	return msgs[len(msgs)-1:], true
}

// TestProactiveCompactionFallsBackAfterTwoFailures is the P28.4 regression:
// a Compactor whose LLM summarizer fails twice in a row must not leave the
// engine skipping compaction indefinitely — the engine falls back to the
// Compactor's deterministic FallbackCompact and the run keeps making
// progress on context size.
func TestProactiveCompactionFallsBackAfterTwoFailures(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_2", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &failingFallbackCompactor{}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Compactor: comp, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if comp.compactCalls < 2 {
		t.Fatalf("expected Compact to be attempted at least twice, got %d", comp.compactCalls)
	}
	if comp.fallbackCalls != 1 {
		t.Fatalf("expected FallbackCompact to be called exactly once (after the 2nd consecutive failure), got %d", comp.fallbackCalls)
	}
	found := false
	for _, n := range notices {
		if strings.Contains(n, "fallback") {
			found = true
		}
	}
	if !found {
		t.Errorf("notices = %v, want one mentioning the fallback compaction", notices)
	}
}

// TestContextFullNoticeWithoutCompactor: with no compactor and the estimate
// at/over 95% of the window, the engine warns the user exactly once per run —
// this is the silent-truncation case on local model servers.
func TestContextFullNoticeWithoutCompactor(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Two iterations both exceed the threshold, but the warning fires once.
	if len(notices) != 1 || !strings.Contains(notices[0], "context") {
		t.Errorf("notices = %v, want exactly one context-full warning", notices)
	}
}
