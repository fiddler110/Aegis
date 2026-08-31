package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneOlderThanDropsCheckpointsAndTheirFileCopies covers P81.24's
// retention half. The assertion that matters is the second one: deleting the
// checkpoint row without its checkpoint_files rows would leave the verbatim
// file copies — the actual sensitive payload — orphaned in the database with
// nothing left that names them.
func TestPruneOlderThanDropsCheckpointsAndTheirFileCopies(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	f := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(f, []byte("api-key-material"), 0o644); err != nil {
		t.Fatal(err)
	}

	old, err := st.Create(ctx, "sess-old", 0, "old turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(old.ID).Capture(f)

	recent, err := st.Create(ctx, "sess-new", 0, "recent turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(recent.ID).Capture(f)

	// Backdate the first checkpoint rather than sleeping.
	if _, err := st.db.ExecContext(ctx, `UPDATE checkpoints SET created_at = ? WHERE id = ?`,
		time.Now().Add(-48*time.Hour).UnixMilli(), old.ID); err != nil {
		t.Fatal(err)
	}

	n, err := st.PruneOlderThan(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d checkpoints, want 1", n)
	}

	if _, err := st.Get(ctx, old.ID); err == nil {
		t.Error("the aged checkpoint survived the prune")
	}
	if _, err := st.Get(ctx, recent.ID); err != nil {
		t.Errorf("the recent checkpoint was pruned too: %v", err)
	}

	var orphans int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM checkpoint_files WHERE checkpoint_id = ?`, old.ID).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d captured file copies outlived their checkpoint row", orphans)
	}
}

// TestPruneOlderThanIsANoOpWhenNothingIsOld guards against a cutoff-comparison
// slip quietly emptying the store.
func TestPruneOlderThanIsANoOpWhenNothingIsOld(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.Create(ctx, "sess", 0, "turn", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	n, err := st.PruneOlderThan(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if n != 0 {
		t.Errorf("pruned %d checkpoints, want 0", n)
	}
}
