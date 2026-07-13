// Package workspacetrust records per-directory trust decisions gating
// project-sourced security-relevant configuration (P27.1, FIND-01/FIND-02).
// A cloned repository's .aegis/config.yaml is applied with no confirmation by
// default; this package backs the "trust this workspace?" decision that lets
// an operator explicitly accept a project's security-relevant settings
// (permission.*, sandbox.*, mcp.servers, notify.webhook, hooks) once per
// directory, persisted across restarts.
package workspacetrust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// Entry records when a directory was trusted.
type Entry struct {
	TrustedAt time.Time `json:"trusted_at"`
}

// Store is a small JSON-backed map of trusted directory paths. Safe for
// concurrent use.
type Store struct {
	mu      sync.Mutex
	path    string
	entries map[string]Entry
}

// Open loads the trust store from path if it exists; a missing or unreadable
// file just starts empty (nothing is trusted yet), matching the fail-closed
// posture the rest of this feature relies on.
func Open(path string) *Store {
	s := &Store{path: path, entries: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &s.entries)
	}
	return s
}

// normalize resolves dir to an absolute, cleaned path so the same directory
// is recognized regardless of how it was spelled (relative vs. absolute,
// trailing separator, ".."), and case-folds on Windows where the filesystem
// itself is case-insensitive.
func normalize(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs
}

// IsTrusted reports whether dir has a recorded trust decision.
func (s *Store) IsTrusted(dir string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[normalize(dir)]
	return ok
}

// Trust records dir as trusted and persists the store.
func (s *Store) Trust(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[normalize(dir)] = Entry{TrustedAt: time.Now().UTC()}
	return s.save()
}

// Revoke removes any trust decision for dir and persists the store.
func (s *Store) Revoke(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, normalize(dir))
	return s.save()
}

// TrustedAt returns when dir was trusted and true, or the zero time and
// false if it has no recorded decision.
func (s *Store) TrustedAt(dir string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[normalize(dir)]
	return e.TrustedAt, ok
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return err
	}
	// Best-effort: matches the ACL hardening already applied to
	// daemon.token/session DB (FIND-29/P24.16) — a failure here shouldn't
	// break the write that already succeeded.
	_ = fsguard.RestrictToOwner(s.path)
	return nil
}
