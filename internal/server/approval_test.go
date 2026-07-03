package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// TestSSEApproverPatternRule verifies the TQ6 allow-always split: a decision
// carrying a pattern installs a scoped rule via persistRule and does NOT fall
// back to the coarse per-tool session cache, while a pattern-less allow-always
// still uses the cache.
func TestSSEApproverPatternRule(t *testing.T) {
	var cache sync.Map
	ch := make(chan approvalDecision, 1)
	var gotTool, gotPattern string
	a := &sseApprover{
		send:      func(api.Event) {},
		ch:        ch,
		runID:     "run-1",
		sessionID: "sess-1",
		permCache: &cache,
		persistRule: func(tool, pattern string) {
			gotTool, gotPattern = tool, pattern
		},
	}

	ch <- approvalDecision{Approved: true, AllowAlways: true, Pattern: "npm test*"}
	if !a.Approve(context.Background(), "shell", "needs approval", json.RawMessage(`{}`)) {
		t.Fatal("expected approval to be granted")
	}
	if gotTool != "shell" || gotPattern != "npm test*" {
		t.Fatalf("expected persistRule(shell, npm test*), got (%q, %q)", gotTool, gotPattern)
	}
	if _, ok := cache.Load("sess-1\x00shell"); ok {
		t.Fatal("pattern-scoped approval must not populate the whole-tool cache")
	}

	// Without a pattern, allow-always falls back to the per-tool cache and the
	// next call is auto-approved without prompting.
	ch <- approvalDecision{Approved: true, AllowAlways: true}
	if !a.Approve(context.Background(), "shell", "needs approval", json.RawMessage(`{}`)) {
		t.Fatal("expected approval to be granted")
	}
	if _, ok := cache.Load("sess-1\x00shell"); !ok {
		t.Fatal("expected pattern-less allow-always to populate the per-tool cache")
	}
	if !a.Approve(context.Background(), "shell", "needs approval", nil) {
		t.Fatal("expected cached auto-approval")
	}
}
