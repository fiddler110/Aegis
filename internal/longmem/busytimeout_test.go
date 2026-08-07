package longmem

import (
	"path/filepath"
	"testing"
)

// TestBusyTimeoutSetOnEveryConnection pins P63.4 for the long-memory store;
// see internal/session's copy for why connection churn is the load-bearing
// part of the assertion.
func TestBusyTimeoutSetOnEveryConnection(t *testing.T) {
	s, err := Open("proj", filepath.Join(t.TempDir(), "lm.db"), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

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
}
