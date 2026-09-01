package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/server"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	_ "modernc.org/sqlite"
)

// TestOpenCheckpointSnapshotterCapturesAcrossConnections is the P9
// regression: a subprocess-mode sub-agent runs in a whole separate process
// with its own ctx tree, so it can't see the parent's in-process Snapshotter.
// openCheckpointSnapshotter must reconstruct one from spec.SessionDBPath and
// spec.Config.CheckpointID that captures into the exact same checkpoint row —
// verified here by opening a second, independent *sql.DB connection to the
// same file, standing in for the worker's separate process.
func TestOpenCheckpointSnapshotterCapturesAcrossConnections(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.db")

	// Simulate the parent daemon: create a checkpoint before spawning.
	parentDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parentDB.Close()
	store, err := checkpoint.NewStore(parentDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// The workspace the checkpoint is bound to (P70.1): restore validates every
	// captured path against the root recorded on this row.
	workspace := t.TempDir()
	cp, err := store.Create(ctx, "sess-1", 0, "test turn", workspace)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the worker: a separate connection, reconstructing a
	// Snapshotter purely from the spec.
	spec := swarm.WorkerSpec{
		Config:        swarm.SpawnConfig{CheckpointID: cp.ID},
		SessionDBPath: dbPath,
	}
	snap, closeDB := openCheckpointSnapshotter(spec)
	if snap == nil {
		t.Fatal("expected a non-nil snapshotter")
	}
	defer closeDB()

	target := filepath.Join(workspace, "out.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap.Capture(target) // as write_file/edit_file do, before mutating
	if err := os.WriteFile(target, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore via the PARENT's own connection: proves the capture landed in
	// the same checkpoint row a genuinely separate process would have
	// written to, not just an in-memory value local to the worker.
	n, err := store.RestoreFiles(ctx, cp.ID)
	if err != nil {
		t.Fatalf("RestoreFiles: %v", err)
	}
	if n != 1 {
		t.Fatalf("RestoreFiles restored %d files, want 1", n)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Errorf("restored content = %q, want %q", data, "original")
	}
}

// TestOpenCheckpointSnapshotterNoOpWithoutSpec verifies a worker spawned
// outside any checkpointed turn (no CheckpointID/SessionDBPath in the spec —
// checkpointing disabled, or no in-flight Snapshotter on the parent) gets a
// nil snapshotter rather than erroring or opening a stray db connection.
func TestOpenCheckpointSnapshotterNoOpWithoutSpec(t *testing.T) {
	snap, closeDB := openCheckpointSnapshotter(swarm.WorkerSpec{})
	defer closeDB()
	if snap != nil {
		t.Error("expected a nil snapshotter when the spec carries no checkpoint id or db path")
	}
}

// envProbeCommand returns a shell command that dumps the process environment,
// in whichever shell syntax the current OS's shell tool uses.
func envProbeCommand() string {
	if runtime.GOOS == "windows" {
		return "Get-ChildItem Env: | ForEach-Object { \"$($_.Name)=$($_.Value)\" }"
	}
	return "env"
}

func execShellTool(t *testing.T, reg *tool.Registry, command string) string {
	t.Helper()
	tl, ok := reg.Get("shell")
	if !ok {
		t.Fatal("shell tool not registered")
	}
	input, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tl.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("shell tool execute: %v", err)
	}
	return res.Content
}

// TestExecuteWorkerSandboxHonorsConfiguredStripEnv is the P10.2 regression: a
// subprocess-mode sub-agent worker used to call builtin.Register with no
// Sandbox at all, so shell.go's own fallback (bare sandbox.NewLocalBackend())
// only stripped the hardcoded DefaultStripEnv names — any *extra* secret name
// an operator configured via sandbox.strip_env (e.g. an MCP token loaded from
// .aegis/.env) still leaked into a worker's shell calls, and a
// container/os-configured sandbox was never honored at all for the worker's
// shell tool. executeWorker now builds its registry with
// server.SelectSandbox(cfg.Sandbox, ...), which does honor both. This test
// exercises that same wiring path directly (bypassing executeWorker's
// config.Load()/provider setup, which needs a real environment) by comparing
// the pre-fix registration (no Sandbox) against the post-fix one (Sandbox
// from SelectSandbox) for the same configured extra strip-env name.
//
// The env var under test is one of sandbox.DefaultEnvAllow's names (GOPROXY),
// not an arbitrary one: since P81.26/FIND-26 inverted the default from a
// denylist to an allowlist, an arbitrary secret name never survives either
// registration's default filtering regardless of this wiring — which would
// make the "leaks without a wired sandbox" setup assertion below false and
// the test moot. GOPROXY is a name real operators put a credential-bearing
// module-proxy URL in and one this project's own toolchain needs by default
// (see CLAUDE.md's go build/test commands), so it stays allowlisted through
// on both paths — exactly the case sandbox.strip_env exists to let an
// operator override, and exactly the case this wiring gap silently ignored.
func TestExecuteWorkerSandboxHonorsConfiguredStripEnv(t *testing.T) {
	t.Setenv("GOPROXY", "https://user:sk-mcp-secret-value@proxy.internal/mod")

	cwd := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.SandboxConfig{StripEnv: []string{"GOPROXY"}}

	// Pre-fix behavior: no Sandbox wired into the registry at all.
	regNoSandbox := tool.NewRegistry()
	if err := builtin.Register(regNoSandbox, builtin.Options{Root: cwd}); err != nil {
		t.Fatal(err)
	}
	leaked := execShellTool(t, regNoSandbox, envProbeCommand())
	if !strings.Contains(leaked, "sk-mcp-secret-value") {
		t.Fatal("test setup invalid: expected the configured extra secret to leak without a wired sandbox (proves this scenario reproduces the pre-fix bug)")
	}

	// Post-fix behavior: the registry is built with server.SelectSandbox's
	// result, exactly as executeWorker now does.
	sb, _, _, err := server.SelectSandbox(cfg, cwd, logger)
	if err != nil {
		t.Fatal(err)
	}
	regWithSandbox := tool.NewRegistry()
	if err := builtin.Register(regWithSandbox, builtin.Options{Root: cwd, Sandbox: sb}); err != nil {
		t.Fatal(err)
	}
	out := execShellTool(t, regWithSandbox, envProbeCommand())
	if strings.Contains(out, "sk-mcp-secret-value") {
		t.Errorf("configured extra secret leaked into worker shell env:\n%s", out)
	}
}

// TestResolveWorkerCwdPrefersSpecWorkdir is the P25.8 regression for gap (a)'s
// subprocess-backend case: a worker process's own cwd is inherited from the
// daemon, not the spawning session, so without this a subprocess-mode
// teammate silently operated in the daemon root regardless of
// spec.Config.Workdir.
func TestResolveWorkerCwdPrefersSpecWorkdir(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	processCwd := t.TempDir()
	specWorkdir := t.TempDir()

	if got := resolveWorkerCwd(processCwd, specWorkdir, logger); got != specWorkdir {
		t.Errorf("resolveWorkerCwd = %q, want specWorkdir %q", got, specWorkdir)
	}
	if got := resolveWorkerCwd(processCwd, "", logger); got != processCwd {
		t.Errorf("resolveWorkerCwd with empty spec workdir = %q, want processCwd %q", got, processCwd)
	}
	nonexistent := filepath.Join(processCwd, "does-not-exist")
	if got := resolveWorkerCwd(processCwd, nonexistent, logger); got != processCwd {
		t.Errorf("resolveWorkerCwd with a nonexistent spec workdir = %q, want fallback to processCwd %q", got, processCwd)
	}
}
