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

// TestToolEntry_ApprovalRequestMarksAwaitingApproval verifies the sidebar
// activity entry for a pending call is flagged "awaiting_approval" when a
// KindApprovalRequest names it, and that the eventual KindToolResult still
// resolves it to ok/err — the same last-matching-pending-by-name correlation
// KindToolResult already uses, now widened to also match the transient
// awaiting_approval sub-state (there is no tool_use ID on ApprovalItem to
// correlate against more precisely).
func TestToolEntry_ApprovalRequestMarksAwaitingApproval(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("write a file", nil)
	m.streaming = true

	toolInput, _ := json.Marshal(map[string]string{"path": "out.txt"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "write_file", ToolID: "tu_1", ToolInput: toolInput})
	if len(m.tools) != 1 || m.tools[0].status != "pending" {
		t.Fatalf("expected one pending tool entry, got %+v", m.tools)
	}
	if m.tools[0].startedAt.IsZero() {
		t.Fatal("expected startedAt to be set when the entry is created")
	}

	m.applyEvent(api.Event{Kind: api.KindApprovalRequest, Tool: "write_file", ApprovalID: "ap_1", ToolInput: toolInput})
	if m.tools[0].status != "awaiting_approval" {
		t.Fatalf("expected status awaiting_approval after the approval request, got %q", m.tools[0].status)
	}

	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "write_file", ToolID: "tu_1", ToolResult: "wrote 3 bytes"})
	if m.tools[0].status != "ok" {
		t.Fatalf("expected the result to resolve the awaiting_approval entry to ok, got %q", m.tools[0].status)
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

	if len(m.toolState.pendingTools) != 2 {
		t.Fatalf("expected two independently-tracked pending cards, got %d", len(m.toolState.pendingTools))
	}
	cardA, okA := m.toolState.pendingTools["tu_a"]
	cardB, okB := m.toolState.pendingTools["tu_b"]
	if !okA || !okB {
		t.Fatalf("expected both tool IDs to have their own card, got tu_a=%v tu_b=%v", okA, okB)
	}
	if cardA.blk == cardB.blk {
		t.Fatal("expected concurrent calls to own distinct transcript items, got a shared one")
	}

	// Resolve out of call order: B's result arrives first.
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_b", ToolResult: "package b\n"})
	if _, stillPending := m.toolState.pendingTools["tu_b"]; stillPending {
		t.Fatal("expected tu_b to be resolved and removed from pendingTools")
	}
	if _, stillPending := m.toolState.pendingTools["tu_a"]; !stillPending {
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
	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected no pending cards left after both results arrived, got %d", len(m.toolState.pendingTools))
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
	if len(m.toolState.pendingTools) != 1 {
		t.Fatalf("expected one pending card before the error, got %d", len(m.toolState.pendingTools))
	}
	beforeLen := m.transcript.Len()

	m.applyEvent(api.Event{Kind: api.KindError, Error: "engine: aborting suspected loop"})

	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected the stuck card to be resolved and cleared after a turn error, got %d still pending", len(m.toolState.pendingTools))
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
	if len(m.toolState.pendingTools) != 1 {
		t.Fatalf("expected one pending card before cancellation, got %d", len(m.toolState.pendingTools))
	}

	m = driveUpdate(t, m, streamClosedMsg{})

	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected streamClosedMsg to resolve the stuck card, got %d still pending", len(m.toolState.pendingTools))
	}
	got := plainView(m)
	if strings.Contains(got, "running") {
		t.Fatalf("expected the running indicator gone after streamClosedMsg, got:\n%s", got)
	}
	if !strings.Contains(got, "interrupted") {
		t.Fatalf("expected the stuck card to render an interrupted/terminal state, got:\n%s", got)
	}
}

