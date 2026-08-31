package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// countBGEvents is the raw row count for one session, bypassing ListBGEvents so
// a test can see rows a prune should have removed.
func countBGEvents(t *testing.T, st *Store, sessionID string) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM bg_events WHERE session_id = ?`, sessionID).Scan(&n); err != nil {
		t.Fatalf("count bg_events: %v", err)
	}
	return n
}

// TestBGEventsBoundedOnADefaultInstall is the P66.9 item proper: bg_events used
// to be pruned only by whole-session delete, and that pruner is gated on
// Cleanup.SessionTTLDays, which has no default — so an install that configures
// nothing grew the table for the life of the install. This store configures
// nothing (no TTL, no retention override, no cleanup call) and must still bound
// the table.
func TestBGEventsBoundedOnADefaultInstall(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess, err := st.Create(ctx, "", "", "build", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	total := DefaultBGEventRetention + bgPruneInterval + 50
	for i := 0; i < total; i++ {
		if err := st.AppendBGEvent(ctx, sess.ID, fmt.Sprintf(`{"kind":"text","text":"%d"}`, i)); err != nil {
			t.Fatalf("AppendBGEvent %d: %v", i, err)
		}
	}

	got := countBGEvents(t, st, sess.ID)
	// The sweep is periodic, so the ceiling is the bound plus at most one
	// sweep interval of appends since the last sweep.
	if max := DefaultBGEventRetention + bgPruneInterval; got > max {
		t.Errorf("bg_events grew to %d rows after %d appends on a default install; want <= %d", got, total, max)
	}
	if got == total {
		t.Errorf("nothing was pruned at all: %d rows for %d appends", got, total)
	}

	// The newest event must survive — bounding the table must not cost the
	// tail a reattaching client is actually trying to catch up on.
	events, err := st.ListBGEvents(ctx, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if want := fmt.Sprintf(`"text":"%d"`, total-1); !strings.Contains(last.Data, want) {
		t.Errorf("newest surviving event = %q, want one containing %q", last.Data, want)
	}
	// And what was dropped must be the oldest, contiguously: the survivors are
	// a suffix of what was appended.
	first := events[0]
	if strings.Contains(first.Data, `"text":"0"`) {
		t.Errorf("oldest event survived a %d-append run: %q", total, first.Data)
	}
}

// TestBGEventRetentionKeepsTheNewestSuffix pins what the bound keeps: the
// newest rows, contiguous, with the oldest dropped.
func TestBGEventRetentionKeepsTheNewestSuffix(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetBGEventRetention(10)

	sess, err := st.Create(ctx, "", "", "build", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	const total = 3 * bgPruneInterval
	for i := 0; i < total; i++ {
		if err := st.AppendBGEvent(ctx, sess.ID, fmt.Sprintf(`{"kind":"text","text":"%d"}`, i)); err != nil {
			t.Fatal(err)
		}
	}
	if got, max := countBGEvents(t, st, sess.ID), 10+bgPruneInterval; got > max {
		t.Errorf("retention 10: %d rows after %d appends, want <= %d", got, total, max)
	}

	events, err := st.ListBGEvents(ctx, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Survivors must be the contiguous newest suffix: event i, i+1, ... total-1.
	start := total - len(events)
	for i, e := range events {
		if want := fmt.Sprintf(`"text":"%d"}`, start+i); !strings.Contains(e.Data, want) {
			t.Fatalf("survivor %d = %q, want one containing %q (survivors should be a contiguous newest suffix)", i, e.Data, want)
		}
	}
}

// TestBGEventPruneSweepsOnAProcessFirstAppend covers the restart case. The
// sweep counter is in memory, so a session appending fewer than bgPruneInterval
// events per daemon lifetime would accumulate across restarts and never be
// swept — the same unbounded growth, just slower. The first append a session
// makes in a process must therefore sweep.
func TestBGEventPruneSweepsOnAProcessFirstAppend(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.Create(ctx, "", "", "build", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Accumulate well past the bound without ever reaching a sweep interval,
	// by writing straight to the table the way a series of short daemon
	// lifetimes would leave it.
	for i := 0; i < 200; i++ {
		if _, err := st.db.ExecContext(ctx,
			`INSERT INTO bg_events (session_id, data, created_at) VALUES (?, ?, 0)`,
			sess.ID, fmt.Sprintf(`{"kind":"text","text":"%d"}`, i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := Open(path) // "restart"
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	st2.SetBGEventRetention(5)
	if err := st2.AppendBGEvent(ctx, sess.ID, `{"kind":"text","text":"new"}`); err != nil {
		t.Fatal(err)
	}
	if got := countBGEvents(t, st2, sess.ID); got != 5 {
		t.Errorf("first append after restart left %d rows, want the retention bound of 5", got)
	}
}

// TestBGEventPruneIsPerSession: bounding one session must not touch another's
// buffered events.
func TestBGEventPruneIsPerSession(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetBGEventRetention(3)

	a, err := st.Create(ctx, "", "", "build", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Create(ctx, "", "", "build", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := st.AppendBGEvent(ctx, b.ID, `{"kind":"text","text":"b"}`); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < bgPruneInterval+1; i++ {
		if err := st.AppendBGEvent(ctx, a.ID, `{"kind":"text","text":"a"}`); err != nil {
			t.Fatal(err)
		}
	}
	if got := countBGEvents(t, st, b.ID); got != 2 {
		t.Errorf("pruning session A left session B with %d rows, want 2", got)
	}
}
