package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// overflowErrorEvent is one mid-stream EventError carrying the P35.2
// context-truncation signature — the same shape IsContextOverflowError already
// recognizes and internal/drive already resets a whole conversation for.
func overflowErrorEvent() provider.Event {
	return provider.Event{Type: provider.EventError, Err: provider.NewContextTruncationError("test", "")}
}

// TestClipOverflowBatchClipsReadFileWithPointer: a read_file result over the
// clip budget keeps its head and a pointer back to the file it already read,
// per P74.16 — no new write, since offset/limit can get the rest.
func TestClipOverflowBatchClipsReadFileWithPointer(t *testing.T) {
	big := strings.Repeat("line of source\n", 500) // well over overflowClipKeepBytes
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleAssistant, Content: []provider.Block{
		provider.ToolUseBlock{ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{"path":"big.go"}`)},
	}})
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.ToolResultBlock{ToolUseID: "tu_1", Content: big},
	}})

	if !clipOverflowBatch(conv) {
		t.Fatal("clipOverflowBatch = false, want true (oversized read_file result present)")
	}
	tr := conv.Messages[1].Content[0].(provider.ToolResultBlock)
	if len(tr.Content) >= len(big) {
		t.Fatalf("clipped content len = %d, want less than original %d", len(tr.Content), len(big))
	}
	if !strings.Contains(tr.Content, "big.go") {
		t.Errorf("clipped read_file result = %q, want a pointer naming big.go", tr.Content)
	}
}

// TestClipOverflowBatchStubsNonReadResult: a non-read_file result over budget
// collapses to a stub rather than a guessed slice, since this package has no
// posture (head/tail) for a tool it did not build the result for.
func TestClipOverflowBatchStubsNonReadResult(t *testing.T) {
	big := strings.Repeat("build output line\n", 500)
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleAssistant, Content: []provider.Block{
		provider.ToolUseBlock{ID: "tu_1", Name: "shell", Input: json.RawMessage(`{"command":"go test ./..."}`)},
	}})
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.ToolResultBlock{ToolUseID: "tu_1", Content: big},
	}})

	if !clipOverflowBatch(conv) {
		t.Fatal("clipOverflowBatch = false, want true (oversized shell result present)")
	}
	tr := conv.Messages[1].Content[0].(provider.ToolResultBlock)
	if strings.Contains(tr.Content, "build output line") {
		t.Errorf("stub still carries original content: %q", tr.Content)
	}
	if !strings.Contains(tr.Content, "shell") {
		t.Errorf("stub = %q, want it to name the tool", tr.Content)
	}
}

// TestClipOverflowBatchNothingToClip: no trailing tool-result message, or every
// result already fits, must report false so the caller gives up rather than
// looping.
func TestClipOverflowBatchNothingToClip(t *testing.T) {
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "hello"},
	}})
	if clipOverflowBatch(conv) {
		t.Fatal("clipOverflowBatch = true on a conversation with no tool results, want false")
	}

	conv2 := &Conversation{}
	conv2.Append(provider.Message{Role: provider.RoleAssistant, Content: []provider.Block{
		provider.ToolUseBlock{ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{"path":"small.go"}`)},
	}})
	conv2.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.ToolResultBlock{ToolUseID: "tu_1", Content: "tiny"},
	}})
	if clipOverflowBatch(conv2) {
		t.Fatal("clipOverflowBatch = true when the only result already fits, want false")
	}
}

// TestEngineRetriesTurnAfterClippingOverflow: a context-overflow error on a
// turn whose preceding round left an oversized read_file result gets clipped
// and the turn retried in place, rather than failing the whole run (P74.16).
func TestEngineRetriesTurnAfterClippingOverflow(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{overflowErrorEvent()},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
	eng, err := New(Options{Adapter: adapter, Model: "test", MaxTokens: 100, ContextWindowTokens: 100_000})
	if err != nil {
		t.Fatal(err)
	}

	big := strings.Repeat("line of source\n", 500)
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "read the file"},
	}})
	conv.Append(provider.Message{Role: provider.RoleAssistant, Content: []provider.Block{
		provider.ToolUseBlock{ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{"path":"big.go"}`)},
	}})
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.ToolResultBlock{ToolUseID: "tu_1", Content: big},
	}})

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, n := range notices {
		if strings.Contains(n, "clipped") {
			found = true
		}
	}
	if !found {
		t.Errorf("notices = %v, want one naming the clip", notices)
	}
	tr := conv.Messages[2].Content[0].(provider.ToolResultBlock)
	if len(tr.Content) >= len(big) {
		t.Errorf("tool result not clipped in the retried conversation: len = %d", len(tr.Content))
	}
}

// TestEngineGivesUpWhenNothingToClip: an overflow error with nothing left to
// clip must still fail the run rather than spinning at maxOverflowClipRounds
// forever reporting the same unclippable state.
func TestEngineGivesUpWhenNothingToClip(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{overflowErrorEvent()},
	}}
	eng, err := New(Options{Adapter: adapter, Model: "test", MaxTokens: 100, ContextWindowTokens: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "hi"},
	}})

	err = eng.Run(context.Background(), conv, func(Event) {})
	if err == nil {
		t.Fatal("Run = nil error, want the overflow error to surface when there is nothing to clip")
	}
	if !provider.IsContextOverflowError(err) {
		t.Errorf("Run error = %v, want a context-overflow error", err)
	}
}
