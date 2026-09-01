package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// approve reads the sent event's ApprovalID back off pendingApprovals and
// answers it — the shape every real client (TUI, ACP, /side) uses.
func approve(t *testing.T, pending *sync.Map, id string, d approvalDecision) {
	t.Helper()
	val, ok := pending.Load(id)
	if !ok {
		t.Fatalf("no pending approval for id %q", id)
	}
	ch := val.(chan approvalDecision)
	select {
	case ch <- d:
	default:
		t.Fatalf("approval channel for %q was not ready", id)
	}
}

// TestSSEApproverPatternRule verifies the TQ6 allow-always split: a decision
// carrying a pattern installs a scoped rule via persistRule and does NOT fall
// back to the coarse per-tool session cache, while a pattern-less allow-always
// still uses the cache.
func TestSSEApproverPatternRule(t *testing.T) {
	var pending sync.Map
	var gotTool, gotPattern string
	sent := make(chan string, 1)
	a := &sseApprover{
		send:             func(ev api.Event) { sent <- ev.ApprovalID },
		runID:            "run-1",
		sessionID:        "sess-1",
		permCache:        &sync.Map{},
		pendingApprovals: &pending,
		persistRule: func(tool, pattern string) {
			gotTool, gotPattern = tool, pattern
		},
	}

	go func() {
		id := <-sent
		approve(t, &pending, id, approvalDecision{Approved: true, AllowAlways: true, Pattern: "npm test*"})
	}()
	if !a.Approve(context.Background(), "shell", "needs approval", json.RawMessage(`{}`)) {
		t.Fatal("expected approval to be granted")
	}
	if gotTool != "shell" || gotPattern != "npm test*" {
		t.Fatalf("expected persistRule(shell, npm test*), got (%q, %q)", gotTool, gotPattern)
	}
	if _, ok := a.permCache.Load("sess-1\x00shell"); ok {
		t.Fatal("pattern-scoped approval must not populate the whole-tool cache")
	}

	// Without a pattern, allow-always falls back to the per-tool cache and the
	// next call is auto-approved without prompting.
	go func() {
		id := <-sent
		approve(t, &pending, id, approvalDecision{Approved: true, AllowAlways: true})
	}()
	if !a.Approve(context.Background(), "shell", "needs approval", json.RawMessage(`{}`)) {
		t.Fatal("expected approval to be granted")
	}
	if _, ok := a.permCache.Load("sess-1\x00shell"); !ok {
		t.Fatal("expected pattern-less allow-always to populate the per-tool cache")
	}
	if !a.Approve(context.Background(), "shell", "needs approval", nil) {
		t.Fatal("expected cached auto-approval")
	}
}

// TestSSEApproverBatchesConcurrentCalls verifies P81.33/FIND-33: two calls
// waiting on Approve() at once each get their own id/channel, and the second
// event's ApprovalBatch lists both — the "reviewable summary" a batch-aware
// client renders instead of two serial prompts that silently clobber each
// other.
func TestSSEApproverBatchesConcurrentCalls(t *testing.T) {
	var pending sync.Map
	var mu sync.Mutex
	var events []api.Event
	sent := make(chan string, 2)
	a := &sseApprover{
		send: func(ev api.Event) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
			sent <- ev.ApprovalID
		},
		runID:            "run-1",
		sessionID:        "sess-1",
		permCache:        &sync.Map{},
		pendingApprovals: &pending,
	}

	results := make(chan bool, 2)
	go func() { results <- a.Approve(context.Background(), "shell", "call A", json.RawMessage(`{}`)) }()
	idA := <-sent
	go func() { results <- a.Approve(context.Background(), "write_file", "call B", json.RawMessage(`{}`)) }()
	idB := <-sent

	if idA == idB {
		t.Fatalf("expected distinct ids for concurrent calls, got %q twice", idA)
	}

	mu.Lock()
	lastEvent := events[len(events)-1]
	mu.Unlock()
	if len(lastEvent.ApprovalBatch) != 2 {
		t.Fatalf("expected the second event's batch to list both pending calls, got %d", len(lastEvent.ApprovalBatch))
	}

	approve(t, &pending, idA, approvalDecision{Approved: true})
	approve(t, &pending, idB, approvalDecision{Approved: false})

	got := map[bool]int{}
	got[<-results]++
	got[<-results]++
	if got[true] != 1 || got[false] != 1 {
		t.Fatalf("expected one approval and one denial answered independently, got %v", got)
	}
}
