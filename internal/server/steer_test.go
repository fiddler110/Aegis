package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestSteerBoxFencesLateOffers covers the drain race P33.2 has to avoid: once
// the run's drain has closed the box, a steer that arrives after it must be
// refused rather than accepted into a channel nobody will ever read again.
func TestSteerBoxFencesLateOffers(t *testing.T) {
	b := newSteerBox(2)
	if err := b.offer("first"); err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := b.offer("second"); err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := b.offer("third"); !errors.Is(err, errSteerFull) {
		t.Errorf("offer past capacity = %v, want errSteerFull", err)
	}

	left := b.close()
	if len(left) != 2 || left[0] != "first" || left[1] != "second" {
		t.Fatalf("close() = %v, want [first second] in order", left)
	}
	if err := b.offer("late"); !errors.Is(err, errSteerClosed) {
		t.Errorf("offer after close = %v, want errSteerClosed", err)
	}
	if got := b.close(); got != nil {
		t.Errorf("second close() = %v, want nothing left (no double delivery)", got)
	}
}

// TestSteerUnconsumedHandedBackAtRunEnd is the P33.2 regression: a steer sent
// while the model is producing its final answer reaches no tool round, so the
// engine never injects it. Before the end-of-run drain it was dropped when the
// handler returned — no injection, no event, no trace of the text the user
// typed. Now the run hands it back as KindSteerUnconsumed before the stream
// closes, and refuses any further steer for the finished run.
func TestSteerUnconsumedHandedBackAtRunEnd(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	adapter := newGatedAdapter()
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
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
	select {
	case <-adapter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the model call")
	}

	if err := cl.Steer(ctx, meta.ID, "actually, stop and explain"); err != nil {
		t.Fatalf("Steer during active run: %v", err)
	}
	close(adapter.release)

	var unconsumed []string
	for ev := range ch {
		switch ev.Kind {
		case api.KindSteer:
			t.Errorf("steer was injected on a text-only run: %q", ev.Text)
		case api.KindSteerUnconsumed:
			unconsumed = append(unconsumed, ev.Text)
		case api.KindError:
			t.Fatalf("run errored: %s", ev.Error)
		}
	}
	if len(unconsumed) != 1 || unconsumed[0] != "actually, stop and explain" {
		t.Fatalf("unconsumed steers = %v, want the one steer that was never injected", unconsumed)
	}

	err = cl.Steer(ctx, meta.ID, "too late")
	if err == nil {
		t.Fatal("expected a steer for a finished run to be refused")
	}
	// P33.15 #1: the client must be able to recover the 404 status without
	// parsing the error string, so the TUI can tell "run already ended, not
	// retryable" apart from a 429 "buffer full, retryable".
	var statusErr *client.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v (%T), want it to unwrap to a *client.StatusError", err, err)
	}
	if statusErr.Code != http.StatusNotFound {
		t.Errorf("statusErr.Code = %d, want 404 for a steer on a finished run", statusErr.Code)
	}
}

// TestSteerFullReturns429RetryableStatusError is the other half of the
// P33.15 #1 status-code distinction: a steer buffer that's full
// (errSteerFull) is a run still very much active, and the daemon reports it
// as 429 rather than 404 — the client must surface that distinctly, since
// it's retryable where a 404 is not.
func TestSteerFullReturns429RetryableStatusError(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	adapter := newGatedAdapter()
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
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
	select {
	case <-adapter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the model call")
	}

	// newSteerBox(8): the first 8 fill the buffer, the 9th must be refused
	// with 429 while the run is still active (unlike the 404 case above).
	for i := 0; i < 8; i++ {
		if err := cl.Steer(ctx, meta.ID, fmt.Sprintf("steer %d", i)); err != nil {
			t.Fatalf("Steer %d: %v", i, err)
		}
	}
	err = cl.Steer(ctx, meta.ID, "one too many")
	if err == nil {
		t.Fatal("expected the 9th steer to be refused once the buffer is full")
	}
	var statusErr *client.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v (%T), want it to unwrap to a *client.StatusError", err, err)
	}
	if statusErr.Code != http.StatusTooManyRequests {
		t.Errorf("statusErr.Code = %d, want 429 for a full steer buffer", statusErr.Code)
	}

	close(adapter.release)
	for range ch {
	}
}