// TestToolCard_StartShowsCardWhileArgumentsStream is the core P33.3
// regression guard: the card must be on screen from KindToolCallStart —
// while the model is still generating the call's arguments, frequently the
// longest phase of a turn — rather than only for the moment between the
// stream ending and the tool running.
func TestToolCard_StartShowsCardWhileArgumentsStream(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read a file", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file", ToolID: "tu_1"})
	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "read_file") {
		t.Fatalf("expected the tool name on screen as soon as the model named it, got:\n%s", got)
	}
	if !strings.Contains(got, "preparing") {
		t.Fatalf("expected the provisional card to show it is still being prepared, got:\n%s", got)
	}
	if len(m.toolState.pendingTools) != 1 {
		t.Fatalf("expected the start event to track one pending card, got %d", len(m.toolState.pendingTools))
	}
	// The arguments aren't known yet, so nothing may claim the call ran or
	// enter the tool-history strip — KindToolCall still owns both.
	if strings.Contains(got, "running") {
		t.Errorf("expected no running indicator before the call itself arrived, got:\n%s", got)
	}
	if len(m.tools) != 0 {
		t.Errorf("expected the start event not to add a tool-history entry, got %v", m.tools)
	}
}

// TestToolCard_StartReconcilesInPlaceWithoutDuplicating covers the handoff:
// the KindToolCall that follows a KindToolCallStart must fill the arguments
// into the card already on screen, not append a second one for the same call.
func TestToolCard_StartReconcilesInPlaceWithoutDuplicating(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read a file", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file", ToolID: "tu_1"})
	afterStart := m.transcript.Len()
	card := m.toolState.pendingTools["tu_1"]
	if card == nil {
		t.Fatal("expected the start event to register a card under its tool_use ID")
	}

	toolInput, _ := json.Marshal(map[string]string{"path": "main.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: toolInput})
	if got := m.transcript.Len(); got != afterStart {
		t.Fatalf("expected the call to reuse the started card (same item count); after start=%d after call=%d", afterStart, got)
	}
	if len(m.toolState.pendingTools) != 1 || m.toolState.pendingTools["tu_1"] != card {
		t.Fatalf("expected the same card still tracked under tu_1, got %v", m.toolState.pendingTools)
	}
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "preparing") {
		t.Fatalf("expected the provisional state replaced once the arguments arrived, got:\n%s", got)
	}
	if !strings.Contains(got, "main.go") {
		t.Fatalf("expected the call's arguments folded into the started card, got:\n%s", got)
	}
	if strings.Count(got, "read_file") != 1 {
		t.Fatalf("expected exactly one card for the call, got:\n%s", got)
	}

	// The result must still resolve the same card in place.
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_1", ToolResult: "package main\n"})
	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected the reconciled card resolved by its result, got %d still pending", len(m.toolState.pendingTools))
	}
	if got := m.transcript.Len(); got != afterStart {
		t.Fatalf("expected the result to update the same item in place; want %d items, got %d", afterStart, got)
	}
	m.refresh()
	if final := plainView(m); !strings.Contains(final, "package main") {
		t.Fatalf("expected the result rendered into the started card, got:\n%s", final)
	}
}

// TestToolCard_StartWithLateToolIDReconcilesAndRekeys covers the OpenAI wire
// shape where a tool call is named in an earlier delta than the one carrying
// its ID: the start event has no ID to key the card by, so reconciliation
// falls back to the oldest provisional card of the same name and re-keys it
// to the real ID — otherwise the KindToolResult, which looks the card up by
// that ID, would leave it stuck and append its own orphan block.
func TestToolCard_StartWithLateToolIDReconcilesAndRekeys(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read a file", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file"})
	afterStart := m.transcript.Len()

	toolInput, _ := json.Marshal(map[string]string{"path": "main.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_late", ToolInput: toolInput})
	if got := m.transcript.Len(); got != afterStart {
		t.Fatalf("expected the ID-less started card reused; after start=%d after call=%d", afterStart, got)
	}
	if _, ok := m.toolState.pendingTools["tu_late"]; !ok {
		t.Fatalf("expected the card re-keyed to the late-arriving tool_use ID, got %v", m.toolState.pendingTools)
	}

	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_late", ToolResult: "package main\n"})
	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected the re-keyed card resolved by its result, got %d still pending", len(m.toolState.pendingTools))
	}
	if got := m.transcript.Len(); got != afterStart {
		t.Fatalf("expected no orphan block appended for the result; want %d items, got %d", afterStart, got)
	}
	m.refresh()
	if got := plainView(m); !strings.Contains(got, "package main") || strings.Contains(got, "preparing") {
		t.Fatalf("expected the result folded into the started card, got:\n%s", got)
	}
}

