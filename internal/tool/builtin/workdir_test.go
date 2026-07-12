package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestEffectiveRootPrefersContextOverride is a direct unit test of the
// helper every workspace-confined tool uses (P25.1): a context carrying
// tool.WithWorkdir wins over the tool's own construction-time root, and an
// unset context falls back to that root unchanged.
func TestEffectiveRootPrefersContextOverride(t *testing.T) {
	ctx := tool.WithWorkdir(context.Background(), "/session/root")
	if got := effectiveRoot(ctx, "/daemon/default"); got != "/session/root" {
		t.Errorf("effectiveRoot with override = %q, want /session/root", got)
	}
	if got := effectiveRoot(context.Background(), "/daemon/default"); got != "/daemon/default" {
		t.Errorf("effectiveRoot without override = %q, want /daemon/default", got)
	}
}

// TestReadToolUsesContextWorkdirOverride proves the wiring end to end at the
// tool level (not just the helper in isolation): a readTool constructed with
// one root reads from a different directory when the call context carries a
// workdir override — the same mechanism the engine applies per session.
func TestReadToolUsesContextWorkdirOverride(t *testing.T) {
	fallback := t.TempDir()
	override := t.TempDir()
	if err := os.WriteFile(filepath.Join(override, "f.txt"), []byte("override content"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &readTool{root: fallback}
	ctx := tool.WithWorkdir(context.Background(), override)
	res, err := r.Execute(ctx, mustJSON(t, map[string]any{"path": "f.txt"}))
	if err != nil || res.IsError {
		t.Fatalf("read: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "override content") {
		t.Errorf("read from context-overridden root failed: %q", res.Content)
	}
}

// recordingBackend is a sandbox.Backend that only records the Dir it was
// asked to execute in, so shellTool tests can assert on ExecOpts.Dir without
// depending on platform-specific shell syntax.
type recordingBackend struct{ lastDir string }

func (b *recordingBackend) Name() string { return "recording" }
func (b *recordingBackend) Exec(_ context.Context, _ string, opts sandbox.ExecOpts) (string, error) {
	b.lastDir = opts.Dir
	return "", nil
}
func (b *recordingBackend) ExecStreaming(_ context.Context, _ string, opts sandbox.ExecOpts, _ func(string)) error {
	b.lastDir = opts.Dir
	return nil
}
func (b *recordingBackend) Close() error { return nil }

// TestShellToolForegroundUsesContextWorkdirOverride verifies the shell tool
// passes the context-overridden root as ExecOpts.Dir, not its own
// construction-time root (P25.1) — this is what makes a per-session workdir
// actually reach the sandbox backend for `shell` calls.
func TestShellToolForegroundUsesContextWorkdirOverride(t *testing.T) {
	fallback := t.TempDir()
	override := t.TempDir()
	rb := &recordingBackend{}
	sh := newShellTool(fallback, 30, nil, rb)

	ctx := tool.WithWorkdir(context.Background(), override)
	res, err := sh.Execute(ctx, mustJSON(t, map[string]any{"command": "echo hi"}))
	if err != nil || res.IsError {
		t.Fatalf("shell: %v %+v", err, res)
	}
	if rb.lastDir != override {
		t.Errorf("ExecOpts.Dir = %q, want context override %q", rb.lastDir, override)
	}
}

// TestShellToolBackgroundUsesContextWorkdirOverride is the background-job
// analogue: the resolved root must be captured before the job is handed off
// to the task manager's own goroutine/context, not read from the (possibly
// workdir-less) job context at execution time.
func TestShellToolBackgroundUsesContextWorkdirOverride(t *testing.T) {
	fallback := t.TempDir()
	override := t.TempDir()
	rb := &recordingBackend{}
	mgr := newTaskMgr(t)
	sh := newShellTool(fallback, 30, mgr, rb)

	ctx := tool.WithWorkdir(context.Background(), override)
	res, err := sh.Execute(ctx, mustJSON(t, map[string]any{"command": "echo hi", "background": true}))
	if err != nil || res.IsError {
		t.Fatalf("background shell: %v %+v", err, res)
	}
	id := extractID(t, res.Content)
	// mgr.Wait(id) is not used here: with an instant-return backend like
	// recordingBackend, the background goroutine can reach
	// Manager.finish's delete(m.live, id) before this goroutine calls Wait,
	// so Wait's live-map lookup misses and it falls through to a single
	// store.Get() that races finish's own store.Save — a pre-existing,
	// unrelated task-manager timing hazard this test would otherwise trip
	// over by chance. Poll instead, which is correct regardless of that
	// ordering.
	deadline := time.Now().Add(2 * time.Second)
	var done *task.Task
	for time.Now().Before(deadline) {
		got, err := mgr.Get(id)
		if err == nil && got.State != task.StateRunning {
			done = got
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if done == nil || done.State != task.StateDone {
		t.Fatalf("task did not reach StateDone in time: %+v", done)
	}
	if rb.lastDir != override {
		t.Errorf("background ExecOpts.Dir = %q, want context override %q", rb.lastDir, override)
	}
}
