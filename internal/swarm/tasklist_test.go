package swarm

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTaskListAddListClaimComplete(t *testing.T) {
	ctx := context.Background()
	tl, err := NewTaskList(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}

	id1, err := tl.Add(ctx, "alpha", "first task")
	if err != nil {
		t.Fatal(err)
	}
	tl.Add(ctx, "alpha", "second task")
	tl.Add(ctx, "beta", "other team")

	list, _ := tl.List(ctx, "alpha")
	if len(list) != 2 {
		t.Fatalf("expected 2 alpha tasks, got %d", len(list))
	}

	claimed, err := tl.Claim(ctx, "alpha", "agent-1")
	if err != nil || claimed == nil {
		t.Fatalf("claim failed: %v", err)
	}
	if claimed.ID != id1 || claimed.Owner != "agent-1" || claimed.Status != TaskClaimed {
		t.Fatalf("unexpected claim: %+v", claimed)
	}

	if err := tl.Complete(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if err := tl.Complete(ctx, 9999); err == nil {
		t.Error("expected error completing missing task")
	}
}

// TestClaimIsExclusive verifies two concurrent claimers never get the same task.
func TestClaimIsExclusive(t *testing.T) {
	ctx := context.Background()
	tl, err := NewTaskList(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	const n = 20
	for i := 0; i < n; i++ {
		tl.Add(ctx, "t", "task")
	}

	var mu sync.Mutex
	seen := map[int64]bool{}
	var wg sync.WaitGroup
	dup := false
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk, err := tl.Claim(ctx, "t", "worker")
			if err != nil || tk == nil {
				return
			}
			mu.Lock()
			if seen[tk.ID] {
				dup = true
			}
			seen[tk.ID] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if dup {
		t.Error("a task was claimed by more than one worker")
	}
}
