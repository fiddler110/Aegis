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
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
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
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL).WithToken("test-token")
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

// turnBlockingAdapter lets a test control exactly when a scripted turn's model
// call returns, to deterministically test races against an in-flight run.
// Calls before blockOnCall proceed immediately; the blockOnCall'th call closes
// entered (signaling the caller it has started) and waits on release.
type turnBlockingAdapter struct {
	mu          sync.Mutex
	calls       int
	blockOnCall int
	entered     chan struct{}
	release     chan struct{}
}

func (a *turnBlockingAdapter) Name() string { return "blocking" }

func (a *turnBlockingAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	a.mu.Lock()
	n := a.calls
	a.calls++
	a.mu.Unlock()

	if n == a.blockOnCall {
		close(a.entered)
		<-a.release
	}

	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: fmt.Sprintf("turn%d", n)}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	close(ch)
	return ch, nil
}

// TestRewindWaitsForInFlightRun is the regression for the rewind/in-flight-turn
// race: handleRewind must acquire the same per-session semaphore
// handlePostMessage does, so a rewind can never truncate the message table
// while a turn is still running and about to append its own tail with a
// Persisted offset captured before the truncation (which would silently
// revive content the user just rewound away).
func TestRewindWaitsForInFlightRun(t *testing.T) {
	root := t.TempDir()
	adapter := &turnBlockingAdapter{blockOnCall: 1, entered: make(chan struct{}), release: make(chan struct{})}
	cl, cleanup := newCheckpointTestServer(t, root, adapter)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1 (call 0) completes instantly and gives us a checkpoint to rewind to.
	ch, err := cl.PostMessage(ctx, meta.ID, "first")
	if err != nil {
		t.Fatalf("PostMessage 1: %v", err)
	}
	for range ch {
	}
	cps, err := cl.ListCheckpoints(ctx, meta.ID)
	if err != nil || len(cps) == 0 {
		t.Fatalf("ListCheckpoints: %v (n=%d)", err, len(cps))
	}

	// Turn 2 (call 1) blocks inside the model call, simulating an in-flight run.
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		ch2, err := cl.PostMessage(ctx, meta.ID, "second")
		if err != nil {
			t.Errorf("PostMessage 2: %v", err)
			return
		}
		for range ch2 {
		}
	}()

	select {
	case <-adapter.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("turn 2 never entered the blocking model call")
	}

	// Rewind, issued while turn 2 holds the session semaphore, must block
	// rather than truncating messages out from under the in-flight run.
	rewindDone := make(chan struct{})
	go func() {
		defer close(rewindDone)
		if _, err := cl.Rewind(ctx, meta.ID, cps[0].ID, "conversation"); err != nil {
			t.Errorf("Rewind: %v", err)
		}
	}()

	select {
	case <-rewindDone:
		t.Fatal("rewind returned while a run was still in flight")
	case <-time.After(150 * time.Millisecond):
	}

	close(adapter.release)

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight run never finished after release")
	}
	select {
	case <-rewindDone:
	case <-time.After(2 * time.Second):
		t.Fatal("rewind never completed after the run finished")
	}

	// Turn 2 must have finished (and persisted its messages) strictly before
	// the rewind truncated them: the session should end up with exactly the
	// checkpoint's pre-turn-1 message count, not turn 2's tail spliced back in.
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 0 {
		t.Errorf("after rewind, %d messages remain, want 0 (turn 2's tail must not survive)", len(sess.Messages))
	}
}

// incrementalPersistAdapter issues one write_file tool call on its first turn,
// then blocks before streaming its second (final) turn — so a test can
// observe session state while one tool round has completed but the overall
// run has not finished.
type incrementalPersistAdapter struct {
	mu      sync.Mutex
	calls   int
	entered chan struct{}
	release chan struct{}
	path    string
	content string
}

func (a *incrementalPersistAdapter) Name() string { return "incremental" }

func (a *incrementalPersistAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	a.mu.Lock()
	n := a.calls
	a.calls++
	a.mu.Unlock()

	if n == 0 {
		input := json.RawMessage(fmt.Sprintf(`{"path":%q,"content":%q}`, a.path, a.content))
		ch := make(chan provider.Event, 2)
		ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu1", Name: "write_file", Input: input}}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{}}
		close(ch)
		return ch, nil
	}

	close(a.entered)
	<-a.release
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: "done"}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	close(ch)
	return ch, nil
}

// TestMessagesPersistIncrementallyDuringRun is the regression for "transcript
// persistence is not actually incremental despite the field name": messages
// must be saved after each completed tool round, not only once eng.Run fully
// returns. Otherwise a crash mid-run loses the whole turn's transcript even
// though the tool's side effects (here, a file write) already happened on
// disk — history desyncs from real repo state with no record of what ran.
func TestMessagesPersistIncrementallyDuringRun(t *testing.T) {
	root := t.TempDir()
	adapter := &incrementalPersistAdapter{entered: make(chan struct{}), release: make(chan struct{}), path: "out.txt", content: "v2"}
	cl, cleanup := newCheckpointTestServer(t, root, adapter)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		ch, err := cl.PostMessage(ctx, meta.ID, "write the file")
		if err != nil {
			t.Errorf("PostMessage: %v", err)
			return
		}
		for range ch {
		}
	}()

	select {
	case <-adapter.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("second model call never started")
	}

	// The run is still blocked in its second model call — nothing has been
	// returned to the client yet — but the first tool round (user prompt,
	// assistant tool_use, tool_result) must already be durably saved.
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) < 3 {
		t.Errorf("mid-run message count = %d, want >= 3 (tool round should already be flushed)", len(sess.Messages))
	}

	close(adapter.release)
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("run never finished after release")
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

