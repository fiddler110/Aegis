package cron

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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
		Workdir: "/some/session/root",
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
	if got.Workdir != "/some/session/root" {
		t.Errorf("workdir = %q, want %q", got.Workdir, "/some/session/root")
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

	j, err := sched.Create(ctx, "@hourly", "echo hello", "hourly echo", false, "", false)
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

// TestSchedulerCreateNotifyRoundTrips pins P58.1's per-job opt-in through the
// store: Toggle and the scheduler's own reloads write the whole row back, so a
// field that scanned but never persisted would silently reset the flag on the
// first toggle rather than failing loudly.
func TestSchedulerCreateNotifyRoundTrips(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(store, func(j Job) {}, nil)
	ctx := context.Background()

	quiet, err := sched.Create(ctx, "@daily", "echo quiet", "quiet", false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	loud, err := sched.Create(ctx, "@daily", "echo loud", "loud", false, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if quiet.Notify {
		t.Error("default job must not opt into notification")
	}
	if !loud.Notify {
		t.Error("Create(notify=true) did not set Notify")
	}

	got, err := store.Get(ctx, loud.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Notify {
		t.Error("Notify did not survive the store round-trip")
	}

	// Toggle rewrites the row; the unrelated Notify flag must survive it.
	if _, err := sched.Toggle(ctx, loud.ID); err != nil {
		t.Fatal(err)
	}
	got, err = store.Get(ctx, loud.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Notify {
		t.Error("Notify was lost when Toggle rewrote the job")
	}
}

func TestSchedulerCreateBadSchedule(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(store, func(j Job) {}, nil)
	if _, err := sched.Create(context.Background(), "not a cron", "echo", "", false, "", false); err == nil {
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

	j, err := sched.Create(ctx, "* * * * *", "echo tick", "all-minutes", false, "", false)
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

func TestRecordAndListRuns(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t1 := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	if err := store.RecordRun(ctx, "job-a", t1, "ok", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRun(ctx, "job-a", t2, "error", "boom"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRun(ctx, "job-b", t1, "blocked", "denied"); err != nil {
		t.Fatal(err)
	}

	// All runs, most-recent-first.
	all, err := store.ListRuns(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(all))
	}
	if all[0].Status != "error" || all[0].JobID != "job-a" {
		t.Errorf("expected most recent run first (job-a/error), got %+v", all[0])
	}

	// Filtered by job.
	jobARuns, err := store.ListRuns(ctx, "job-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobARuns) != 2 {
		t.Fatalf("expected 2 runs for job-a, got %d", len(jobARuns))
	}
	for _, r := range jobARuns {
		if r.JobID != "job-a" {
			t.Errorf("expected only job-a runs, got %+v", r)
		}
	}

	// Limit.
	limited, err := store.ListRuns(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected 1 run with limit=1, got %d", len(limited))
	}
	if limited[0].Status != "error" {
		t.Errorf("expected the most recent run under limit=1, got %+v", limited[0])
	}
}

func TestRecordRunTruncatesOversizedOutput(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	huge := strings.Repeat("x", maxRunOutputBytes+5000)
	if err := store.RecordRun(ctx, "job-a", time.Now(), "ok", huge); err != nil {
		t.Fatal(err)
	}

	runs, err := store.ListRuns(ctx, "job-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	got := runs[0].Output
	if len(got) >= len(huge) {
		t.Errorf("expected output to be truncated, got length %d (input was %d)", len(got), len(huge))
	}
	if !strings.HasSuffix(got, "[...truncated]") {
		t.Errorf("expected truncation marker suffix, got tail: %q", got[max(0, len(got)-30):])
	}
	if !strings.HasPrefix(got, strings.Repeat("x", 10)) {
		t.Errorf("expected truncated output to retain the head of the original content")
	}
}

func TestSchedulerHistory(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(store, func(j Job) {}, nil)
	ctx := context.Background()

	if err := store.RecordRun(ctx, "job-a", time.Now(), "ok", "output"); err != nil {
		t.Fatal(err)
	}

	runs, err := sched.History(ctx, "job-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "ok" {
		t.Errorf("expected 1 ok run for job-a, got %+v", runs)
	}

	// Unknown job id yields no rows, not an error.
	none, err := sched.History(ctx, "no-such-job", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("expected no runs for unknown job, got %d", len(none))
	}
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

	j, err := sched.Create(ctx, "* * * * *", "echo", "", false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	sched.Toggle(ctx, j.ID) // disable

	sched.tick(time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))
	if fired {
		t.Error("expected disabled job not to fire")
	}
}
