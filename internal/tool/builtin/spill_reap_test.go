package builtin

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// LLM-18: a spill used to pay for a full directory scan every time. The
// throttle must skip the scan inside the interval and must still run it once
// the interval has passed — a throttle that never reaps is a disk leak, which
// is the failure the original code was avoiding.
func TestReapSpillsThrottledSkipsInsideTheInterval(t *testing.T) {
	dir := t.TempDir()
	// A file older than spillTTL is what a real reap would delete, so its
	// survival is the observable signal that no reap ran.
	stale := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * spillTTL)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	spillReapMu.Lock()
	delete(spillReapLast, dir)
	spillReapMu.Unlock()

	// First call reaps: the stale file goes.
	reapSpillsThrottled(dir)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("first reap did not remove the stale file (err=%v)", err)
	}

	// Recreate it and call again immediately — the throttle must suppress the
	// scan, so the stale file survives.
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	reapSpillsThrottled(dir)
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("throttle did not suppress the second scan: %v", err)
	}

	// Age the throttle past the interval; the next call must reap again.
	spillReapMu.Lock()
	spillReapLast[dir] = time.Now().Add(-2 * spillReapInterval)
	spillReapMu.Unlock()
	reapSpillsThrottled(dir)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("reap did not resume after the interval elapsed (err=%v)", err)
	}
}

// One busy workspace must not starve another's reap, which is why the throttle
// is keyed by directory rather than held as a single timestamp.
func TestReapSpillsThrottleIsPerDirectory(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	old := time.Now().Add(-2 * spillTTL)
	staleIn := func(dir string) string {
		p := filepath.Join(dir, "stale.txt")
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}
	sa, sb := staleIn(a), staleIn(b)

	spillReapMu.Lock()
	delete(spillReapLast, a)
	delete(spillReapLast, b)
	spillReapMu.Unlock()

	reapSpillsThrottled(a)
	if _, err := os.Stat(sa); !os.IsNotExist(err) {
		t.Errorf("dir a was not reaped: %v", err)
	}
	// b has its own timestamp, so a's recent reap must not suppress it.
	reapSpillsThrottled(b)
	if _, err := os.Stat(sb); !os.IsNotExist(err) {
		t.Errorf("dir b was starved by dir a's reap: %v", err)
	}
}
