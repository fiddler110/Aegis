package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/scottymacleod/agentharness/internal/provider"
	"github.com/scottymacleod/agentharness/internal/tool"
)

// barrierTool signals on each Execute start and blocks until released, letting a
// test prove that concurrent invocations overlap.
type barrierTool struct {
	started chan struct{}
	release chan struct{}
}

func (b *barrierTool) Name() string                 { return "barrier" }
func (b *barrierTool) Description() string          { return "blocks until released" }
func (b *barrierTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (b *barrierTool) Capability() tool.Capability  { return tool.CapRead }
func (b *barrierTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	b.started <- struct{}{}
	<-b.release
	return tool.Result{Content: "done"}, nil
}

func TestParallelToolsOverlapAndPreserveOrder(t *testing.T) {
	const n = 3
	bt := &barrierTool{started: make(chan struct{}, n), release: make(chan struct{})}

	// Turn 1 requests the barrier tool n times; turn 2 is the final answer.
	var turn1 []provider.Event
	for i := range n {
		turn1 = append(turn1, provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: fmt.Sprintf("tu_%d", i), Name: "barrier", Input: json.RawMessage(`{}`),
		}})
	}
	turn1 = append(turn1, provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse})
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		turn1,
		{{Type: provider.EventTextDelta, Text: "ok"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(bt); err != nil {
		t.Fatal(err)
	}
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test"})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	done := make(chan error, 1)
	go func() { done <- eng.Run(context.Background(), conv, nil) }()

	// All n calls must start before any is released — proves real overlap.
	for i := range n {
		select {
		case <-bt.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d tool calls started; not running in parallel", i, n)
		}
	}
	close(bt.release)

	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	// conv: user, assistant(tool calls), user(tool results), assistant(final).
	results := conv.Messages[2].Content
	if len(results) != n {
		t.Fatalf("got %d result blocks, want %d", len(results), n)
	}
	for i, blk := range results {
		tr, ok := blk.(provider.ToolResultBlock)
		if !ok {
			t.Fatalf("block %d is %T, want ToolResultBlock", i, blk)
		}
		if want := fmt.Sprintf("tu_%d", i); tr.ToolUseID != want {
			t.Errorf("result %d has id %q, want %q (order not preserved)", i, tr.ToolUseID, want)
		}
	}
}
