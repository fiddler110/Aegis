package session

import (
	"path/filepath"
	"testing"
)

// TestBusyTimeoutSetOnEveryConnection pins P63.4: the store must not fall back
// to SQLite's default of failing a contended write with SQLITE_BUSY
// immediately, and the setting must survive connection churn.
//
// The churn half is the part that distinguishes a DSN `_pragma=` parameter
// from a `db.Exec("PRAGMA busy_timeout=...")`: busy_timeout is per-connection
// state and is not persisted in the database file the way journal_mode=WAL is,
// so an Exec covers only the connection that happened to serve it. Dropping
// the idle pool to zero forces database/sql to open a fresh connection for
// each query, which is what an Exec-configured store would fail.
//
// This cannot be a genuine cross-process contention test — reliably parking a
// write lock long enough to observe the wait would need a second process and a
// timing window, which is exactly the kind of test that flakes. Asserting the
// setting is in force on every connection is the durable half.
func TestBusyTimeoutSetOnEveryConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// No idle connections retained: each query below gets a brand new one.
	s.db.SetMaxIdleConns(0)

	for i := 0; i < 3; i++ {
		var ms int
		if err := s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&ms); err != nil {
			t.Fatalf("query busy_timeout: %v", err)
		}
		if ms != 5000 {
			t.Fatalf("connection %d: busy_timeout = %d ms, want 5000", i, ms)
		}
	}

	// The DSN parameter must not have leaked into the path SQLite opened.
	var journal string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}
}
