package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"net/http/httptest"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// buildRepoMapCache writes root/.aegis/repomap.json the same way
// handleRepoMapIndex/`aegis index` do, so loadRepoMap has a cache to read
// instead of returning "" for an unindexed root.
func buildRepoMapCache(t *testing.T, root string) {
	t.Helper()
	m, err := repomap.Build(root, repomap.Options{})
	if err != nil {
		t.Fatalf("repomap.Build(%s): %v", root, err)
	}
	if err := m.Save(filepath.Join(root, ".aegis", "repomap.json")); err != nil {
		t.Fatalf("repomap.Save(%s): %v", root, err)
	}
}

// newScopingTestServer builds a bare daemon rooted at workspace, mirroring
// the newKnowledgeTestServer/newScanTestServer pattern used elsewhere in
// this package.
func newScopingTestServer(t *testing.T, workspace string) (*Server, *client.Client) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.workspace = workspace

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, client.New(ts.URL).WithToken("test-token")
}

// TestKnowledgeStoreForIsolatesRoots is the P25.9 regression: a session on a
// root other than the daemon's own workspace must get its own knowledge
// store, isolated from both the daemon's and any other root's.
func TestKnowledgeStoreForIsolatesRoots(t *testing.T) {
	daemonWS := t.TempDir()
	srv, _ := newScopingTestServer(t, daemonWS)

	otherRoot := t.TempDir()
	store, err := srv.knowledgeStoreFor(otherRoot)
	if err != nil {
		t.Fatalf("knowledgeStoreFor: %v", err)
	}
	if store == nil {
		t.Fatal("expected a non-nil store for a non-default root")
	}
	// srv.knowledgeStores caches this instance for srv's lifetime, so close
	// it explicitly rather than relying on srv teardown (there is none —
	// this test never calls ListenAndServe) to release the sqlite handle
	// before t.TempDir()'s cleanup removes the underlying directory.
	t.Cleanup(func() { store.Close() })
	if store == srv.knowledge {
		t.Error("expected a distinct store for a non-default root, got the daemon's own")
	}

	ctx := context.Background()
	if _, err := store.Index(ctx); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Same root resolves to the same cached instance, not a fresh Open.
	again, err := srv.knowledgeStoreFor(otherRoot)
	if err != nil {
		t.Fatalf("knowledgeStoreFor (cached): %v", err)
	}
	if again != store {
		t.Error("expected the cached store instance on a second call for the same root")
	}

	// The default workspace's fast path is unaffected.
	def, err := srv.knowledgeStoreFor(daemonWS)
	if err != nil {
		t.Fatalf("knowledgeStoreFor(daemonWS): %v", err)
	}
	if def != srv.knowledge {
		t.Error("expected the daemon's own store for its default workspace")
	}
	def2, err := srv.knowledgeStoreFor("")
	if err != nil || def2 != srv.knowledge {
		t.Error("expected the daemon's own store for an empty root")
	}
}

// TestRepoMapForDiffersPerRoot is the P25.9 regression: effectiveSystem must
// inject a repo-map block reflecting each session's own Workdir, not always
// the daemon's own workspace.
func TestRepoMapForDiffersPerRoot(t *testing.T) {
	daemonWS := t.TempDir()
	if err := os.WriteFile(filepath.Join(daemonWS, "daemon_only.go"), []byte("package daemon\n\nfunc DaemonOnly() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildRepoMapCache(t, daemonWS)
	srv, cl := newScopingTestServer(t, daemonWS)
	// newScopingTestServer doesn't go through New(), so prime the daemon's
	// own fast-path repoMap field the way New() would at startup.
	srv.repoMap = loadRepoMap(daemonWS, srv.logger)

	sessionRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionRoot, "session_only.go"), []byte("package sessionpkg\n\nfunc SessionOnly() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildRepoMapCache(t, sessionRoot)

	ctx := context.Background()
	sess, err := cl.CreateSession(ctx, api.CreateSessionRequest{Workdir: sessionRoot})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	daemonMap := srv.repoMapFor(daemonWS)
	sessionMap := srv.repoMapFor(sessionRoot)
	if daemonMap == sessionMap {
		t.Error("expected different repo-map blocks for different roots")
	}
	if !strings.Contains(sessionMap, "session_only.go") {
		t.Errorf("session repo map missing session_only.go:\n%s", sessionMap)
	}
	if strings.Contains(daemonMap, "session_only.go") {
		t.Errorf("daemon repo map leaked the session's file:\n%s", daemonMap)
	}

	// effectiveSystem for that session picks up its own root's map.
	sys := srv.effectiveSystem("base", sess.ID)
	if !strings.Contains(sys, "session_only.go") {
		t.Errorf("effectiveSystem for the session did not include its own repo map:\n%s", sys)
	}

	// The default session (no Workdir) is unaffected.
	defaultSys := srv.effectiveSystem("base", "")
	if strings.Contains(defaultSys, "session_only.go") {
		t.Errorf("default session's system prompt leaked the other session's repo map:\n%s", defaultSys)
	}
}

// TestPersonaForSeesSessionsOwnProjectPersona is the P25.9 regression: a
// session created against a Workdir with its own .aegis/personas/ directory
// must resolve that project's persona, not just the daemon's own.
func TestPersonaForSeesSessionsOwnProjectPersona(t *testing.T) {
	daemonWS := t.TempDir()
	_, cl := newScopingTestServer(t, daemonWS)

	sessionRoot := t.TempDir()
	personaDir := filepath.Join(sessionRoot, ".aegis", "personas")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ndescription: session-scoped reviewer\n---\nYou are the session-scoped reviewer."
	if err := os.WriteFile(filepath.Join(personaDir, "session-reviewer.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	sess, err := cl.CreateSession(ctx, api.CreateSessionRequest{Workdir: sessionRoot, Persona: "session-reviewer"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := cl.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !strings.Contains(got.System, "session-scoped reviewer") {
		t.Errorf("session system prompt = %q, want it to contain the session's own project persona body", got.System)
	}
}
