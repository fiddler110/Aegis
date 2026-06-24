package task

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	st, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NewManager(st, nil)
}

func TestTaskCompletes(t *testing.T) {
	m := newTestManager(t)
	task, err := m.Start(Spec{Kind: "shell", Title: "echo"}, func(ctx context.Context, emit func(string)) (string, error) {
		emit("hello ")
		emit("world")
		return "", nil
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if task.State != StateRunning {
		t.Errorf("initial state = %q, want running", task.State)
	}

	done, ok := m.Wait(task.ID)
	if !ok {
		t.Fatal("Wait: task not found")
	}
	if done.State != StateDone {
		t.Errorf("state = %q, want done", done.State)
	}
	if done.Output != "hello world" {
		t.Errorf("output = %q, want %q", done.Output, "hello world")
	}
}

func TestTaskFailure(t *testing.T) {
	m := newTestManager(t)
	task, _ := m.Start(Spec{Kind: "shell"}, func(ctx context.Context, emit func(string)) (string, error) {
		return "", context.DeadlineExceeded
	})
	done, _ := m.Wait(task.ID)
	if done.State != StateFailed {
		t.Errorf("state = %q, want failed", done.State)
	}
	if done.Error == "" {
		t.Error("expected error message")
	}
}

func TestTaskPanicRecovered(t *testing.T) {
	m := newTestManager(t)
	task, _ := m.Start(Spec{}, func(ctx context.Context, emit func(string)) (string, error) {
		panic("boom")
	})
	done, _ := m.Wait(task.ID)
	if done.State != StateFailed {
		t.Errorf("state = %q, want failed", done.State)
	}
}

func TestTaskStop(t *testing.T) {
	m := newTestManager(t)
	started := make(chan struct{})
	task, _ := m.Start(Spec{Kind: "shell"}, func(ctx context.Context, emit func(string)) (string, error) {
		close(started)
		<-ctx.Done() // block until stopped
		return "", ctx.Err()
	})
	<-started
	if err := m.Stop(task.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	done, _ := m.Wait(task.ID)
	if done.State != StateStopped {
		t.Errorf("state = %q, want stopped", done.State)
	}
}

func TestStopUnknown(t *testing.T) {
	m := newTestManager(t)
	if err := m.Stop("nope"); err != ErrNotFound {
		t.Errorf("Stop unknown = %v, want ErrNotFound", err)
	}
}

func TestStopFinishedIsNoop(t *testing.T) {
	m := newTestManager(t)
	task, _ := m.Start(Spec{}, func(ctx context.Context, emit func(string)) (string, error) {
		return "ok", nil
	})
	m.Wait(task.ID)
	if err := m.Stop(task.ID); err != nil {
		t.Errorf("Stop finished = %v, want nil", err)
	}
}

func TestLivePollAndList(t *testing.T) {
	m := newTestManager(t)
	release := make(chan struct{})
	emitted := make(chan struct{})
	task, _ := m.Start(Spec{SessionID: "s1", Kind: "shell"}, func(ctx context.Context, emit func(string)) (string, error) {
		emit("partial")
		close(emitted)
		<-release
		return "", nil
	})
	<-emitted

	// A running task exposes its streamed output via Get.
	got, err := m.Get(task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Output != "partial" {
		t.Errorf("live output = %q, want %q", got.Output, "partial")
	}

	// List scoped to the session finds it.
	list, err := m.List("s1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != task.ID {
		t.Fatalf("List(s1) = %+v, want 1 task", list)
	}
	if list2, _ := m.List("other"); len(list2) != 0 {
		t.Errorf("List(other) = %d, want 0", len(list2))
	}

	close(release)
	m.Wait(task.ID)
}

func TestSetTitle(t *testing.T) {
	m := newTestManager(t)
	task, _ := m.Start(Spec{Title: "old"}, func(ctx context.Context, emit func(string)) (string, error) {
		return "", nil
	})
	m.Wait(task.ID)
	if err := m.SetTitle(task.ID, "new"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	got, _ := m.Get(task.ID)
	if got.Title != "new" {
		t.Errorf("title = %q, want new", got.Title)
	}
}

func TestShutdownCancelsLive(t *testing.T) {
	m := newTestManager(t)
	var wg sync.WaitGroup
	wg.Add(1)
	task, _ := m.Start(Spec{}, func(ctx context.Context, emit func(string)) (string, error) {
		defer wg.Done()
		<-ctx.Done()
		return "", ctx.Err()
	})
	m.Shutdown(context.Background())
	wg.Wait()
	got, _ := m.Get(task.ID)
	if got.State != StateStopped {
		t.Errorf("state after shutdown = %q, want stopped", got.State)
	}
}

func TestPersistenceSurvivesNewManager(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	st, _ := NewStore(db)

	m1 := NewManager(st, nil)
	task, _ := m1.Start(Spec{Kind: "shell", Title: "job"}, func(ctx context.Context, emit func(string)) (string, error) {
		emit("result")
		return "", nil
	})
	m1.Wait(task.ID)

	// A fresh manager over the same store still sees the finished task.
	m2 := NewManager(st, nil)
	got, err := m2.Get(task.ID)
	if err != nil {
		t.Fatalf("Get from new manager: %v", err)
	}
	if got.State != StateDone || got.Output != "result" {
		t.Errorf("persisted task = %+v", got)
	}
}
