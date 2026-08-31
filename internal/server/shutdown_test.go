package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
)

// newShutdownTestServer builds a real Server through New (not newWithDeps), so
// the test sees every handle the production constructor actually opens.
func newShutdownTestServer(t *testing.T, dir string) *Server {
	t.Helper()
	cfg := &config.Config{
		DataDir:    dir,
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

// TestCloseReleasesEveryHandleNewOpened is the C2 regression test. The first
// live_workflow run built daemons through New + httptest.Server — never
// ListenAndServe — and every subtest then failed to delete its data directory
// because audit.jsonl, longmem.db and knowledge.db were all still open. There
// was no exported way to let go of them: teardown lived entirely inside
// ListenAndServe.
//
// Removing the data directory is the assertion because it is the one that
// actually fails when a handle is left open. It is only a real test on
// Windows, where an open handle makes the file undeletable; POSIX unlinks a
// busy file happily, so there the test still exercises Close (and its
// idempotence) but cannot detect a leak. That asymmetry is why this pins the
// behavior on the platform the bug appeared on rather than being skipped
// outright.
func TestCloseReleasesEveryHandleNewOpened(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	srv := newShutdownTestServer(t, dir)

	if err := srv.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent: ListenAndServe defers Close, and an embedder may also call
	// it directly. Double-closing a database must not panic or error.
	if err := srv.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("data dir still held open after Close (%s): %v", runtime.GOOS, err)
	}
}

// TestCloseToleratesANilContext pins the documented nil-ctx contract, since
// the natural call from a test or an embedder is Close(nil) as often as not.
func TestCloseToleratesANilContext(t *testing.T) {
	srv := newShutdownTestServer(t, t.TempDir())
	if err := srv.Close(nil); err != nil { //nolint:staticcheck // nil ctx is the documented contract
		t.Fatalf("Close(nil): %v", err)
	}
}

// TestListenAndServeTearsDownOnEveryExit pins the halves of the teardown that
// the ctx.Done path always had and no other path did. ListenAndServe can leave
// four ways: a refusal to start (no auth token), a refused listen address, a
// serve error arriving on errCh, and ctx cancellation. Teardown used to live
// inside the ctx.Done case only, so a daemon that failed to bind returned
// without stopping cron, the swarm, the task manager, the sandbox or the
// language servers, and without closing a single store. All four exits now run
// one deferred Close.
//
// The observable consequence asserted here is that the data directory can be
// deleted afterwards, which is only a true leak detector on Windows (POSIX
// unlinks a file with a live handle without complaint) — see
// TestCloseReleasesEveryHandleNewOpened.
func TestListenAndServeTearsDownOnEveryExit(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*Server)
	}{
		{
			// Port 70000 is out of range, so the address passes
			// validateListenAddr's loopback check and then fails in net.Listen
			// — the errCh exit, reached without depending on what a given OS
			// does when two processes bind one port.
			name:    "serve error",
			arrange: func(s *Server) { setAddr(s, "127.0.0.1:70000") },
		},
		{
			name: "refused listen address",
			arrange: func(s *Server) {
				setAddr(s, "0.0.0.0:4127")
				s.cfg.Server.AllowRemote = false
			},
		},
		{
			name:    "missing auth token",
			arrange: func(s *Server) { s.authToken = "" },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "data")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			srv := newShutdownTestServer(t, dir)
			srv.authToken = "test-token"
			setAddr(srv, "127.0.0.1:0")
			tc.arrange(srv)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.ListenAndServe(ctx); err == nil {
				t.Fatal("ListenAndServe: want an error, got nil")
			}
			if ctx.Err() != nil {
				t.Fatal("ListenAndServe returned via ctx timeout, not via the failure path this case means to exercise")
			}
			if err := os.RemoveAll(dir); err != nil {
				t.Fatalf("data dir still held open after ListenAndServe failed (%s): %v", runtime.GOOS, err)
			}
		})
	}
}

// setAddr keeps the two copies of the listen address in step: validateListenAddr
// reads cfg.Server.Addr, but the socket is opened from http.Server.Addr, which
// newWithDeps snapshots at construction.
func setAddr(s *Server, addr string) {
	s.cfg.Server.Addr = addr
	s.http.Addr = addr
}
