package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// TestToolCard_PendingUpdatesInPlaceToOK is the core P21.2 regression guard:
// a tool call must render as a single addressable transcript item that is
// mutated from pending to ok in place — not two independently-appended
// static blocks (the pre-P21.2 behavior) — once its matching KindToolResult
// (correlated by ToolID) arrives.
func TestToolCard_PendingUpdatesInPlaceToOK(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read a file", nil)
	m.streaming = true
	m.followBottom = true

	toolInput, _ := json.Marshal(map[string]string{"path": "main.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: toolInput})
	beforeLen := m.transcript.Len()
	m.refresh()
	pending := plainView(m)
	if !strings.Contains(pending, "running") {
		t.Fatalf("expected pending card to show a running indicator, got:\n%s", pending)
	}
	if !strings.Contains(pending, "read_file") {
		t.Fatalf("expected pending card to show the tool name, got:\n%s", pending)
	}

	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_1", ToolResult: "package main\n"})
	afterLen := m.transcript.Len()
	if afterLen != beforeLen {
		t.Fatalf("expected the result to update the existing card in place (same item count); before=%d after=%d", beforeLen, afterLen)
	}
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "running") {
		t.Fatalf("expected the running indicator gone once the result arrived, got:\n%s", got)
	}
	if !strings.Contains(got, "package main") {
		t.Fatalf("expected the result content folded into the same card, got:\n%s", got)
	}
	if !strings.Contains(got, "read_file") {
		t.Fatalf("expected the call's tool name to still be present after the update, got:\n%s", got)
	}
}

// TestToolCard_PendingUpdatesInPlaceToErr mirrors the OK case for a failed
// tool call: the card must flip to its error rendering in place, with no
// extra transcript item appended.
func TestToolCard_PendingUpdatesInPlaceToErr(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("run a broken command", nil)
	m.streaming = true
	m.followBottom = true

	toolInput, _ := json.Marshal(map[string]string{"command": "false"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "shell", ToolID: "tu_err", ToolInput: toolInput})
	beforeLen := m.transcript.Len()

	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "shell", ToolID: "tu_err", ToolResult: "command exited 1", ToolIsError: true})
	afterLen := m.transcript.Len()
	if afterLen != beforeLen {
		t.Fatalf("expected the error result to update the existing card in place; before=%d after=%d", beforeLen, afterLen)
	}
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "running") {
		t.Fatalf("expected the running indicator gone once the error result arrived, got:\n%s", got)
	}
	if !strings.Contains(got, "command exited 1") {
		t.Fatalf("expected the error content folded into the same card, got:\n%s", got)
	}
}

// TestToolCard_ConcurrentCallsUpdateIndependently is the P21.2 concurrency
// regression guard: the engine can run several tool calls in the same round
// concurrently (runTools), so their results don't necessarily arrive in
// call order. Two in-flight cards, keyed by distinct ToolIDs, must each
// resolve to their own result with no cross-talk, regardless of resolution
// order.
func TestToolCard_ConcurrentCallsUpdateIndependently(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read two files", nil)
	m.streaming = true
	m.followBottom = true

	inputA, _ := json.Marshal(map[string]string{"path": "a.go"})
	inputB, _ := json.Marshal(map[string]string{"path": "b.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_a", ToolInput: inputA})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_b", ToolInput: inputB})

	if len(m.pendingTools) != 2 {
		t.Fatalf("expected two independently-tracked pending cards, got %d", len(m.pendingTools))
	}
	cardA, okA := m.pendingTools["tu_a"]
	cardB, okB := m.pendingTools["tu_b"]
	if !okA || !okB {
		t.Fatalf("expected both tool IDs to have their own card, got tu_a=%v tu_b=%v", okA, okB)
	}
	if cardA.blk == cardB.blk {
		t.Fatal("expected concurrent calls to own distinct transcript items, got a shared one")
	}

	// Resolve out of call order: B's result arrives first.
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_b", ToolResult: "package b\n"})
	if _, stillPending := m.pendingTools["tu_b"]; stillPending {
		t.Fatal("expected tu_b to be resolved and removed from pendingTools")
	}
	if _, stillPending := m.pendingTools["tu_a"]; !stillPending {
		t.Fatal("expected tu_a to remain pending; resolving tu_b must not affect it")
	}
	m.refresh()
	afterB := plainView(m)
	if !strings.Contains(afterB, "package b") {
		t.Fatalf("expected tu_b's own result rendered, got:\n%s", afterB)
	}
	if !strings.Contains(afterB, "running") {
		t.Fatalf("expected tu_a's card still showing pending while tu_b resolved, got:\n%s", afterB)
	}

	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_a", ToolResult: "package a\n"})
	if len(m.pendingTools) != 0 {
		t.Fatalf("expected no pending cards left after both results arrived, got %d", len(m.pendingTools))
	}
	m.refresh()
	final := plainView(m)
	if !strings.Contains(final, "package a") || !strings.Contains(final, "package b") {
		t.Fatalf("expected both results present with no cross-talk, got:\n%s", final)
	}
	if strings.Contains(final, "running") {
		t.Fatalf("expected no pending indicator left once both cards resolved, got:\n%s", final)
	}
}

