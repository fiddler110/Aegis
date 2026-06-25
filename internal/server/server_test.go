package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/session"
	"github.com/scottymacleod/aegis/internal/swarm"
	"github.com/scottymacleod/aegis/internal/tool"
)

// fixedAdapter returns a single text response regardless of input.
type fixedAdapter struct{ text string }

func (fixedAdapter) Name() string { return "fixed" }
func (a fixedAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: a.text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 2}}
	close(ch)
	return ch, nil
}

func newTestServer(t *testing.T) (*client.Client, func()) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hello from agent"}, tool.NewRegistry())

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL)
	return cl, func() { ts.Close(); store.Close() }
}

func TestServerSessionLifecycle(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if meta.Mode != "build" {
		t.Errorf("mode = %q, want build", meta.Mode)
	}

	list, err := cl.ListSessions(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSessions: %v len=%d", err, len(list))
	}

	if err := cl.DeleteSession(ctx, meta.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	list, _ = cl.ListSessions(ctx)
	if len(list) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(list))
	}
}

func TestServerListTeammates(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())

	reg := swarm.NewRegistry()
	id := swarm.NewIdentity("explore-1", "default", "sess")
	reg.Add(id)
	reg.Update(id.AgentID, swarm.StatusDone, "found it")
	srv.swarmReg = reg

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL)

	tms, err := cl.Teammates(context.Background())
	if err != nil {
		t.Fatalf("Teammates: %v", err)
	}
	if len(tms) != 1 {
		t.Fatalf("got %d teammates, want 1", len(tms))
	}
	if tms[0].AgentID != id.AgentID || tms[0].Status != "done" || tms[0].Summary != "found it" {
		t.Errorf("teammate = %+v", tms[0])
	}
}

func TestServerListTeammatesEmpty(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	tms, err := cl.Teammates(context.Background())
	if err != nil {
		t.Fatalf("Teammates: %v", err)
	}
	if len(tms) != 0 {
		t.Errorf("expected no teammates, got %d", len(tms))
	}
}

func TestServerMessageStreaming(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cl.PostMessage(ctx, meta.ID, "say hi")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	var text string
	var sawDone bool
	for ev := range ch {
		switch ev.Kind {
		case api.KindText:
			text += ev.Text
		case api.KindDone:
			sawDone = true
		case api.KindError:
			t.Fatalf("error event: %s", ev.Error)
		}
	}
	if text != "hello from agent" {
		t.Errorf("streamed text = %q", text)
	}
	if !sawDone {
		t.Error("did not receive done event")
	}

	// The exchange must have been persisted: user + assistant.
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 2 {
		t.Fatalf("persisted %d messages, want 2", len(sess.Messages))
	}
	if sess.Title == "" {
		t.Error("expected a derived title")
	}
	// Sanity: assistant message round-tripped as text.
	b, _ := json.Marshal(sess.Messages[1])
	if !json.Valid(b) {
		t.Error("assistant message did not serialize")
	}
}
