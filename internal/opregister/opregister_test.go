package opregister

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opreg.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	st, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st, path
}

func TestMarkStartedThenFinishedLeavesNoRow(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	if err := st.MarkStarted(ctx, "sess-1", "tu-1", "write_file", json.RawMessage(`{"path":"a.txt"}`)); err != nil {
		t.Fatalf("MarkStarted: %v", err)
	}
	pending, err := st.Pending(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ToolUseID != "tu-1" || pending[0].ToolName != "write_file" {
		t.Fatalf("Pending = %+v, want one row for tu-1", pending)
	}
	if err := st.MarkFinished(ctx, "sess-1", "tu-1"); err != nil {
		t.Fatalf("MarkFinished: %v", err)
	}
	pending, err = st.Pending(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Pending after finish: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("Pending after finish = %+v, want none", pending)
	}
}

func TestPendingIsScopedPerSession(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	if err := st.MarkStarted(ctx, "sess-1", "tu-1", "shell", nil); err != nil {
		t.Fatalf("MarkStarted sess-1: %v", err)
	}
	if err := st.MarkStarted(ctx, "sess-2", "tu-2", "shell", nil); err != nil {
		t.Fatalf("MarkStarted sess-2: %v", err)
	}
	pending, err := st.Pending(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ToolUseID != "tu-1" {
		t.Fatalf("Pending(sess-1) = %+v, want only tu-1", pending)
	}
}

// TestSurvivesProcessRestart is the property this package exists for: a row
// written by one *sql.DB handle (simulating the process that died mid-call)
// is visible to a completely separate handle opened later against the same
// file (simulating the next process's recovery pass).
func TestSurvivesProcessRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()

	db1, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	db1.SetMaxOpenConns(1)
	st1, err := NewStore(db1)
	if err != nil {
		t.Fatalf("NewStore db1: %v", err)
	}
	if err := st1.MarkStarted(ctx, "sess-1", "tu-1", "threat_model_scaffold", json.RawMessage(`{"framework":"stride"}`)); err != nil {
		t.Fatalf("MarkStarted: %v", err)
	}
	// No MarkFinished — the process "dies" here.
	db1.Close()

	db2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	db2.SetMaxOpenConns(1)
	t.Cleanup(func() { db2.Close() })
	st2, err := NewStore(db2)
	if err != nil {
		t.Fatalf("NewStore db2: %v", err)
	}
	pending, err := st2.Pending(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Pending on db2: %v", err)
	}
	if len(pending) != 1 || pending[0].ToolUseID != "tu-1" || pending[0].ToolName != "threat_model_scaffold" {
		t.Fatalf("Pending on db2 = %+v, want the row st1 wrote before dying", pending)
	}
}
