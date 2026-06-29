package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/session"
	"github.com/scottymacleod/aegis/internal/tool"
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
