package cli

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/swarm"
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
	cp, err := store.Create(ctx, "sess-1", 0, "test turn")
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

	target := filepath.Join(t.TempDir(), "out.txt")
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
