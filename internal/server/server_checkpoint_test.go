package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/checkpoint"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/filetracker"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/session"
	"github.com/scottymacleod/aegis/internal/tool"
	"github.com/scottymacleod/aegis/internal/tool/builtin"
)

// scriptedAdapter issues a write_file tool call on its first turn, then a final
// text response, so a run produces a real file mutation the checkpoint can
// capture.
type scriptedAdapter struct {
	mu      sync.Mutex
	calls   int
	path    string
	content string
}

func (a *scriptedAdapter) Name() string { return "scripted" }

func (a *scriptedAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	a.mu.Lock()
	n := a.calls
	a.calls++
	a.mu.Unlock()

	ch := make(chan provider.Event, 4)
	if n == 0 {
		input := json.RawMessage(fmt.Sprintf(`{"path":%q,"content":%q}`, a.path, a.content))
		ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu1", Name: "write_file", Input: input}}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{}}
	} else {
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "done"}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	}
	close(ch)
	return ch, nil
}

func newCheckpointTestServer(t *testing.T, root string, adapter provider.Adapter) (*client.Client, func()) {
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
	ft := filetracker.New()
	if err := builtin.Register(reg, builtin.Options{Root: root, FileTracker: ft}); err != nil {
		t.Fatal(err)
	}
	cpStore, err := checkpoint.NewStore(store.DB())
	if err != nil {
		t.Fatal(err)
	}

	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, reg)
	srv.checkpoints = cpStore
	srv.fileTracker = ft

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL)
	return cl, func() { ts.Close(); store.Close() }
}

func TestCheckpointRewind(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "out.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	cl, cleanup := newCheckpointTestServer(t, root, &scriptedAdapter{path: "out.txt", content: "v2"})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	// Drive a turn that writes the file.
	ch, err := cl.PostMessage(ctx, meta.ID, "overwrite the file")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	for range ch { // drain to completion
	}

	if got, _ := os.ReadFile(target); string(got) != "v2" {
		t.Fatalf("after run, file = %q, want v2", got)
	}

	// A checkpoint should have captured the pre-turn file.
	cps, err := cl.ListCheckpoints(ctx, meta.ID)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 1 {
		t.Fatalf("got %d checkpoints, want 1", len(cps))
	}
	if cps[0].FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", cps[0].FileCount)
	}
	if cps[0].Seq != 0 {
		t.Errorf("Seq = %d, want 0", cps[0].Seq)
	}

	// Rewind code only: file reverts, conversation stays.
	resp, err := cl.Rewind(ctx, meta.ID, cps[0].ID, "code")
	if err != nil {
		t.Fatalf("Rewind code: %v", err)
	}
	if resp.FilesRestored != 1 {
		t.Errorf("FilesRestored = %d, want 1", resp.FilesRestored)
	}
	if got, _ := os.ReadFile(target); string(got) != "v1" {
		t.Errorf("after code rewind, file = %q, want v1", got)
	}
	sess, _ := cl.GetSession(ctx, meta.ID)
	if len(sess.Messages) == 0 {
		t.Error("code-only rewind should not truncate the conversation")
	}

	// Rewind conversation: messages truncate to the pre-turn count (0).
	resp, err = cl.Rewind(ctx, meta.ID, cps[0].ID, "conversation")
	if err != nil {
		t.Fatalf("Rewind conversation: %v", err)
	}
	if resp.MessagesKept != 0 {
		t.Errorf("MessagesKept = %d, want 0", resp.MessagesKept)
	}
	sess, _ = cl.GetSession(ctx, meta.ID)
	if len(sess.Messages) != 0 {
		t.Errorf("after conversation rewind, %d messages remain, want 0", len(sess.Messages))
	}
}

func TestRewindValidation(t *testing.T) {
	root := t.TempDir()
	cl, cleanup := newCheckpointTestServer(t, root, &scriptedAdapter{path: "x.txt", content: "x"})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	// Unknown checkpoint → error.
	if _, err := cl.Rewind(ctx, meta.ID, "no-such-checkpoint", "both"); err == nil {
		t.Error("expected error for unknown checkpoint")
	}
	// Empty checkpoint id → error.
	if _, err := cl.Rewind(ctx, meta.ID, "", "both"); err == nil {
		t.Error("expected error for empty checkpoint id")
	}
	// Invalid scope → error.
	// Create a checkpoint to target.
	ch, _ := cl.PostMessage(ctx, meta.ID, "do something")
	for range ch {
	}
	cps, _ := cl.ListCheckpoints(ctx, meta.ID)
	if len(cps) == 0 {
		t.Fatal("expected a checkpoint")
	}
	if _, err := cl.Rewind(ctx, meta.ID, cps[0].ID, "bogus"); err == nil {
		t.Error("expected error for invalid scope")
	}
}
