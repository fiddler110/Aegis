package cron

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestParseAndMatches(t *testing.T) {
	tests := []struct {
		expr   string
		time   time.Time
		expect bool
	}{
		{"* * * * *", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"0 * * * *", time.Date(2025, 1, 1, 3, 0, 0, 0, time.UTC), true},
		{"0 * * * *", time.Date(2025, 1, 1, 3, 5, 0, 0, time.UTC), false},
		{"30 9 * * 1-5", time.Date(2025, 1, 6, 9, 30, 0, 0, time.UTC), true},  // Monday
		{"30 9 * * 1-5", time.Date(2025, 1, 5, 9, 30, 0, 0, time.UTC), false}, // Sunday
		{"0 0 1 * *", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), true},
		{"0 0 1 * *", time.Date(2025, 6, 2, 0, 0, 0, 0, time.UTC), false},
		{"*/15 * * * *", time.Date(2025, 1, 1, 0, 45, 0, 0, time.UTC), true},
		{"*/15 * * * *", time.Date(2025, 1, 1, 0, 7, 0, 0, time.UTC), false},
	}
	for _, tc := range tests {
		s, err := Parse(tc.expr)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", tc.expr, err)
		}
		if got := s.Matches(tc.time); got != tc.expect {
			t.Errorf("Parse(%q).Matches(%v) = %v, want %v", tc.expr, tc.time, got, tc.expect)
		}
	}
}

func TestParseMacros(t *testing.T) {
	for _, macro := range []string{"@hourly", "@daily", "@weekly", "@monthly"} {
		if _, err := Parse(macro); err != nil {
			t.Errorf("Parse(%q) error: %v", macro, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{"", "* *", "99 * * * *", "* * * * 8", "*/0 * * * *"}
	for _, expr := range bad {
		if _, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q) expected error, got nil", expr)
		}
	}
}

func TestDomDowOrRule(t *testing.T) {
	// Both dom and dow restricted: either match should succeed.
	s, err := Parse("0 0 15 * 0")
	if err != nil {
		t.Fatal(err)
	}
	// 2025-06-15 is a Sunday — both dom=15 and dow=0 match
	if !s.Matches(time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Error("expected match when both dom and dow match")
	}
	// 2025-06-16 is Monday (dow=1, dom=16) — neither matches
	if s.Matches(time.Date(2025, 6, 16, 0, 0, 0, 0, time.UTC)) {
		t.Error("expected no match when neither dom nor dow matches")
	}
	// 2025-06-22 is Sunday (dow=0, dom=22) — dow matches
	if !s.Matches(time.Date(2025, 6, 22, 0, 0, 0, 0, time.UTC)) {
		t.Error("expected match when only dow matches (OR rule)")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	j := &Job{
		ID: "j1", Schedule: "* * * * *", Command: "echo hi",
		Title: "test", Enabled: true, Created: time.Now().Truncate(time.Millisecond),
	}
	if err := store.Save(ctx, j); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, "j1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "echo hi" || !got.Enabled {
		t.Errorf("unexpected job: %+v", got)
	}

	jobs, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	if err := store.Delete(ctx, "j1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "j1"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSchedulerCreateAndToggle(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(store, func(j Job) {}, nil)
	ctx := context.Background()

	j, err := sched.Create(ctx, "@hourly", "echo hello", "hourly echo")
	if err != nil {
		t.Fatal(err)
	}
	if !j.Enabled {
		t.Error("expected enabled")
	}

	nowEnabled, err := sched.Toggle(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nowEnabled {
		t.Error("expected disabled after toggle")
	}

	nowEnabled, err = sched.Toggle(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !nowEnabled {
		t.Error("expected enabled after second toggle")
	}
}

func TestSchedulerCreateBadSchedule(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(store, func(j Job) {}, nil)
	if _, err := sched.Create(context.Background(), "not a cron", "echo", ""); err == nil {
		t.Error("expected error for bad schedule")
	}
}

func TestTickFiresAndIdempotent(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var fired []string
	run := func(j Job) {
		mu.Lock()
		fired = append(fired, j.ID)
		mu.Unlock()
	}

	sched := NewScheduler(store, run, nil)
	ctx := context.Background()

	j, err := sched.Create(ctx, "* * * * *", "echo tick", "all-minutes")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	sched.tick(now)

	mu.Lock()
	if len(fired) != 1 || fired[0] != j.ID {
		t.Errorf("expected exactly 1 firing, got %v", fired)
	}
	mu.Unlock()

	// Second tick at the same minute should be idempotent.
	sched.tick(now)
	mu.Lock()
	if len(fired) != 1 {
		t.Errorf("expected idempotent second tick, got %d firings", len(fired))
	}
	mu.Unlock()

	// Next minute should fire again.
	sched.tick(now.Add(time.Minute))
	mu.Lock()
	if len(fired) != 2 {
		t.Errorf("expected 2 firings total, got %d", len(fired))
	}
	mu.Unlock()
}

func TestTickSkipsDisabled(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	fired := false
	sched := NewScheduler(store, func(j Job) { fired = true }, nil)
	ctx := context.Background()

	j, err := sched.Create(ctx, "* * * * *", "echo", "")
	if err != nil {
		t.Fatal(err)
	}
	sched.Toggle(ctx, j.ID) // disable

	sched.tick(time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))
	if fired {
		t.Error("expected disabled job not to fire")
	}
}
