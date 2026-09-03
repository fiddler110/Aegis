package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestExternalChangesSinceDetectsAnEditAfterTheTurn is P81.31/FIND-31's
// confirmation half. A file changed by something other than the turn's own
// tool calls, after RecordPostTurnState ran, must be reported.
func TestExternalChangesSinceDetectsAnEditAfterTheTurn(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := st.NewSnapshotter(cp.ID)
	snap.Capture(f) // pre-turn: "original"

	// The turn's own edit.
	if err := os.WriteFile(f, []byte("agent wrote this"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordPostTurnState(ctx, cp.ID); err != nil {
		t.Fatalf("RecordPostTurnState: %v", err)
	}

	// No external change yet.
	changed, err := st.ExternalChangesSince(ctx, cp.ID)
	if err != nil {
		t.Fatalf("ExternalChangesSince: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v before any external edit, want none", changed)
	}

	// Something else edits the file after the turn finished.
	if err := os.WriteFile(f, []byte("a reviewer's edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = st.ExternalChangesSince(ctx, cp.ID)
	if err != nil {
		t.Fatalf("ExternalChangesSince: %v", err)
	}
	if len(changed) != 1 || changed[0] != f {
		t.Fatalf("changed = %v, want [%s]", changed, f)
	}
}

// TestExternalChangesSinceDetectsDeletion covers a file the turn left in
// place (post-turn digest recorded) that something else then removes.
func TestExternalChangesSinceDetectsDeletion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(cp.ID).Capture(f)
	if err := st.RecordPostTurnState(ctx, cp.ID); err != nil {
		t.Fatalf("RecordPostTurnState: %v", err)
	}

	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	changed, err := st.ExternalChangesSince(ctx, cp.ID)
	if err != nil {
		t.Fatalf("ExternalChangesSince: %v", err)
	}
	if len(changed) != 1 || changed[0] != f {
		t.Fatalf("changed = %v, want [%s]", changed, f)
	}
}

// TestExternalChangesSinceSkipsCheckpointsWithNoBaseline guards the backward
// compatibility case: a checkpoint RecordPostTurnState was never called for
// (pre-P81.31 rows, or a turn that never reached the post-run bookkeeping)
// must not be reported as "changed" — there is nothing to compare against.
func TestExternalChangesSinceSkipsCheckpointsWithNoBaseline(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(cp.ID).Capture(f)
	// RecordPostTurnState deliberately not called.

	if err := os.WriteFile(f, []byte("anything"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := st.ExternalChangesSince(ctx, cp.ID)
	if err != nil {
		t.Fatalf("ExternalChangesSince: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v for a checkpoint with no recorded baseline, want none", changed)
	}
}

// TestEvictOldestOverCapKeepsTheNewestCheckpoint is P81.31/FIND-31's byte-cap
// half. With three checkpoints over budget, the oldest are evicted first and
// the most recently created one always survives, even if it alone still
// exceeds the cap.
func TestEvictOldestOverCapKeepsTheNewestCheckpoint(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	mk := func(name string, size int, age time.Duration) *Checkpoint {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		cp, err := st.Create(ctx, "s1", 0, name, dir)
		if err != nil {
			t.Fatal(err)
		}
		st.NewSnapshotter(cp.ID).Capture(f)
		if age > 0 {
			if _, err := st.db.ExecContext(ctx, `UPDATE checkpoints SET created_at = ? WHERE id = ?`,
				time.Now().Add(-age).UnixMilli(), cp.ID); err != nil {
				t.Fatal(err)
			}
		}
		return cp
	}

	oldest := mk("a.bin", 100, 3*time.Hour)
	middle := mk("b.bin", 100, 2*time.Hour)
	newest := mk("c.bin", 100, 0)

	// Cap under the total (300 bytes) but over one checkpoint's share, so
	// eviction must remove the two oldest and stop, keeping the newest.
	n, err := st.EvictOldestOverCap(ctx, "s1", 150)
	if err != nil {
		t.Fatalf("EvictOldestOverCap: %v", err)
	}
	if n != 2 {
		t.Fatalf("evicted %d checkpoints, want 2", n)
	}
	if _, err := st.Get(ctx, oldest.ID); err == nil {
		t.Error("oldest checkpoint survived eviction")
	}
	if _, err := st.Get(ctx, middle.ID); err == nil {
		t.Error("middle checkpoint survived eviction")
	}
	if _, err := st.Get(ctx, newest.ID); err != nil {
		t.Errorf("newest checkpoint was evicted: %v", err)
	}
}

// TestEvictOldestOverCapNoOpUnderCap guards against evicting anything when the
// session is already within budget.
func TestEvictOldestOverCapNoOpUnderCap(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	f := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(f, make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(cp.ID).Capture(f)

	n, err := st.EvictOldestOverCap(ctx, "s1", 1<<20)
	if err != nil {
		t.Fatalf("EvictOldestOverCap: %v", err)
	}
	if n != 0 {
		t.Fatalf("evicted %d checkpoints while under cap, want 0", n)
	}

	n, err = st.EvictOldestOverCap(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("EvictOldestOverCap with disabled cap: %v", err)
	}
	if n != 0 {
		t.Fatalf("evicted %d checkpoints with maxBytes<=0, want 0 (disabled)", n)
	}
}
