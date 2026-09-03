package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestMidStreamErrorPreservesStreamedText is the ARCH-09 regression: a
// mid-stream EventError used to discard the whole assistant turn, including
// text already streamed to the user via KindText events — so the transcript
// silently lost content the user watched arrive. The turn must instead
// persist what was actually streamed before failing.
func TestMidStreamErrorPreservesStreamedText(t *testing.T) {
	streamErr := errors.New("boom")
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "partial answer before the crash"},
			{Type: provider.EventError, Err: streamErr},
		},
	}}
	eng, err := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	runErr := eng.Run(context.Background(), conv, func(Event) {})
	if runErr == nil {
		t.Fatal("Run: want error, got nil")
	}

	if len(conv.Messages) != 2 {
		t.Fatalf("conv.Messages: want 2 (user + partial assistant), got %d: %+v", len(conv.Messages), conv.Messages)
	}
	last := conv.Messages[len(conv.Messages)-1]
	if last.Role != provider.RoleAssistant {
		t.Fatalf("last message role = %v, want assistant", last.Role)
	}
	if len(last.Content) != 1 {
		t.Fatalf("last message content: want 1 block, got %d", len(last.Content))
	}
	tb, ok := last.Content[0].(provider.TextBlock)
	if !ok {
		t.Fatalf("last message content[0] = %T, want TextBlock", last.Content[0])
	}
	if tb.Text != "partial answer before the crash" {
		t.Errorf("preserved text = %q, want the streamed text", tb.Text)
	}
}

// TestMidStreamErrorWithNoTextAppendsNothing: an error with nothing streamed
// yet (or only thinking, no text) must not add a stray empty assistant
// message to the transcript.
func TestMidStreamErrorWithNoTextAppendsNothing(t *testing.T) {
	streamErr := errors.New("boom")
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{{Type: provider.EventError, Err: streamErr}},
	}}
	eng, err := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	if err := eng.Run(context.Background(), conv, func(Event) {}); err == nil {
		t.Fatal("Run: want error, got nil")
	}
	if len(conv.Messages) != 1 {
		t.Fatalf("conv.Messages: want 1 (user only), got %d: %+v", len(conv.Messages), conv.Messages)
	}
}