// TestToolCard_TurnErrorResolvesStuckPendingCard covers the case where a
// turn errors out (a hard engine error, budget/loop abort, etc.) before a
// still-in-flight tool call's KindToolResult ever arrives: KindError must
// resolve the card to a terminal state instead of leaving it stuck showing
// "running" forever.
func TestToolCard_TurnErrorResolvesStuckPendingCard(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("do something that fails mid-round", nil)
	m.streaming = true
	m.followBottom = true

	toolInput, _ := json.Marshal(map[string]string{"path": "main.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_stuck", ToolInput: toolInput})
	if len(m.pendingTools) != 1 {
		t.Fatalf("expected one pending card before the error, got %d", len(m.pendingTools))
	}
	beforeLen := m.transcript.Len()

	m.applyEvent(api.Event{Kind: api.KindError, Error: "engine: aborting suspected loop"})

	if len(m.pendingTools) != 0 {
		t.Fatalf("expected the stuck card to be resolved and cleared after a turn error, got %d still pending", len(m.pendingTools))
	}
	// KindError itself appends one new item (the "error: ..." banner); the
	// stuck card must be resolved in place, not via a second appended item,
	// so the count should grow by exactly that one banner item.
	if got := m.transcript.Len(); got != beforeLen+1 {
		t.Fatalf("expected the stuck card resolved in place (only the error banner appended); before=%d after=%d", beforeLen, got)
	}
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "running") {
		t.Fatalf("expected the running indicator gone once the card was resolved as stuck, got:\n%s", got)
	}
	if !strings.Contains(got, "interrupted") {
		t.Fatalf("expected the stuck card to render an interrupted/terminal state, got:\n%s", got)
	}
}

// TestToolCard_StreamClosedResolvesStuckPendingCard covers the
// client-initiated-cancel path: engine.ErrInterrupted's callers return
// without emitting KindError or KindDone at all, so streamClosedMsg is the
// only signal guaranteed to fire after every kind of run end. It must
// resolve any card KindError didn't get a chance to.
func TestToolCard_StreamClosedResolvesStuckPendingCard(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("cancel me mid tool call", nil)
	m.streaming = true
	m.followBottom = true

	toolInput, _ := json.Marshal(map[string]string{"command": "sleep 10"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "shell", ToolID: "tu_cancelled", ToolInput: toolInput})
	if len(m.pendingTools) != 1 {
		t.Fatalf("expected one pending card before cancellation, got %d", len(m.pendingTools))
	}

	m = driveUpdate(t, m, streamClosedMsg{})

	if len(m.pendingTools) != 0 {
		t.Fatalf("expected streamClosedMsg to resolve the stuck card, got %d still pending", len(m.pendingTools))
	}
	got := plainView(m)
	if strings.Contains(got, "running") {
		t.Fatalf("expected the running indicator gone after streamClosedMsg, got:\n%s", got)
	}
	if !strings.Contains(got, "interrupted") {
		t.Fatalf("expected the stuck card to render an interrupted/terminal state, got:\n%s", got)
	}
}

// TestToolCard_NoToolIDFallsBackToFIFO covers events with no ToolID (older
// producers, or hand-built events like the ones integration_test.go already
// uses) — resolution must fall back to the pre-P21.2 same-name FIFO match
// instead of leaving the card stuck or panicking on a nil lookup.
func TestToolCard_NoToolIDFallsBackToFIFO(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("no id", nil)
	m.streaming = true
	m.followBottom = true

	toolInput, _ := json.Marshal(map[string]string{"path": "main.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolInput: toolInput})
	beforeLen := m.transcript.Len()
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolResult: "package main\n"})
	if got := m.transcript.Len(); got != beforeLen {
		t.Fatalf("expected the ID-less result to still resolve the card in place; before=%d after=%d", beforeLen, got)
	}
	if len(m.pendingTools) != 0 {
		t.Fatalf("expected the ID-less card resolved via FIFO fallback, got %d still pending", len(m.pendingTools))
	}
	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "package main") {
		t.Fatalf("expected the result rendered via the FIFO fallback match, got:\n%s", got)
	}
}
