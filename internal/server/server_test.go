package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"net/http"
	"strings"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/hooks"
	"github.com/scottymacleod/aegis/internal/memory"
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
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL).WithToken("test-token")
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
	srv.authToken = "test-token"

	reg := swarm.NewRegistry()
	id := swarm.NewIdentity("explore-1", "default", "sess")
	reg.Add(id)
	reg.Update(id.AgentID, swarm.StatusDone, "found it")
	srv.swarmReg = reg

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")

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

func TestServerPersistsTurnTrace(t *testing.T) {
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
	// Trace events must NOT leak to the SSE client.
	for ev := range ch {
		if string(ev.Kind) == "trace" {
			t.Errorf("trace event leaked to SSE client: %+v", ev)
		}
		if ev.Kind == api.KindError {
			t.Fatalf("error event: %s", ev.Error)
		}
	}

	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Traces) != 1 {
		t.Fatalf("persisted %d traces, want 1", len(sess.Traces))
	}
	tr := sess.Traces[0]
	if tr.InputTokens != 5 || tr.OutputTokens != 2 {
		t.Errorf("trace tokens = %d/%d, want 5/2", tr.InputTokens, tr.OutputTokens)
	}
	if tr.Model != "test" {
		t.Errorf("trace model = %q, want \"test\"", tr.Model)
	}
}

func TestServerHealthEndpoint(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	if err := cl.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestServerGetSessionNotFound(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	_, err := cl.GetSession(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestServerAuthRequired(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token-123"

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()

	// No token → 401 on session endpoints.
	resp, err := http.Get(ts.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	// With token → 200.
	req, _ := http.NewRequest("GET", ts.URL+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status with token = %d, want 200", resp.StatusCode)
	}

	// Health bypasses auth.
	resp, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}
}

func TestServerOriginBlocking(t *testing.T) {
	cl, cleanup := newTestServer(t)
	_ = cl
	defer cleanup()

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()

	// Non-loopback origin → 403.
	req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-loopback origin: status = %d, want 403", resp.StatusCode)
	}

	// Loopback origin → OK.
	req, _ = http.NewRequest("GET", ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "http://127.0.0.1:4127")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("loopback origin: status = %d, want 200", resp.StatusCode)
	}
}

func TestEffectiveSystemCombinesMemory(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.memory = memory.Sources{ProjectRoot: root, DataDir: filepath.Join(root, "data")}

	// With no memory files, effectiveSystem returns the base system plus the
	// platform block (which is always injected).
	got := srv.effectiveSystem("base prompt")
	if !strings.Contains(got, "base prompt") {
		t.Errorf("effectiveSystem missing base prompt: %q", got)
	}
	if !strings.Contains(got, "Execution Environment") {
		t.Errorf("effectiveSystem missing platform block: %q", got)
	}

	// Create a memory file and check it gets appended.
	if err := memory.Append(srv.memory.ProjectMemoryPath(), "test fact"); err != nil {
		t.Fatal(err)
	}
	got = srv.effectiveSystem("base prompt")
	if !strings.Contains(got, "base prompt") || !strings.Contains(got, "test fact") {
		t.Errorf("effectiveSystem didn't include memory: %q", got)
	}
}

func TestAuditCloseCleanup(t *testing.T) {
	dir := t.TempDir()
	a := hooks.NewAudit(filepath.Join(dir, "audit.jsonl"))
	a.PreToolUse(context.Background(), "test_tool", nil)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Double-close should be safe.
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestDeriveTitle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello world", "hello world"},
		{"  spaced  out  ", "spaced  out"},
		{strings.Repeat("x", 100), strings.Repeat("x", 60) + "…"},
	}
	for _, tt := range tests {
		got := deriveTitle(tt.in)
		if got != tt.want {
			t.Errorf("deriveTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPatchSession(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}

	// Patch mode.
	mode := "build"
	updated, err := cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Mode: &mode})
	if err != nil {
		t.Fatalf("UpdateSession mode: %v", err)
	}
	if updated.Mode != "build" {
		t.Errorf("mode = %q, want build", updated.Mode)
	}

	// Patch system via persona: prefix.
	system := "persona:security"
	_, err = cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{System: &system})
	if err != nil {
		t.Fatalf("UpdateSession persona: %v", err)
	}
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sess.System, "SECURITY PLATFORM ARCHITECT") {
		t.Errorf("system not updated to security persona, got %q...", sess.System[:50])
	}

	// Invalid mode rejected.
	bad := "invalid"
	_, err = cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Mode: &bad})
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestListPersonas(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	personas, err := cl.ListPersonas(context.Background())
	if err != nil {
		t.Fatalf("ListPersonas: %v", err)
	}
	if len(personas) < 10 {
		t.Errorf("expected at least 10 personas, got %d", len(personas))
	}
	found := false
	for _, p := range personas {
		if p.Name == "security" {
			found = true
		}
	}
	if !found {
		t.Error("security persona not found")
	}
}

func TestMemoryEndpoints(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:    filepath.Join(root, "data"),
		Provider:   config.ProviderConfig{Model: "test"},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.memory = memory.Sources{ProjectRoot: root, DataDir: cfg.DataDir}
	srv.workspace = root
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")

	ctx := context.Background()

	// Initially empty.
	mem, err := cl.GetMemory(ctx)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if mem.ProjectMemory != "" || mem.UserMemory != "" {
		t.Errorf("expected empty memory, got project=%q user=%q", mem.ProjectMemory, mem.UserMemory)
	}

	// Append.
	if err := cl.AppendMemory(ctx, api.AppendMemoryRequest{Entry: "test fact", Scope: "project"}); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}

	// Verify.
	mem, err = cl.GetMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mem.ProjectMemory, "test fact") {
		t.Errorf("project memory = %q, want to contain 'test fact'", mem.ProjectMemory)
	}
}

func TestListCommands(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	cmds, err := cl.ListCommands(context.Background())
	if err != nil {
		t.Fatalf("ListCommands: %v", err)
	}
	// No custom commands expected in test environment.
	if cmds == nil {
		t.Error("expected non-nil (empty) list")
	}
}

func TestIsLoopbackOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://127.0.0.1:4127", true},
		{"http://localhost:4127", true},
		{"http://[::1]:4127", true},
		{"http://[::1]", true}, // IPv6 loopback without an explicit port
		{"http://evil.com", false},
		{"http://192.168.1.1:4127", false},
	}
	for _, tt := range tests {
		if got := isLoopbackOrigin(tt.origin); got != tt.want {
			t.Errorf("isLoopbackOrigin(%q) = %v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestEffectiveSystem_containsToolUseBlock(t *testing.T) {
	s := &Server{
		memory:    memory.Sources{},
		workspace: "",
	}
	out := s.effectiveSystem("base-system")

	if !strings.Contains(out, "A tool result is input") {
		t.Error("effectiveSystem output missing ToolUseBlock content")
	}
	tuIdx := strings.Index(out, "A tool result is input")
	ctIdx := strings.Index(out, "## Completing tasks")
	if tuIdx == -1 || ctIdx == -1 {
		t.Fatalf("missing expected block markers: tuIdx=%d ctIdx=%d", tuIdx, ctIdx)
	}
	if tuIdx > ctIdx {
		t.Error("ToolUseBlock must appear before CompletingTasksBlock in effectiveSystem output")
	}
}