// TestToolCard_ConcurrentStartsTrackedSeparately guards the dedupe rule: a
// repeated tool_use ID must not stack a second card, but two ID-less starts
// are two distinct calls (an adapter that names a call before it IDs it) and
// must each get their own — deduping those by name would silently lose one.
func TestToolCard_ConcurrentStartsTrackedSeparately(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read two files", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file", ToolID: "tu_a"})
	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file", ToolID: "tu_a"})
	if len(m.toolState.pendingTools) != 1 {
		t.Fatalf("expected a repeated tool_use ID to be ignored, got %d cards", len(m.toolState.pendingTools))
	}

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file"})
	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file"})
	if len(m.toolState.pendingTools) != 3 {
		t.Fatalf("expected two ID-less starts to own separate cards, got %d total", len(m.toolState.pendingTools))
	}
}

// TestToolCard_StartResolvedAsStuckOnCancel covers the orphan case unique to
// P33.3: a run cancelled while the model is still writing a call's arguments
// never produces the KindToolCall, let alone a result. The existing
// resolveStuckToolCards safety nets must cover cards the start event created
// too, rather than leaving one shimmering "preparing…" forever.
func TestToolCard_StartResolvedAsStuckOnCancel(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("cancel me mid tool call", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "shell", ToolID: "tu_cancelled"})
	if len(m.toolState.pendingTools) != 1 {
		t.Fatalf("expected one provisional card before cancellation, got %d", len(m.toolState.pendingTools))
	}

	m = driveUpdate(t, m, streamClosedMsg{})

	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected streamClosedMsg to resolve the provisional card, got %d still pending", len(m.toolState.pendingTools))
	}
	got := plainView(m)
	if strings.Contains(got, "preparing") {
		t.Fatalf("expected the preparing indicator gone after the stream closed, got:\n%s", got)
	}
	if !strings.Contains(got, "interrupted") || !strings.Contains(got, "shell") {
		t.Fatalf("expected the provisional card to render an interrupted state naming the tool, got:\n%s", got)
	}
}

// TestToolCard_StartResolvedAsStuckOnError is the KindError half of the
// safety net above: a turn that fails while a call's arguments are streaming
// must resolve the provisional card in place, appending only the error banner.
func TestToolCard_StartResolvedAsStuckOnError(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("fail mid tool call", nil)
	m.streaming = true
	m.followBottom = true

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file", ToolID: "tu_err"})
	beforeLen := m.transcript.Len()

	m.applyEvent(api.Event{Kind: api.KindError, Error: "engine: context canceled"})

	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected the provisional card resolved after the error, got %d still pending", len(m.toolState.pendingTools))
	}
	if got := m.transcript.Len(); got != beforeLen+1 {
		t.Fatalf("expected the card resolved in place (only the error banner appended); before=%d after=%d", beforeLen, got)
	}
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "preparing") {
		t.Fatalf("expected the preparing indicator gone once the card was resolved as stuck, got:\n%s", got)
	}
	if !strings.Contains(got, "interrupted") {
		t.Fatalf("expected the provisional card to render an interrupted state, got:\n%s", got)
	}
}

// TestToolCard_NoStartEventStillRendersOneCard is the additive-only guard: a
// producer that never emits KindToolCallStart (an older daemon, an adapter
// whose provider doesn't announce tool calls early, a hand-built event) must
// behave exactly as it did before P33.3.
func TestToolCard_NoStartEventStillRendersOneCard(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read a file", nil)
	m.streaming = true
	m.followBottom = true

	toolInput, _ := json.Marshal(map[string]string{"path": "main.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: toolInput})
	if len(m.toolState.pendingTools) != 1 {
		t.Fatalf("expected one card for an unannounced call, got %d", len(m.toolState.pendingTools))
	}
	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "running") || strings.Contains(got, "preparing") {
		t.Fatalf("expected the pre-P33.3 pending rendering, got:\n%s", got)
	}
	if strings.Count(got, "read_file") != 1 {
		t.Fatalf("expected exactly one card, got:\n%s", got)
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
	if len(m.toolState.pendingTools) != 0 {
		t.Fatalf("expected the ID-less card resolved via FIFO fallback, got %d still pending", len(m.toolState.pendingTools))
	}
	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "package main") {
		t.Fatalf("expected the result rendered via the FIFO fallback match, got:\n%s", got)
	}
}
