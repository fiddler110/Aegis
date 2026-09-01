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
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/reqorigin"
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

	// P81.9: the allowlist now applies unconditionally, so a session outside
	// the daemon's own workspace needs the TUI/CLI origin carve-out to be
	// created at all — this test is about workdir confinement, not the
	// allowlist gate itself, so declare the origin that exercises it.
	metaA, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: dirA, Origin: reqorigin.TUI})
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}
	if metaA.Workdir != dirA {
		t.Errorf("session A Workdir = %q, want %q", metaA.Workdir, dirA)
	}
	metaB, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: dirB, Origin: reqorigin.TUI})
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

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: dirA, Origin: reqorigin.TUI})
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

// TestCreateSessionWorkdirTrustBoundary covers P25.1/P81.9's daemon
// safeguard: a session Workdir outside the daemon's own workspace is
// rejected (403) unless it falls under server.session_workdir_allowlist —
// enforced unconditionally now, not only once server.allow_remote is set,
// since a token holder that isn't the operator's own shell (an MCP client,
// an ACP editor plugin, a scheduled job) is exactly as able to reach a
// loopback-only daemon as a remote one.
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
		Server:     config.ServerConfig{SessionWorkdirAllowlist: []string{allowedParent}},
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

// TestSessionWorkdirOriginCarveOut is the P81.9 regression: the allowlist
// exemption that used to key off server.allow_remote (never set on a
// loopback-only daemon, so effectively always exempt) now keys off request
// origin. TUI and CLI — interactive, operator-driven local-shell surfaces —
// keep the exemption; Web, ACP and MCP, none of which represent the operator
// choosing a directory, do not.
func TestSessionWorkdirOriginCarveOut(t *testing.T) {
	defaultRoot := t.TempDir()
	outsideDir := t.TempDir()

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
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
	cl := client.New(ts.URL).WithToken("test-token")

	for _, origin := range []string{reqorigin.Web, reqorigin.ACP, reqorigin.MCP} {
		_, err := cl.CreateSession(context.Background(), api.CreateSessionRequest{Mode: "build", Workdir: outsideDir, Origin: origin})
		if err == nil {
			t.Errorf("origin %q: expected the allowlist to reject a session outside the daemon's workspace, got no error", origin)
		}
	}
	for _, origin := range []string{reqorigin.TUI, reqorigin.CLI} {
		if _, err := cl.CreateSession(context.Background(), api.CreateSessionRequest{Mode: "build", Workdir: outsideDir, Origin: origin}); err != nil {
			t.Errorf("origin %q: expected the interactive-local carve-out to allow it, got: %v", origin, err)
		}
	}
}

// TestSessionWorkdirTrustGrantSatisfiesAllowlist covers the other legitimate
// path P81.9 asks for: a directory the operator has already vetted via
// `aegis trust --dir` (config.TrustWorkspace) satisfies the allowlist gate
// for any origin, the same way workspace.additional_roots already reuses
// that trust store rather than a second consent mechanism.
func TestSessionWorkdirTrustGrantSatisfiesAllowlist(t *testing.T) {
	defaultRoot := t.TempDir()
	trustedDir := t.TempDir()

	// Redirect the workspace-trust store (config.WorkspaceTrustStorePath ->
	// defaultDataDir) into a scratch location so this test never touches the
	// real developer machine's grant file. Mirrors config package's own
	// redirectConfigDir test helper: HOME for Unix's os.UserHomeDir, APPDATA
	// for Windows' os.UserConfigDir, XDG_CONFIG_HOME so Linux doesn't fall
	// back to a real ~/.config.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("APPDATA", filepath.Join(fakeHome, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, ".config"))

	if err := config.TrustWorkspace(trustedDir); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
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
	cl := client.New(ts.URL).WithToken("test-token")

	if _, err := cl.CreateSession(context.Background(), api.CreateSessionRequest{Mode: "build", Workdir: trustedDir, Origin: reqorigin.MCP}); err != nil {
		t.Errorf("expected a trusted workdir to satisfy the allowlist for an MCP-origin session, got: %v", err)
	}
}

