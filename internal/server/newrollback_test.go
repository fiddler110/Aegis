package server

import (
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

// TestNewClosesWhatItOpenedWhenItFails is the regression test for New's
// rollback.
//
// New is a sequence of wire* stages, several of which take a durable resource:
// three SQLite handles (sessions, knowledge, long-term memory), a spawned
// language-server process per `lsp:` entry, a spawned MCP server process per
// `mcp:` entry, a possibly-persistent workspace container, and the audit file.
// Each error path used to hand-write its own cleanup, and each was the cleanup
// that was correct when that stage was added — so a wireTools failure closed
// only the session store and a wireSwarm failure, the last stage, closed only
// the session store while stranding everything the seven stages before it had
// opened.
//
// The failure this measures against is the P25.2 posture refusal
// (auto_approve_exec over an unsandboxed local backend), because it is
// deterministic, needs no fault injection, and — importantly — trips at
// wireSecurityWarnings, which is downstream of the session store, the sandbox,
// the cron store, the language servers and both recall stores. A leak is
// therefore several descriptors per call (SQLite opens the db, its WAL and its
// shared-memory file separately), which is far enough above the runtime's own
// jitter to be a real signal rather than a threshold someone tuned.
//
// Counting descriptors rather than asserting on the rollback slice is
// deliberate: the slice is the mechanism, and a test that reads it would keep
// passing if a future stage acquired something and never registered an undo.
// What must stay true is the property — a New that returns an error leaves the
// process holding nothing.
func TestNewClosesWhatItOpenedWhenItFails(t *testing.T) {
	fdDir := procFDDir()
	if fdDir == "" {
		t.Skipf("no open-descriptor listing on %s", runtime.GOOS)
	}

	failingNew := func(t *testing.T) {
		t.Helper()
		cfg := &config.Config{
			DataDir:  t.TempDir(),
			Provider: config.ProviderConfig{Model: "test", MaxTokens: 100},
			// P25.2: auto-approved execution over the (default, zero-value)
			// local backend is the one posture New refuses outright.
			Permission: config.PermissionConfig{Mode: "auto", AutoApproveExec: true},
		}
		srv, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil {
			closeTestServerStores(srv)
			t.Fatal("expected New to refuse auto_approve_exec over a local sandbox; got nil error")
		}
		if srv != nil {
			t.Fatalf("New returned a non-nil server alongside its error: %#v", srv)
		}
		if !strings.Contains(err.Error(), "allow_unsandboxed_auto_exec") {
			t.Fatalf("New failed for the wrong reason — this test measures the P25.2 path: %v", err)
		}
	}

	// One warm-up call first. The first New in a process pays one-time costs
	// that legitimately hold descriptors afterwards (embedded skill
	// materialization, the model-capability cache), and counting those as a
	// leak would make this test fail for a reason that has nothing to do with
	// rollback.
	failingNew(t)

	before := countOpenFDs(t, fdDir)
	const iterations = 5
	for i := 0; i < iterations; i++ {
		failingNew(t)
	}
	after := countOpenFDs(t, fdDir)

	// Tolerance covers runtime jitter — a GC-triggered file, a lazily opened
	// timer fd — not a per-iteration leak. Before the rollback existed this
	// path stranded the session, knowledge and long-memory handles on every
	// call, so the growth being bounded here is the whole assertion.
	const tolerance = 4
	if growth := after - before; growth > tolerance {
		t.Errorf("New leaked descriptors on its error path: %d open before %d failing calls, %d after (growth %d, tolerance %d).\n"+
			"A stage acquired something and did not register its undo with onFailure.",
			before, iterations, after, growth, tolerance)
	}
}

// procFDDir names the directory listing this process's open descriptors, or ""
// where the platform has none (Windows).
func procFDDir() string {
	switch runtime.GOOS {
	case "linux":
		return "/proc/self/fd"
	case "darwin":
		return "/dev/fd"
	default:
		return ""
	}
}

func countOpenFDs(t *testing.T, dir string) int {
	t.Helper()
	// Read the directory rather than stat-ing a range: the listing itself
	// consumes a descriptor, but it does so identically on both calls, so it
	// cancels out of the comparison.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return len(entries)
}
