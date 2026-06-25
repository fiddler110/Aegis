// Package filetracker records when files were last read by the agent and
// detects external modifications between a read and a subsequent write. This
// enforces a safe read-before-write discipline: if a file has been changed
// outside the agent since the last read, the write is rejected and the agent
// must re-read the file first.
package filetracker

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const maxTrackedFiles = 10000

// Tracker records read timestamps for workspace files. Thread-safe.
type Tracker struct {
	mu    sync.Mutex
	reads map[string]time.Time // abs path → mtime at last read
}

// New creates a file tracker.
func New() *Tracker {
	return &Tracker{reads: make(map[string]time.Time)}
}

// RecordRead stores the current mtime of path. Called after a successful
// read_file execution.
func (t *Tracker) RecordRead(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	t.mu.Lock()
	t.reads[path] = info.ModTime()
	if len(t.reads) > maxTrackedFiles {
		t.pruneOldestLocked()
	}
	t.mu.Unlock()
}

func (t *Tracker) pruneOldestLocked() {
	var oldestPath string
	var oldestTime time.Time
	for p, mt := range t.reads {
		if oldestPath == "" || mt.Before(oldestTime) {
			oldestPath = p
			oldestTime = mt
		}
	}
	if oldestPath != "" {
		delete(t.reads, oldestPath)
	}
}

// CheckWrite verifies that path has not been modified externally since the
// last read. Returns nil if safe to write, or an error explaining the
// staleness. If the file has never been read, it is allowed (new file
// creation, or first write to an existing file the agent hasn't inspected).
func (t *Tracker) CheckWrite(path string) error {
	t.mu.Lock()
	readMtime, tracked := t.reads[path]
	t.mu.Unlock()

	if !tracked {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		// File was deleted externally; allow the write (re-creation).
		return nil
	}

	currentMtime := info.ModTime()
	if !currentMtime.Equal(readMtime) {
		return fmt.Errorf("file %s was modified externally (read mtime %s, current %s); re-read the file before editing",
			path, readMtime.Format(time.RFC3339Nano), currentMtime.Format(time.RFC3339Nano))
	}
	return nil
}

// RecordWrite updates the tracked mtime after a successful write, so a
// subsequent write without an intervening external modification is allowed.
func (t *Tracker) RecordWrite(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	t.mu.Lock()
	t.reads[path] = info.ModTime()
	t.mu.Unlock()
}

// Clear removes all tracking state.
func (t *Tracker) Clear() {
	t.mu.Lock()
	t.reads = make(map[string]time.Time)
	t.mu.Unlock()
}

// TrackedFiles returns the number of files being tracked.
func (t *Tracker) TrackedFiles() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.reads)
}
