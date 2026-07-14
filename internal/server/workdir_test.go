package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// readFileAdapter issues a single read_file tool call for a fixed
// workspace-relative path, then returns a final text answer. It is stateless
// across calls (it inspects the conversation history for a prior tool
// result rather than keeping its own counter), so one instance can safely
// drive turns for multiple sessions with different working directories
// without mixing up state (P25.1).
type readFileAdapter struct{ path string }

func (a *readFileAdapter) Name() string { return "readfile" }

func (a *readFileAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	done := false
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if _, ok := b.(provider.ToolResultBlock); ok {
				done = true
			}
		}
	}
	ch := make(chan provider.Event, 4)
	if !done {
		input := json.RawMessage(fmt.Sprintf(`{"path":%q}`, a.path))
		ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu1", Name: "read_file", Input: input}}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{}}
	} else {
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "done"}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	}
	close(ch)
	return ch, nil
}

func newWorkdirTestServer(t *testing.T, defaultRoot string, adapter provider.Adapter) (*Server, *client.Client, func()) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: defaultRoot}); err != nil {
		t.Fatal(err)
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, reg)
	srv.workspace = defaultRoot
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL).WithToken("test-token")
	return srv, cl, func() { ts.Close(); store.Close() }
}

// toolResults drains a message-run event channel and returns the content of
// every KindToolResult event, in order.
func toolResults(ch <-chan api.Event) []api.Event {
	var out []api.Event
	for ev := range ch {
		if ev.Kind == api.KindToolResult {
			out = append(out, ev)
		}
	}
	return out
}

// TestSessionWorkdirConfinement is the core P25.1 regression: two sessions on
// one shared daemon, each given a different Workdir at creation, must read
// their own "target.txt" via the same relative path and tool call — not the
// daemon's own default workspace, and not each other's directory. This is
// the exact "TUI in dir X, daemon started in dir Y" scenario from the
// roadmap's live-eval writeup, driven end to end over the HTTP API.
func TestSessionWorkdirConfinement(t *testing.T) {
	defaultRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(defaultRoot, "target.txt"), []byte("content-default"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirA := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "target.txt"), []byte("content-A"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirB, "target.txt"), []byte("content-B"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cl, cleanup := newWorkdirTestServer(t, defaultRoot, &readFileAdapter{path: "target.txt"})
	defer cleanup()
	ctx := context.Background()

	metaA, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: dirA})
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}
	if metaA.Workdir != dirA {
		t.Errorf("session A Workdir = %q, want %q", metaA.Workdir, dirA)
	}
	metaB, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: dirB})
	if err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}

	chA, err := cl.PostMessage(ctx, metaA.ID, "read target.txt")
	if err != nil {
		t.Fatalf("PostMessage A: %v", err)
	}
	resA := toolResults(chA)
	if len(resA) != 1 || !strings.Contains(resA[0].ToolResult, "content-A") {
		t.Fatalf("session A tool result = %+v, want content containing content-A", resA)
	}

	chB, err := cl.PostMessage(ctx, metaB.ID, "read target.txt")
	if err != nil {
		t.Fatalf("PostMessage B: %v", err)
	}
	resB := toolResults(chB)
	if len(resB) != 1 || !strings.Contains(resB[0].ToolResult, "content-B") {
		t.Fatalf("session B tool result = %+v, want content containing content-B", resB)
	}
}

