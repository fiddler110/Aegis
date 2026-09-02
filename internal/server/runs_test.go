package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

func TestRunRegistryLifecycle(t *testing.T) {
	r := newRunRegistry()
	if len(r.list()) != 0 {
		t.Fatal("new registry should be empty")
	}

	r.start("run-1", "sess-1", "fix tests")
	r.start("run-2", "sess-2", "update docs")
	r.observe("run-1", api.KindToolCall)
	r.observe("run-1", api.KindToolCall)
	r.observe("run-1", api.KindText)

	runs := r.list()
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}

	var one api.RunInfo
	for _, rn := range runs {
		if rn.RunID == "run-1" {
			one = rn
		}
	}
	if one.SessionID != "sess-1" || one.Title != "fix tests" {
		t.Errorf("run-1 = %+v", one)
	}
	if one.Tools != 2 {
		t.Errorf("run-1 tools = %d, want 2", one.Tools)
	}
	if one.LastKind != string(api.KindText) {
		t.Errorf("run-1 lastKind = %q", one.LastKind)
	}

	r.finish("run-1")
	if got := len(r.list()); got != 1 {
		t.Fatalf("after finish, got %d runs, want 1", got)
	}
}

// blockingAdapter emits one text delta, signals that the run is active, then
// waits for release before finishing — so a test can observe an in-flight run.
type blockingAdapter struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingAdapter) Name() string { return "blocking" }
func (a *blockingAdapter) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event)
	go func() {
		defer close(ch)
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "working"}
		close(a.started)
		select {
		case <-a.release:
		case <-ctx.Done():
		}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	}()
	return ch, nil
}

func TestRunsEndpointReflectsActiveRun(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	adapter := &blockingAdapter{started: make(chan struct{}), release: make(chan struct{})}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		ch, err := cl.PostMessage(ctx, meta.ID, "go do work")
		if err != nil {
			return
		}
		for range ch {
		}
	}()

	<-adapter.started // the run is registered and mid-stream
	runs, err := cl.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].SessionID != meta.ID {
		t.Fatalf("ListRuns = %+v, want one run for %s", runs, meta.ID)
	}

	close(adapter.release)
	<-drained

	runs, _ = cl.ListRuns(ctx)
	if len(runs) != 0 {
		t.Errorf("after completion, ListRuns = %+v, want empty", runs)
	}
}

// TestRunRegistryStopSession covers the P28.5 cancel path in isolation: a
// run with no registered cancel (a plain, non-resumable run) can't be
// stopped this way, and stopping removes the ability to double-stop.
func TestRunRegistryStopSession(t *testing.T) {
	r := newRunRegistry()
	r.start("run-1", "sess-1", "t")

	if r.stopSession("sess-1") {
		t.Fatal("stopSession should report false before a cancel is registered")
	}

	var gotHard bool
	called := false
	r.setCancel("run-1", func(hard bool) { called = true; gotHard = hard })

	if !r.stopSession("sess-1") {
		t.Fatal("stopSession should report true once a cancel is registered")
	}
	if !called {
		t.Fatal("stopSession did not invoke the registered cancel func")
	}
	if gotHard {
		t.Error("stopSession should call the soft (hard=false) path, giving the engine a chance to let an in-flight call finish")
	}
	if r.stopSession("no-such-session") {
		t.Fatal("stopSession should report false for an unknown session")
	}
}

// TestResumableRunSurvivesClientDisconnect is the core P28.5 behavior: a run
// started with Resumable:true keeps executing after the client's HTTP
// request is torn down (simulating a dropped connection), unlike a plain
// run, and its events land in the buffered store so a reconnecting client
// can catch up via GetBGEvents.
func TestResumableRunSurvivesClientDisconnect(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	adapter := &blockingAdapter{started: make(chan struct{}), release: make(chan struct{})}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")

	bgCtx := context.Background()
	meta, err := cl.CreateSession(bgCtx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	// A separate, cancellable context for the request itself — cancelling
	// this (not bgCtx) is what simulates the client disconnecting, the same
	// way an aborted browser fetch tears down its own request context
	// without the daemon process going anywhere.
	reqCtx, cancelReq := context.WithCancel(bgCtx)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		ch, err := cl.PostMessageReq(reqCtx, meta.ID, api.PostMessageRequest{Text: "go do work", Resumable: true})
		if err != nil {
			return
		}
		for range ch {
		}
	}()

	<-adapter.started // the run is registered and mid-stream
	cancelReq()       // simulate a dropped connection
	<-drained         // the client-side stream read loop has exited

	// The run must still be active server-side — a resumable run's context
	// is deliberately not tied to the request context that just died.
	deadline := time.Now().Add(2 * time.Second)
	for {
		runs, err := cl.ListRuns(bgCtx)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && runs[0].SessionID == meta.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not survive client disconnect: ListRuns = %+v", runs)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(adapter.release) // let the still-running turn finish

	deadline = time.Now().Add(2 * time.Second)
	for {
		runs, err := cl.ListRuns(bgCtx)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never finished after release: ListRuns = %+v", runs)
		}
		time.Sleep(10 * time.Millisecond)
	}

	events, err := cl.GetBGEvents(bgCtx, meta.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected the disconnected-but-still-running turn's events to be buffered")
	}
	sawDone := false
	for _, e := range events {
		if strings.Contains(e.Data, `"kind":"done"`) {
			sawDone = true
		}
	}
	if !sawDone {
		t.Errorf("buffered events did not include a done event: %+v", events)
	}
}

// TestStopRunCancelsResumableRun verifies POST /sessions/{id}/stop actually
// interrupts a resumable run's execution (not just removes it from
// /runs) — needed because with a resumable run's context decoupled from its
// HTTP request (P28.5), the client disconnecting no longer does this itself.
func TestStopRunCancelsResumableRun(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	adapter := &blockingAdapter{started: make(chan struct{}), release: make(chan struct{})}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatal(err)
	}

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		ch, err := cl.PostMessageReq(ctx, meta.ID, api.PostMessageRequest{Text: "go do work", Resumable: true})
		if err != nil {
			return
		}
		for range ch {
		}
	}()

	<-adapter.started
	if err := cl.StopRun(ctx, meta.ID); err != nil {
		t.Fatalf("StopRun: %v", err)
	}
	<-drained // the stream closes once the cancelled run finishes up

	runs, err := cl.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Errorf("run still active after StopRun: %+v", runs)
	}

	// Stopping a session with no active resumable run 404s rather than
	// silently no-oping, so a caller can tell "nothing to stop" apart from
	// a real failure.
	if err := cl.StopRun(ctx, meta.ID); err == nil {
		t.Error("StopRun on an already-finished run should error")
	}
}

func TestRunRegistryConcurrent(t *testing.T) {
	r := newRunRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%26))
			r.start("run-"+id, "sess", "t")
			r.observe("run-"+id, api.KindToolCall)
			_ = r.list()
			r.finish("run-" + id)
		}(i)
	}
	wg.Wait()
	// All started runs were finished; the exact count depends on key collisions
	// but the registry must not have panicked and must be internally consistent.
	for _, rn := range r.list() {
		if rn.SessionID == "" {
			t.Error("inconsistent run state")
		}
	}
}
