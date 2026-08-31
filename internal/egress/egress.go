// Package egress tracks outbound fetch volume (web_fetch) for the lifetime of
// one engine run — the live, session-scoped counterpart to the durable
// per-call record hooks.Audit already writes to the audit sink (P81.8/FIND-08).
// The audit sink is the forensic trail; Tracker is what a TUI or web UI reads
// to answer "how much has this run sent out" without parsing JSONL.
package egress

import (
	"maps"
	"sync"
)

// Tracker accumulates outbound fetch volume across an arbitrary number of
// calls. It is safe for concurrent use — parallel tool rounds (P8.6) can fetch
// more than one URL at once.
type Tracker struct {
	mu         sync.Mutex
	totalBytes int64
	fetches    int
	hosts      map[string]int64 // host -> cumulative bytes
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker { return &Tracker{} }

// Add records one fetch of n bytes from host. A nil Tracker is a valid no-op
// receiver, so call sites need not nil-check before recording.
func (t *Tracker) Add(host string, n int) {
	if t == nil || n < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalBytes += int64(n)
	t.fetches++
	if host != "" {
		if t.hosts == nil {
			t.hosts = make(map[string]int64)
		}
		t.hosts[host] += int64(n)
	}
}

// Snapshot is a point-in-time view of accumulated egress.
type Snapshot struct {
	TotalBytes int64
	Fetches    int
	Hosts      map[string]int64 // host -> cumulative bytes
}

// Snapshot returns the current totals. A nil Tracker reports the zero value.
func (t *Tracker) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	hosts := make(map[string]int64, len(t.hosts))
	maps.Copy(hosts, t.hosts)
	return Snapshot{TotalBytes: t.totalBytes, Fetches: t.fetches, Hosts: hosts}
}
