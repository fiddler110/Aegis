package egress

import "testing"

func TestTrackerAccumulates(t *testing.T) {
	tr := NewTracker()
	tr.Add("example.com", 100)
	tr.Add("example.com", 50)
	tr.Add("other.org", 25)

	snap := tr.Snapshot()
	if snap.TotalBytes != 175 {
		t.Errorf("TotalBytes = %d, want 175", snap.TotalBytes)
	}
	if snap.Fetches != 3 {
		t.Errorf("Fetches = %d, want 3", snap.Fetches)
	}
	if snap.Hosts["example.com"] != 150 {
		t.Errorf("Hosts[example.com] = %d, want 150", snap.Hosts["example.com"])
	}
	if snap.Hosts["other.org"] != 25 {
		t.Errorf("Hosts[other.org] = %d, want 25", snap.Hosts["other.org"])
	}
}

func TestTrackerNilSafe(t *testing.T) {
	var tr *Tracker
	tr.Add("example.com", 100) // must not panic
	if snap := tr.Snapshot(); snap.TotalBytes != 0 || snap.Fetches != 0 {
		t.Errorf("nil tracker snapshot = %+v, want zero value", snap)
	}
}

func TestSnapshotIsolatesHostsMap(t *testing.T) {
	tr := NewTracker()
	tr.Add("example.com", 10)
	snap := tr.Snapshot()
	snap.Hosts["example.com"] = 999 // mutating the snapshot must not reach the tracker
	if got := tr.Snapshot().Hosts["example.com"]; got != 10 {
		t.Errorf("tracker's internal map leaked through Snapshot: got %d, want 10", got)
	}
}