// TestDeleteSessionReapsUnsharedSpillDir is the P81.24 regression: deleting
// the last session rooted at a workdir must reap that workdir's
// .aegis/spill/ directory, closing the gap where a spill file (a verbatim
// copy of truncated tool output) outlived the session that produced it by up
// to spillTTL (24h).
func TestDeleteSessionReapsUnsharedSpillDir(t *testing.T) {
	defaultRoot := t.TempDir()
	sessionRoot := t.TempDir()
	spillDir := filepath.Join(sessionRoot, ".aegis", "spill")
	if err := os.MkdirAll(spillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spillDir, "leftover.txt"), []byte("stale spill"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cl, cleanup := newWorkdirTestServer(t, defaultRoot, &readFileAdapter{path: "target.txt"})
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: sessionRoot, Origin: reqorigin.TUI})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := cl.DeleteSession(ctx, meta.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, err := os.Stat(spillDir); !os.IsNotExist(err) {
		t.Errorf("expected the spill directory to be reaped after the last session on its workdir was deleted, stat err = %v", err)
	}
}

// TestDeleteSessionKeepsSpillDirWhileSharedWorkdirSessionLives is the other
// half: builtin.ReapSpillDir's own doc comment requires a caller to prove no
// other live session shares the workdir first, since spillText scopes files
// by workspace rather than by session.
func TestDeleteSessionKeepsSpillDirWhileSharedWorkdirSessionLives(t *testing.T) {
	defaultRoot := t.TempDir()
	sessionRoot := t.TempDir()
	spillDir := filepath.Join(sessionRoot, ".aegis", "spill")
	if err := os.MkdirAll(spillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spillDir, "leftover.txt"), []byte("stale spill"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cl, cleanup := newWorkdirTestServer(t, defaultRoot, &readFileAdapter{path: "target.txt"})
	defer cleanup()
	ctx := context.Background()

	metaA, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: sessionRoot, Origin: reqorigin.TUI})
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}
	metaB, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: sessionRoot, Origin: reqorigin.TUI})
	if err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}

	if err := cl.DeleteSession(ctx, metaA.ID); err != nil {
		t.Fatalf("DeleteSession A: %v", err)
	}
	if _, err := os.Stat(spillDir); err != nil {
		t.Fatalf("expected the spill directory to survive while session B still shares its workdir, stat err = %v", err)
	}

	if err := cl.DeleteSession(ctx, metaB.ID); err != nil {
		t.Fatalf("DeleteSession B: %v", err)
	}
	if _, err := os.Stat(spillDir); !os.IsNotExist(err) {
		t.Errorf("expected the spill directory to be reaped once the last sharing session was deleted, stat err = %v", err)
	}
}

// TestRunRetentionPruneDeletesOldArchivedSessions is the P81.24 wiring
// regression: config.CleanupConfig.ArchivedSessionTTLDays previously had no
// caller at all, so an archived session's conversation, traces and
// checkpoint file copies were immortal regardless of this setting.
// session.Store.PruneArchived itself is covered at the store level; this
// proves the daemon's own pruner actually calls it.
func TestRunRetentionPruneDeletesOldArchivedSessions(t *testing.T) {
	defaultRoot := t.TempDir()
	srv, cl, cleanup := newWorkdirTestServer(t, defaultRoot, &readFileAdapter{})
	defer cleanup()
	srv.cfg.Cleanup.ArchivedSessionTTLDays = 1
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := srv.store.Archive(ctx, meta.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	// Backdate archived_at past the 1-day TTL — PruneArchived's own store-level
	// tests already cover the predicate; this only needs a row old enough to
	// trip it.
	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	if _, err := srv.store.DB().ExecContext(ctx, `UPDATE sessions SET archived_at = ? WHERE id = ?`, old, meta.ID); err != nil {
		t.Fatalf("backdate archived_at: %v", err)
	}

	srv.runRetentionPrune()

	if _, err := cl.GetSession(ctx, meta.ID); err == nil {
		t.Error("expected the archived session to be pruned once past its ArchivedSessionTTLDays")
	}
}
