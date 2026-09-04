package server

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/repomap"
)

// repoMapServer returns a Server rooted at a freshly indexed temp workspace,
// wired the way New leaves it: the block loaded once and the staleness clock
// stamped.
func repoMapServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	writeRepoFile(t, root, "alpha.go", "package p\n\nfunc AlphaSymbol() {}\n")

	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := repomap.Build(root, repoMapOptions(cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := m.Save(filepath.Join(root, ".aegis", "repomap.json")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s := &Server{cfg: cfg, logger: logger, workspace: root}
	s.repoMap = loadRepoMap(root, repoMapOptions(cfg), logger)
	s.repoMapCheckedAt = time.Now()
	if !strings.Contains(s.repoMap, "alpha.go") {
		t.Fatalf("indexed block does not mention alpha.go: %q", s.repoMap)
	}
	return s, root
}

func writeRepoFile(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRepoMapForRefreshesWhenTheRepositoryChanges is P66.20/PERF-04: the
// daemon loaded the map once at construction and injected that same string for
// its whole lifetime, so a long-lived daemon described the repository as it
// looked at startup — including to the agent whose own writes were the reason
// it had changed. loadRepoMap already fingerprints and rebuilds; it was simply
// never called a second time.
func TestRepoMapForRefreshesWhenTheRepositoryChanges(t *testing.T) {
	s, root := repoMapServer(t)

	writeRepoFile(t, root, "beta.go", "package p\n\nfunc BetaSymbol() {}\n")

	// Inside the re-check interval the cached block still stands: the whole
	// point of the interval is that a burst of turns pays for one walk.
	if got := s.repoMapFor(root); strings.Contains(got, "beta.go") {
		t.Error("re-checked inside repoMapRecheckInterval; the interval is not bounding the cost")
	}

	s.repoMapMu.Lock()
	s.repoMapCheckedAt = time.Now().Add(-2 * repoMapRecheckInterval)
	s.repoMapMu.Unlock()

	got := s.repoMapFor(root)
	if !strings.Contains(got, "beta.go") {
		t.Errorf("repo map still stale after the re-check interval elapsed: %q", got)
	}
	if !strings.Contains(got, "alpha.go") {
		t.Errorf("refresh lost the pre-existing entry: %q", got)
	}
	// "" and s.workspace name the same root and must agree.
	if s.repoMapFor("") != got {
		t.Error(`repoMapFor("") disagrees with repoMapFor(s.workspace)`)
	}
}

// TestRepoMapIndexStillPublishesImmediately guards the path the fix above must
// not regress: POST /repomap/index (P14.3, the TUI's /index) rebuilds and
// publishes synchronously, and the staleness re-check must layer on top of that
// rather than replace or delay it.
func TestRepoMapIndexStillPublishesImmediately(t *testing.T) {
	s, root := repoMapServer(t)

	writeRepoFile(t, root, "gamma.go", "package p\n\nfunc GammaSymbol() {}\n")

	// Deliberately still inside the re-check interval: /index must not wait for it.
	w := httptest.NewRecorder()
	s.handleRepoMapIndex(w, httptest.NewRequest("POST", "/repomap/index", nil))
	if w.Code != 200 {
		t.Fatalf("POST /repomap/index = %d, body %s", w.Code, w.Body.String())
	}
	if got := s.repoMapFor(root); !strings.Contains(got, "gamma.go") {
		t.Errorf("explicit /index did not publish immediately: %q", got)
	}
}

// TestRepoMapForSecondaryWorkdirRefreshes covers the other half of PERF-04: a
// session on its own Workdir (P25.9) reads through the rootCache, which had no
// invalidation of any kind — its entry was created on the session's first
// prompt build and never reconsidered.
func TestRepoMapForSecondaryWorkdirRefreshes(t *testing.T) {
	s, _ := repoMapServer(t)

	other := t.TempDir()
	writeRepoFile(t, other, "delta.go", "package q\n\nfunc DeltaSymbol() {}\n")
	m, err := repomap.Build(other, repoMapOptions(s.cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := m.Save(filepath.Join(other, ".aegis", "repomap.json")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := s.repoMapFor(other); !strings.Contains(got, "delta.go") {
		t.Fatalf("secondary root's first load = %q, want it to mention delta.go", got)
	}

	writeRepoFile(t, other, "epsilon.go", "package q\n\nfunc EpsilonSymbol() {}\n")
	if got := s.repoMapFor(other); strings.Contains(got, "epsilon.go") {
		t.Error("secondary root re-checked inside repoMapRecheckInterval")
	}

	e, err := s.repoMaps.getOrCreate(other, func() (*repoMapEntry, error) { return &repoMapEntry{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.checkedAt = time.Now().Add(-2 * repoMapRecheckInterval)
	e.mu.Unlock()

	if got := s.repoMapFor(other); !strings.Contains(got, "epsilon.go") {
		t.Errorf("secondary root still stale after the interval elapsed: %q", got)
	}
	// The primary workspace's own block must be untouched by any of this.
	if !strings.Contains(s.repoMapFor(s.workspace), "alpha.go") {
		t.Error("a secondary root's refresh disturbed the primary workspace's block")
	}
}

// TestRepoMapForStaysEmptyForAnUnindexedWorkspace keeps the map opt-in: a
// workspace nobody has ever run `aegis index` against injects nothing, and the
// re-check must not start synthesizing one.
func TestRepoMapForStaysEmptyForAnUnindexedWorkspace(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "zeta.go", "package p\n\nfunc ZetaSymbol() {}\n")
	s := &Server{
		cfg:       &config.Config{},
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspace: root,
	}
	if got := s.repoMapFor(root); got != "" {
		t.Errorf("unindexed workspace injected %q, want the empty string", got)
	}
}