// TestSessionFork is the P22.3 counterpart to TestCheckpointRewind: forking
// must create a genuinely separate session truncated to the checkpoint, while
// leaving the source session's own messages completely untouched (the whole
// point of a fork over a rewind).
func TestSessionFork(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "out.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	cl, cleanup := newCheckpointTestServer(t, root, &scriptedAdapter{path: "out.txt", content: "v2"})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Title: "original"})
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1: writes the file (produces a checkpoint at Seq 0).
	ch, err := cl.PostMessage(ctx, meta.ID, "overwrite the file")
	if err != nil {
		t.Fatalf("PostMessage 1: %v", err)
	}
	for range ch {
	}

	// Turn 2: a second, plain turn (produces a second checkpoint).
	ch2, err := cl.PostMessage(ctx, meta.ID, "second message")
	if err != nil {
		t.Fatalf("PostMessage 2: %v", err)
	}
	for range ch2 {
	}

	origBefore, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(origBefore.Messages) == 0 {
		t.Fatal("expected messages on original session")
	}

	cps, err := cl.ListCheckpoints(ctx, meta.ID)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 2 {
		t.Fatalf("got %d checkpoints, want 2", len(cps))
	}
	// cps is newest-first; the oldest (last element) is Seq 0, the very start.
	oldest := cps[len(cps)-1]
	if oldest.Seq != 0 {
		t.Fatalf("oldest checkpoint Seq = %d, want 0", oldest.Seq)
	}

	// Fork at the oldest checkpoint: the new session should be truncated to
	// that checkpoint's Seq, just like /rewind's "conversation" scope would
	// truncate the *same* session — except here it lands in a new one.
	forkResp, err := cl.Fork(ctx, meta.ID, oldest.ID)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if forkResp.SessionID == meta.ID {
		t.Fatal("fork must create a separate session id")
	}
	if forkResp.MessagesKept != oldest.Seq {
		t.Errorf("MessagesKept = %d, want %d", forkResp.MessagesKept, oldest.Seq)
	}
	if forkResp.Title != "Fork of original" {
		t.Errorf("Title = %q, want %q", forkResp.Title, "Fork of original")
	}

	forkedSess, err := cl.GetSession(ctx, forkResp.SessionID)
	if err != nil {
		t.Fatalf("GetSession(forked): %v", err)
	}
	if len(forkedSess.Messages) != oldest.Seq {
		t.Errorf("forked session has %d messages, want %d", len(forkedSess.Messages), oldest.Seq)
	}
	if forkedSess.Mode != origBefore.Mode {
		t.Errorf("forked mode = %q, want %q", forkedSess.Mode, origBefore.Mode)
	}

	// The source session must be completely untouched by the fork.
	origAfter, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(origAfter.Messages) != len(origBefore.Messages) {
		t.Errorf("original session message count changed: %d -> %d", len(origBefore.Messages), len(origAfter.Messages))
	}

	// Fork with no checkpoint id: carries the full current conversation as-is.
	forkAllResp, err := cl.Fork(ctx, meta.ID, "")
	if err != nil {
		t.Fatalf("Fork (no checkpoint): %v", err)
	}
	if forkAllResp.MessagesKept != len(origBefore.Messages) {
		t.Errorf("no-checkpoint fork MessagesKept = %d, want %d", forkAllResp.MessagesKept, len(origBefore.Messages))
	}
	forkAllSess, err := cl.GetSession(ctx, forkAllResp.SessionID)
	if err != nil {
		t.Fatalf("GetSession(forkAll): %v", err)
	}
	if len(forkAllSess.Messages) != len(origBefore.Messages) {
		t.Errorf("no-checkpoint fork has %d messages, want %d", len(forkAllSess.Messages), len(origBefore.Messages))
	}
}

// TestSessionForkValidation mirrors TestRewindValidation's error-path
// coverage for the new /fork endpoint.
func TestSessionForkValidation(t *testing.T) {
	root := t.TempDir()
	cl, cleanup := newCheckpointTestServer(t, root, &scriptedAdapter{path: "x.txt", content: "x"})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	// Unknown checkpoint → error.
	if _, err := cl.Fork(ctx, meta.ID, "no-such-checkpoint"); err == nil {
		t.Error("expected error for unknown checkpoint")
	}

	// Unknown session id → error.
	if _, err := cl.Fork(ctx, "no-such-session", ""); err == nil {
		t.Error("expected error for unknown session")
	}

	// A checkpoint belonging to a different session → error.
	other, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := cl.PostMessage(ctx, other.ID, "do something")
	for range ch {
	}
	otherCps, _ := cl.ListCheckpoints(ctx, other.ID)
	if len(otherCps) == 0 {
		t.Fatal("expected a checkpoint on the other session")
	}
	if _, err := cl.Fork(ctx, meta.ID, otherCps[0].ID); err == nil {
		t.Error("expected error forking with another session's checkpoint")
	}
}
