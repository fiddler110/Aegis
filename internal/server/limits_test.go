package server

import (
	"bytes"
	"context"
	"fmt"
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

// gatedAdapter emits one text delta immediately (so the run visibly starts
// and the SSE response flushes right away instead of waiting for the
// heartbeat), then blocks until release before finishing the turn. started
// closes the moment Stream is entered so a test can observe "the run has
// actually begun occupying a global run slot" without a race.
type gatedAdapter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGatedAdapter() *gatedAdapter {
	return &gatedAdapter{started: make(chan struct{}), release: make(chan struct{})}
}

func (a *gatedAdapter) Name() string { return "gated" }

func (a *gatedAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.once.Do(func() { close(a.started) })
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: "start "}
	go func() {
		defer close(ch)
		select {
		case <-a.release:
		case <-ctx.Done():
			return
		}
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "done"}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 1, OutputTokens: 1}}
	}()
	return ch, nil
}

// TestMaxConcurrentRunsRejectsOverflow is the P21.5 regression: with
// server.max_concurrent_runs set, a run on one session that is actively
// occupying the daemon's only global run slot causes a concurrent request on
// a *different* session to be rejected immediately (429) rather than queued
// or run anyway — sessionSems alone only serializes runs within a single
// session and does nothing to bound total daemon-wide concurrency.
func TestMaxConcurrentRunsRejectsOverflow(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Server:     config.ServerConfig{MaxConcurrentRuns: 1},
	}
	adapter := newGatedAdapter()
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta1, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	meta2, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	// Occupy the daemon's only global run slot with a run on session 1 that
	// blocks mid-flight until the test releases it.
	ch1, err := cl.PostMessage(ctx, meta1.ID, "go")
	if err != nil {
		t.Fatalf("PostMessage session1: %v", err)
	}
	select {
	case <-adapter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("run on session1 never reached the model call")
	}

	// A concurrent run on a *different* session must be rejected immediately.
	_, err = cl.PostMessage(ctx, meta2.ID, "go")
	if err == nil {
		t.Fatal("expected PostMessage on session2 to fail while the global run cap is exhausted")
	}
	if !strings.Contains(err.Error(), "max concurrent runs") {
		t.Errorf("error = %v, want mention of max concurrent runs", err)
	}

	// Release session1's run and confirm it completes normally — the cap
	// only refuses the overflow, it doesn't wedge the run that holds the slot.
	close(adapter.release)
	var sawText bool
	for ev := range ch1 {
		if ev.Kind == api.KindText {
			sawText = true
		}
		if ev.Kind == api.KindError {
			t.Fatalf("session1 run errored: %s", ev.Error)
		}
	}
	if !sawText {
		t.Error("session1 run never produced text output")
	}

	// Now that the slot is free, session2 must succeed.
	ch2, err := cl.PostMessage(ctx, meta2.ID, "go")
	if err != nil {
		t.Fatalf("PostMessage session2 after slot freed: %v", err)
	}
	for ev := range ch2 {
		if ev.Kind == api.KindError {
			t.Fatalf("session2 run errored: %s", ev.Error)
		}
	}
}

// hangingAdapter simulates a model call that never returns on its own,
// mirroring a stuck local model or a hostile/broken backend — it only stops
// when the run's context is cancelled.
type hangingAdapter struct{}

func (hangingAdapter) Name() string { return "hanging" }

func (hangingAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestMaxRunDurationAbortsLongRun is the P21.5 regression for the optional
// wall-clock ceiling: a run whose model call never returns on its own is
// still aborted once server.max_run_duration_sec elapses, instead of holding
// its session/global run slot open indefinitely.
func TestMaxRunDurationAbortsLongRun(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Server:     config.ServerConfig{MaxRunDurationSec: 1},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, hangingAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cl.PostMessage(ctx, meta.ID, "go")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	start := time.Now()
	timeout := time.After(10 * time.Second)
drain:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break drain
			}
		case <-timeout:
			t.Fatal("run did not abort within max_run_duration_sec; event stream never closed")
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("run took %v to abort, want close to the configured 1s ceiling", elapsed)
	}
}

// noopFlusher satisfies http.Flusher without doing anything, for tests that
// drive sseWriter directly against a plain io.Writer.
type noopFlusher struct{}

func (noopFlusher) Flush() {}

// blockingWriter is an io.Writer whose first Write blocks until release is
// closed, closing started the moment that first Write begins — used to hold
// sseWriter's background goroutine paused mid-flush so a test can force its
// queue to fill deterministically.
type blockingWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{started: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// TestSSEWriterDropsOldestOnOverflow is the P21.5 regression for bounding SSE
// event-queue memory growth: once the queue is full because the consumer
// (here, a Write call held open by blockingWriter) isn't draining it, further
// sends must drop the oldest queued event instead of growing the queue
// without bound.
func TestSSEWriterDropsOldestOnOverflow(t *testing.T) {
	bw := newBlockingWriter()
	var writeMu sync.Mutex
	var dropped int
	const bufSize = 3
	sw := newSSEWriter(bw, noopFlusher{}, &writeMu, bufSize, func() { dropped++ })

	// This first event gets dequeued by the writer goroutine immediately and
	// blocks inside Write, leaving the queue itself empty and ready to fill.
	sw.send(api.Event{Kind: api.KindText, Text: "0"})
	select {
	case <-bw.started:
	case <-time.After(5 * time.Second):
		t.Fatal("writer goroutine never reached the blocking write")
	}

	for i := 1; i <= bufSize+7; i++ {
		sw.send(api.Event{Kind: api.KindText, Text: fmt.Sprintf("%d", i)})
	}

	if got := len(sw.ch); got > bufSize {
		t.Errorf("queue length = %d, want <= %d (bounded)", got, bufSize)
	}
	if dropped == 0 {
		t.Error("expected at least one dropped event once the queue filled")
	}

	close(bw.release)
	sw.Close()
}

// TestSSEBufferSizeConfigurable verifies a small server.sse_buffer_size is
// actually honored by the writer built inside handlePostMessage (not just by
// the standalone sseWriter unit above), using the same overflow signal: a
// stalled HTTP client plus a burst of events large enough to overflow a
// buffer of 1 must not hang the request.
func TestSSEBufferSizeConfigurable(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Server:     config.ServerConfig{SSEBufferSize: 1},
	}
	burstAdapter := fixedAdapter{text: strings.Repeat("x", 4096)}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, burstAdapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		ch, err := cl.PostMessage(ctx, meta.ID, "go")
		if err != nil {
			done <- err
			return
		}
		for range ch {
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run with sse_buffer_size=1 did not complete in time")
	}
}
