package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestDeleteSessionFreesPerSessionRunState is P66.15: sessionSems and
// sessionPermCache are the two per-session maps that were never freed. Every
// other one (sessionTools, sessionWorkdirs, sessionSkills, taskScopes,
// promptSectionCache) was already cleaned up on delete; these two grew for the
// daemon's lifetime, so a loopback caller creating and deleting sessions in a
// loop leaked a channel plus one entry per approved tool, indefinitely.
func TestDeleteSessionFreesPerSessionRunState(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store,
		fixedAdapter{text: "hello from agent"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Both maps are populated lazily by a run, not by session creation: take
	// the semaphore the way handlePostMessage does and record an
	// "allow always" grant the way sseApprover does.
	srv.sessionSemaphore(meta.ID)
	srv.sess.permCache.Store(meta.ID+"\x00shell", struct{}{})
	srv.sess.permCache.Store(meta.ID+"\x00write_file", struct{}{})
	// A second session's grant must survive the first one's delete.
	srv.sess.permCache.Store("other-session\x00shell", struct{}{})
	// M6: toolCallWarned is the third map keyed by session, and was the one
	// still missing from this list — it records that a session was told its
	// model cannot call tools, and nothing ever removed the entry.
	srv.toolCallWarnedMu.Lock()
	srv.toolCallWarned = map[string]struct{}{
		meta.ID + "\x00qwen3:14b":    {},
		meta.ID + "\x00llama3.2:3b":  {},
		"other-session\x00qwen3:14b": {},
	}
	srv.toolCallWarnedMu.Unlock()

	if err := cl.DeleteSession(ctx, meta.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, ok := srv.sess.sems.Load(meta.ID); ok {
		t.Error("sessionSems still holds the deleted session's semaphore")
	}
	if got := countKeys(&srv.sess.permCache, meta.ID+"\x00"); got != 0 {
		t.Errorf("sessionPermCache still holds %d grant(s) for the deleted session", got)
	}
	if got := countKeys(&srv.sess.permCache, "other-session\x00"); got != 1 {
		t.Errorf("another session's grants were dropped too: %d remain, want 1", got)
	}
	srv.toolCallWarnedMu.Lock()
	remaining := make([]string, 0, len(srv.toolCallWarned))
	for k := range srv.toolCallWarned {
		remaining = append(remaining, k)
	}
	srv.toolCallWarnedMu.Unlock()
	if len(remaining) != 1 || !strings.HasPrefix(remaining[0], "other-session\x00") {
		t.Errorf("toolCallWarned after delete = %q, want only the other session's entry", remaining)
	}
}

// TestDeleteSessionFreesWebCache is P71.6's own cleanup half:
// sessionWebCache follows the same lazily-created-on-first-use,
// freed-on-delete shape as sessionTools, so a session's web_fetch/web_search
// memoization does not outlive the session.
func TestDeleteSessionFreesWebCache(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store,
		fixedAdapter{text: "hello from agent"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	cache := srv.sessionWebCacheFor(meta.ID)
	cache.Set("https://example.com/", "cached body")
	if _, _, ok := srv.sessionWebCacheFor(meta.ID).Get("https://example.com/"); !ok {
		t.Fatal("expected the entry to be readable before delete")
	}

	if err := cl.DeleteSession(ctx, meta.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, ok := srv.sess.webCache.Load(meta.ID); ok {
		t.Error("sessionWebCache still holds the deleted session's cache")
	}
	// A fresh lookup after delete must build a new, empty cache rather than
	// reusing whatever survived the map deletion by reference.
	if _, _, ok := srv.sessionWebCacheFor(meta.ID).Get("https://example.com/"); ok {
		t.Error("cache recreated after delete should be empty")
	}
}

// TestPruneFreesPerSessionStateToo is P66.20/ARCH-10's remaining half. The
// delete handler's cleanup list was complete; the *prune* paths cleaned
// nothing at all — Store.Prune and Store.PruneArchived delete rows by
// predicate and report a count, so every map keyed by a pruned session's id
// kept its entry for the daemon's lifetime. A daemon with retention configured
// leaks exactly the state a daemon without it does not.
func TestPruneFreesPerSessionStateToo(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store,
		fixedAdapter{text: "hello from agent"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	gone, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	kept, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Populate every map forgetSession is responsible for, for both sessions.
	for _, id := range []string{gone.ID, kept.ID} {
		srv.sessionToolRegistry(id)
		srv.taskScopeFor(id)
		srv.sessionWebCacheFor(id)
		srv.sessionSemaphore(id)
		// A default-workdir session is never tracked in sessionWorkdirs (see
		// handleCreateSession), so seed it directly — the map still has to be
		// reconciled, and it is the one whose residue would matter most.
		srv.sess.workdirs.Store(id, t.TempDir())
		srv.sess.skills.Store(id, []string{"deep-research"})
		srv.sess.promptCache.Store(id, &sync.Map{})
		srv.sess.permCache.Store(id+"\x00shell", struct{}{})
		srv.toolCallWarnedMu.Lock()
		if srv.toolCallWarned == nil {
			srv.toolCallWarned = map[string]struct{}{}
		}
		srv.toolCallWarned[id+"\x00qwen3:14b"] = struct{}{}
		srv.toolCallWarnedMu.Unlock()
	}

	// Delete the row the way Prune does — straight out of the store, with no
	// handler-side cleanup — then run the reconciliation the prune handler now
	// performs.
	if err := store.Delete(ctx, gone.ID); err != nil {
		t.Fatalf("store.Delete: %v", err)
	}
	w := httptest.NewRecorder()
	srv.handlePruneSessions(w, httptest.NewRequest("POST", "/sessions/prune?days=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("POST /sessions/prune = %d, body %s", w.Code, w.Body.String())
	}

	for _, tc := range []struct {
		name string
		load func(string) bool
	}{
		{"tools", func(id string) bool { _, ok := srv.sess.tools.Load(id); return ok }},
		{"workdirs", func(id string) bool { _, ok := srv.sess.workdirs.Load(id); return ok }},
		{"skills", func(id string) bool { _, ok := srv.sess.skills.Load(id); return ok }},
		{"taskScopes", func(id string) bool { _, ok := srv.sess.taskScopes.Load(id); return ok }},
		{"promptCache", func(id string) bool { _, ok := srv.sess.promptCache.Load(id); return ok }},
		{"webCache", func(id string) bool { _, ok := srv.sess.webCache.Load(id); return ok }},
		{"sems", func(id string) bool { _, ok := srv.sess.sems.Load(id); return ok }},
		{"permCache", func(id string) bool { return countKeys(&srv.sess.permCache, id+"\x00") > 0 }},
		{"toolCallWarned", func(id string) bool {
			srv.toolCallWarnedMu.Lock()
			defer srv.toolCallWarnedMu.Unlock()
			_, ok := srv.toolCallWarned[id+"\x00qwen3:14b"]
			return ok
		}},
	} {
		if tc.load(gone.ID) {
			t.Errorf("%s still holds the pruned session's entry", tc.name)
		}
		if !tc.load(kept.ID) {
			t.Errorf("%s dropped a live session's entry", tc.name)
		}
	}
}

func countKeys(m *sync.Map, prefix string) int {
	n := 0
	m.Range(func(k, _ any) bool {
		if s, ok := k.(string); ok && len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			n++
		}
		return true
	})
	return n
}
