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

// readState is what the tracker remembers about the agent's last look at a
// file: when the bytes on disk were last stamped, and whether the agent has
// actually seen the file's *whole* content.
//
// partial is the P38.1 corollary. Once read_file caps an unbounded read at
// defaultReadLines (or the caller asks for an explicit offset/limit window),
// the agent holds only a slice of the file — but the mtime alone can't tell
// that apart from a complete read, so a subsequent whole-file write_file would
// happily replace content the agent never saw. Recording the distinction lets
// CheckFullOverwrite refuse exactly that case while leaving anchored edits
// (edit_file/multi_edit, which can only touch text they matched) unaffected.
type readState struct {
	mtime   time.Time
	partial bool
}

// Tracker records read timestamps for workspace files. Thread-safe.
type Tracker struct {
	mu    sync.Mutex
	reads map[string]readState  // abs path → state at last read
	hunks map[string]*fileHunks // abs path → agent-authored line ranges (P45.2)
}

// New creates a file tracker.
func New() *Tracker {
	return &Tracker{
		reads: make(map[string]readState),
		hunks: make(map[string]*fileHunks),
	}
}

// RecordRead stores the current mtime of path and marks the agent as having
// seen the file in full. Called after a read_file execution that returned the
// entire file.
func (t *Tracker) RecordRead(path string) { t.recordRead(path, false) }

// RecordPartialRead is RecordRead for a read that returned only a window of the
// file — a capped unbounded read, or an explicit offset/limit range that did
// not reach the end. The staleness guard behaves identically; only
// CheckFullOverwrite distinguishes the two.
func (t *Tracker) RecordPartialRead(path string) { t.recordRead(path, true) }

func (t *Tracker) recordRead(path string, partial bool) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	t.mu.Lock()
	t.reads[path] = readState{mtime: info.ModTime(), partial: partial}
	if len(t.reads) > maxTrackedFiles {
		t.pruneOldestLocked()
	}
	t.mu.Unlock()
}

func (t *Tracker) pruneOldestLocked() {
	var oldestPath string
	var oldestTime time.Time
	for p, st := range t.reads {
		if oldestPath == "" || st.mtime.Before(oldestTime) {
			oldestPath = p
			oldestTime = st.mtime
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
	st, tracked := t.reads[path]
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
	if !currentMtime.Equal(st.mtime) {
		return fmt.Errorf("file %s was modified externally (read mtime %s, current %s); re-read the file before editing",
			path, st.mtime.Format(time.RFC3339Nano), currentMtime.Format(time.RFC3339Nano))
	}
	return nil
}

// CheckFullOverwrite reports whether it is safe to replace path's entire
// content. It is the stricter companion to CheckWrite, for whole-file writes
// only: an anchored edit can only alter text it matched, but a whole-file write
// discards everything the agent did not see. It fails when the agent's last
// look at the file was a partial read, and is silent otherwise — including for
// an untracked file, where the agent has made no claim to have read it and the
// existing "first write to an unseen file" allowance stands.
//
// Callers should still call CheckWrite; this check is additive, not a
// replacement for the external-modification guard.
func (t *Tracker) CheckFullOverwrite(path string) error {
	t.mu.Lock()
	st, tracked := t.reads[path]
	t.mu.Unlock()

	if !tracked || !st.partial {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		// Gone from disk; a write re-creates rather than overwrites.
		return nil
	}
	return fmt.Errorf("file %s was only read in part, so a whole-file write would discard content never seen; "+
		"re-read it in full (read_file with offset/limit until the end) before overwriting, or use edit_file to change just the part you mean", path)
}

// RecordWrite updates the tracked mtime after a successful write that changed
// only part of the file (an anchored edit). The partial-read flag is
// deliberately preserved: replacing a matched string tells the agent nothing
// about the rest of the file, so a capped read followed by an edit must not
// silently license a later whole-file overwrite.
func (t *Tracker) RecordWrite(path string) { t.recordWrite(path, false) }

// RecordOverwrite updates the tracked mtime after a successful whole-file
// write, and clears any partial-read flag — after replacing every byte, the
// agent knows the file's full content by construction.
func (t *Tracker) RecordOverwrite(path string) { t.recordWrite(path, true) }

func (t *Tracker) recordWrite(path string, full bool) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	t.mu.Lock()
	st := t.reads[path]
	st.mtime = info.ModTime()
	if full {
		st.partial = false
	}
	t.reads[path] = st
	if len(t.reads) > maxTrackedFiles {
		t.pruneOldestLocked()
	}
	t.mu.Unlock()
}

// Clear removes all tracking state.
func (t *Tracker) Clear() {
	t.mu.Lock()
	t.reads = make(map[string]readState)
	t.hunks = make(map[string]*fileHunks)
	t.mu.Unlock()
}

// TrackedFiles returns the number of files being tracked.
func (t *Tracker) TrackedFiles() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.reads)
}