// TestSessionWorkdirEscapeErrorNamesSessionRoot verifies the workspace-
// confinement error a session sees when a tool call reaches outside its own
// directory names *that session's* root, not the daemon's default
// workspace — the fix's answer to the "read_file with the session dir's
// absolute path was refused" symptom from the roadmap: the confinement
// boundary itself must track the session, and its error text must be
// debuggable against the boundary that was actually applied.
func TestSessionWorkdirEscapeErrorNamesSessionRoot(t *testing.T) {
	defaultRoot := t.TempDir()
	dirA := t.TempDir()
	dirB := t.TempDir()
	outsidePath := filepath.Join(dirB, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cl, cleanup := newWorkdirTestServer(t, defaultRoot, &readFileAdapter{path: outsidePath})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: dirA})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Compare against the server's own resolved/stored root (meta.Workdir)
	// rather than the raw dirA string: the daemon resolves the client-sent
	// path with filepath.Abs, which is free to normalize representation
	// (e.g. drive-letter casing on Windows) without changing which
	// directory it denotes — the test cares that the error names *that*
	// resolved root, not that it byte-matches the pre-resolution input.
	ch, err := cl.PostMessage(ctx, meta.ID, "read the file")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	res := toolResults(ch)
	if len(res) != 1 || !res[0].ToolIsError {
		t.Fatalf("tool result = %+v, want a single error result", res)
	}
	// pathvalidator.go formats both path and root with %q (a Go-syntax
	// quoted string literal, which escapes backslashes on Windows paths) —
	// match that exact rendering rather than the raw path, or a Windows
	// path's single backslashes will never substring-match its doubled-up
	// %q form.
	if !strings.Contains(res[0].ToolResult, strconv.Quote(meta.Workdir)) {
		t.Errorf("confinement error = %q, want it to name the session root %q", res[0].ToolResult, meta.Workdir)
	}
	if strings.Contains(res[0].ToolResult, strconv.Quote(defaultRoot)) {
		t.Errorf("confinement error = %q, must not name the daemon's default workspace %q", res[0].ToolResult, defaultRoot)
	}
}

// TestCreateSessionRejectsMissingWorkdir covers the 400 path: a Workdir that
// doesn't exist (or isn't a directory) must be rejected at session creation,
// before any tool ever runs against it.
func TestCreateSessionRejectsMissingWorkdir(t *testing.T) {
	defaultRoot := t.TempDir()
	_, cl, cleanup := newWorkdirTestServer(t, defaultRoot, &readFileAdapter{path: "target.txt"})
	defer cleanup()

	_, err := cl.CreateSession(context.Background(), api.CreateSessionRequest{
		Mode:    "build",
		Workdir: filepath.Join(defaultRoot, "does-not-exist"),
	})
	if err == nil {
		t.Fatal("expected an error creating a session with a nonexistent workdir")
	}
}

// TestCreateSessionWorkdirTrustBoundary covers P25.1's remote-daemon
// safeguard: once server.allow_remote is set, a session Workdir outside the
// daemon's own workspace is rejected (403) unless it falls under
// server.session_workdir_allowlist — a remote-accessible daemon must not
// become an arbitrary-filesystem oracle, one session at a time.
func TestCreateSessionWorkdirTrustBoundary(t *testing.T) {
	defaultRoot := t.TempDir()
	outsideDir := t.TempDir()
	allowedParent := t.TempDir()
	allowedChild := filepath.Join(allowedParent, "child")
	if err := os.MkdirAll(allowedChild, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
		Server:     config.ServerConfig{AllowRemote: true, SessionWorkdirAllowlist: []string{allowedParent}},
	}
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: defaultRoot}); err != nil {
		t.Fatal(err)
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, &readFileAdapter{}, reg)
	srv.workspace = defaultRoot
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(workdir string) int {
		body, _ := json.Marshal(api.CreateSessionRequest{Mode: "build", Workdir: workdir})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sessions", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(outsideDir); got != http.StatusForbidden {
		t.Errorf("workdir outside workspace/allowlist: status = %d, want 403", got)
	}
	if got := post(allowedChild); got != http.StatusCreated {
		t.Errorf("workdir nested under allowlisted root: status = %d, want 201", got)
	}
	if got := post(defaultRoot); got != http.StatusCreated {
		t.Errorf("workdir equal to the daemon's own workspace: status = %d, want 201", got)
	}
	if got := post(""); got != http.StatusCreated {
		t.Errorf("empty workdir (default behavior): status = %d, want 201", got)
	}

	// P31.2: a nonexistent path outside the allowlist must still be 403, not
	// 400 — otherwise a remote client could use the existence-vs-permission
	// status code to probe for arbitrary paths on the host before ever
	// clearing the allowlist gate.
	nonexistentOutside := filepath.Join(outsideDir, "does-not-exist")
	if got := post(nonexistentOutside); got != http.StatusForbidden {
		t.Errorf("nonexistent workdir outside workspace/allowlist: status = %d, want 403 (not 400 — must not leak existence before the allowlist gate)", got)
	}
}
