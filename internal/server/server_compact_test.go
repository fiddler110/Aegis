package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// compactStubAdapter answers ordinary conversation turns with a short reply,
// and answers the compaction summarizer's distinct system prompt with a
// canned summary — so the same adapter can drive both engine turns and the
// Summarizer's own auxiliary model call.
type compactStubAdapter struct {
	mu    sync.Mutex
	calls int
}

func (a *compactStubAdapter) Name() string { return "compact-stub" }

func (a *compactStubAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 2)
	if strings.Contains(req.System, "You compress conversation history") {
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "summary of earlier turns"}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
		close(ch)
		return ch, nil
	}

	a.mu.Lock()
	n := a.calls
	a.calls++
	a.mu.Unlock()

	ch <- provider.Event{Type: provider.EventTextDelta, Text: fmt.Sprintf("reply %d", n)}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	close(ch)
	return ch, nil
}

func newCompactTestServer(t *testing.T, root string, adapter provider.Adapter) (*client.Client, func()) {
	t.Helper()
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: root}); err != nil {
		t.Fatal(err)
	}

	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, reg)
	if adapter != nil {
		srv.compactor = compaction.New(compaction.Options{Adapter: adapter, Model: "test"})
	}
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL).WithToken("test-token")
	return cl, func() { ts.Close(); store.Close() }
}

// TestHandleCompactSession exercises the manual /compact command end to end
// (P19.2): a long-enough conversation must actually shrink, and the session's
// stored messages must reflect the compacted result afterward.
func TestHandleCompactSession(t *testing.T) {
	root := t.TempDir()
	cl, cleanup := newCompactTestServer(t, root, &compactStubAdapter{})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	// Default KeepRecent is 8; drive enough turns that a boundary exists
	// before the kept-recent tail.
	for i := 0; i < 6; i++ {
		ch, err := cl.PostMessage(ctx, meta.ID, fmt.Sprintf("turn %d", i))
		if err != nil {
			t.Fatalf("PostMessage %d: %v", i, err)
		}
		for range ch {
		}
	}

	before, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Messages) < 9 {
		t.Fatalf("setup: got %d messages, want >= 9 to exercise a real compaction boundary", len(before.Messages))
	}

	resp, err := cl.Compact(ctx, meta.ID)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !resp.Compacted {
		t.Fatal("Compacted = false, want true for a long conversation")
	}
	if resp.MessagesBefore != len(before.Messages) {
		t.Errorf("MessagesBefore = %d, want %d", resp.MessagesBefore, len(before.Messages))
	}
	if resp.MessagesAfter >= resp.MessagesBefore {
		t.Errorf("MessagesAfter = %d, want < MessagesBefore (%d)", resp.MessagesAfter, resp.MessagesBefore)
	}

	after, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Messages) != resp.MessagesAfter {
		t.Errorf("stored message count = %d, want %d (response should reflect what was persisted)", len(after.Messages), resp.MessagesAfter)
	}
}

// TestHandleCompactSessionTooShort covers a conversation with nothing safe to
// compact: ForceCompact must report Compacted=false rather than fabricating a
// summary turn out of almost nothing.
func TestHandleCompactSessionTooShort(t *testing.T) {
	root := t.TempDir()
	cl, cleanup := newCompactTestServer(t, root, &compactStubAdapter{})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cl.PostMessage(ctx, meta.ID, "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	for range ch {
	}

	resp, err := cl.Compact(ctx, meta.ID)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if resp.Compacted {
		t.Error("Compacted = true, want false for a too-short conversation")
	}
}

// TestHandleCompactSessionNoCompactor covers a server started without a model
// adapter (s.compactor stays nil): the handler must fail cleanly rather than
// panicking on a nil-interface type assertion.
func TestHandleCompactSessionNoCompactor(t *testing.T) {
	root := t.TempDir()
	cl, cleanup := newCompactTestServer(t, root, nil)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = cl.Compact(ctx, meta.ID)
	if err == nil {
		t.Fatal("expected error when no compactor is configured")
	}
	if !strings.Contains(err.Error(), "compaction not available") {
		t.Errorf("error = %v, want to mention compaction not available", err)
	}
}
