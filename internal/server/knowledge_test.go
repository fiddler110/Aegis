package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
	"net/http/httptest"
)

// newKnowledgeTestServer builds a daemon with its workspace pinned to a fresh
// temp dir containing one README, and a real (non-nil) knowledge store wired
// up the same way the daemon wires one at startup — so /knowledge exercises
// the same store instance a running daemon would use, mirroring
// newScanTestServer's approach for /security/scan.
func newKnowledgeTestServer(t *testing.T) *client.Client {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# Widget\n\nThe widget subsystem handles frobnication."), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	kstore, err := knowledge.Open(workspace, filepath.Join(workspace, ".aegis", "knowledge.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kstore.Close() })

	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.workspace = workspace
	srv.knowledge = kstore

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token")
}

// TestHandleKnowledgeIndexThenQuery is the core /knowledge regression:
// indexing the workspace's README must make it findable by a subsequent
// query, the same round trip `aegis knowledge index` + the project_knowledge
// tool provide.
func TestHandleKnowledgeIndexThenQuery(t *testing.T) {
	cl := newKnowledgeTestServer(t)
	ctx := context.Background()

	idxResp, err := cl.Knowledge(ctx, api.KnowledgeRequest{Action: "index"})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if idxResp.DocCount < 1 {
		t.Errorf("doc count = %d, want at least 1 (the README)", idxResp.DocCount)
	}
	if idxResp.DBPath == "" {
		t.Error("expected a non-empty db path")
	}

	queryResp, err := cl.Knowledge(ctx, api.KnowledgeRequest{Action: "query", Query: "frobnication"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if queryResp.Count < 1 {
		t.Fatalf("count = %d, want at least 1 match for a term straight from the indexed README", queryResp.Count)
	}
	if queryResp.Results[0].Path == "" {
		t.Error("expected a non-empty result path")
	}
}

// TestHandleKnowledgeQueryRequiresQueryText checks the empty-query case is
// rejected rather than silently searching for nothing.
func TestHandleKnowledgeQueryRequiresQueryText(t *testing.T) {
	cl := newKnowledgeTestServer(t)
	_, err := cl.Knowledge(context.Background(), api.KnowledgeRequest{Action: "query"})
	if err == nil {
		t.Fatal("expected an error for a query action with no query text")
	}
}

// TestHandleKnowledgeUnknownAction checks an action other than index/query is
// rejected with a clear error rather than silently doing nothing.
func TestHandleKnowledgeUnknownAction(t *testing.T) {
	cl := newKnowledgeTestServer(t)
	_, err := cl.Knowledge(context.Background(), api.KnowledgeRequest{Action: "bogus"})
	if err == nil {
		t.Fatal("expected an error for an unknown action")
	}
}

// TestHandleKnowledgeUnavailableWithoutStore checks a daemon that started
// without a knowledge store (e.g. it failed to open) reports 503 rather than
// panicking on a nil store.
func TestHandleKnowledgeUnavailableWithoutStore(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.workspace = t.TempDir()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL)

	if _, err := cl.Knowledge(context.Background(), api.KnowledgeRequest{Action: "index"}); err == nil {
		t.Fatal("expected an error when no knowledge store is configured")
	}
}

// TestHandleRepoMapIndexRebuildsCacheAndPrompt is the core /repomap/index
// regression: it must both write the on-disk cache `aegis index` writes and
// refresh the daemon's own cached system-prompt block, so a subsequent turn
// picks up the new map without a restart.
func TestHandleRepoMapIndexRebuildsCacheAndPrompt(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workspace := t.TempDir()
	src := "package widget\n\nfunc Frobnicate() {}\n"
	if err := os.WriteFile(filepath.Join(workspace, "widget.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.workspace = workspace

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")

	before := srv.effectiveSystem("base")
	resp, err := cl.RepoMapIndex(context.Background())
	if err != nil {
		t.Fatalf("RepoMapIndex: %v", err)
	}
	if resp.FileCount < 1 {
		t.Errorf("file count = %d, want at least 1 (widget.go)", resp.FileCount)
	}
	if _, err := os.Stat(resp.Path); err != nil {
		t.Errorf("expected the cache file to exist at %s: %v", resp.Path, err)
	}

	after := srv.effectiveSystem("base")
	if after == before {
		t.Error("expected effectiveSystem to change after indexing (repo map should now be injected)")
	}
}
