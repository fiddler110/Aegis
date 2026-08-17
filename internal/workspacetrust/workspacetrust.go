// Package workspacetrust records per-directory trust decisions gating
// project-sourced security-relevant configuration (P27.1, FIND-01/FIND-02).
// A cloned repository's .aegis/config.yaml is applied with no confirmation by
// default; this package backs the "trust this workspace?" decision that lets
// an operator explicitly accept a project's security-relevant settings
// (permission.*, sandbox.*, mcp.servers, notify.webhook, hooks) once per
// directory, persisted across restarts.
//
// Since P66.25/SEC-07 a grant also records a fingerprint of the content it was
// granted against, so that "trusted" stops meaning "trusted forever, whatever
// this repository becomes". See Entry and Check.
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

// Entry records when a directory was trusted, and against *what content*.
//
// P66.25/SEC-07: before this, an entry was a timestamp and nothing else, so a
// grant said "this path is trusted" and never "this content is trusted". A
// `git pull` that added a `hooks:` block, flipped `security.*` or introduced a
// `commands:` override re-prompted nothing — the operator approved a directory
// weeks ago and silently inherited whatever the repository had become since.
// Fingerprint pins the grant to the security-relevant configuration that was on
// disk when it was made; Check reports a moved fingerprint as Stale.
//
// The value is opaque here on purpose. This package must not import
// internal/config (config imports it, to resolve trust before any
// project-controlled file is read — P66.1), so *what* is hashed is config's
// business; see config.SecurityFingerprint, which is also where the deliberate
// `.aegis/.env` hole is written down.
type Entry struct {
	TrustedAt time.Time `json:"trusted_at"`
	// Fingerprint is empty only in a grant written before P66.25. Computed
	// fingerprints are never empty (they are a hash, always rendered), so the
	// empty string is a reliable "pre-fingerprint grant" marker rather than an
	// ambiguous one — see Check for what that migration does.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Status is the answer to "may this directory's project-sourced
// security-relevant config be applied?" (P66.25/SEC-07).
type Status int

const (
	// Untrusted: no grant was ever recorded for this directory.
	Untrusted Status = iota
	// Trusted: a grant exists and the content it was made against is still
	// the content on disk.
	Trusted
	// Stale: a grant exists but the security-relevant config has changed
	// since — or the grant predates P66.25 and records no content at all.
	// Callers treat Stale exactly as Untrusted for *gating* purposes (the
	// freeze applies, nothing is unlocked); it exists as a separate value so
	// the operator-facing message can say "what you approved has changed"
	// rather than "you never approved this".
	Stale
)

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
// trailing separator, "..", or reached through a symlink), and case-folds on
// Windows where the filesystem itself is case-insensitive. Symlink resolution
// matters because callers record and look up trust for the current working
// directory via os.Getwd, which already returns the fully-resolved real path
// (on macOS a temp/home dir is commonly a symlink, e.g. /var → /private/var);
// without EvalSymlinks here a store seeded with the symlink spelling would not
// match the getwd-resolved spelling and a revoke could silently miss.
func normalize(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	abs = filepath.Clean(abs)
	// EvalSymlinks only works on paths that exist; a revoke of an already-
	// deleted directory (or any non-existent spelling) keeps the cleaned form.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs
}

// Check reports whether dir's recorded trust decision still covers the content
// described by fingerprint (P66.25/SEC-07).
//
// The migration rule for a grant with no fingerprint — every grant written
// before P66.25 — is **Stale, not Trusted**: the operator is re-prompted once,
// after which the grant carries a fingerprint and behaves normally. The
// alternative (adopt whatever is on disk at first load) is exactly the silent
// inheritance this item exists to end: those grants were made against content
// nobody recorded, so "it still matches" is not a fact anyone can check, and
// adopting would bless a `hooks:` block added between the grant and the
// upgrade. One prompt per already-trusted directory is the price, and it is
// paid once.
func (s *Store) Check(dir, fingerprint string) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[normalize(dir)]
	switch {
	case !ok:
		return Untrusted
	case e.Fingerprint == "":
		return Stale // pre-P66.25 grant: content unknown, so never assumed equal
	case e.Fingerprint != fingerprint:
		return Stale
	default:
		return Trusted
	}
}

// IsTrusted reports whether dir has a recorded trust decision that still
// covers fingerprint. Stale and Untrusted are both false here — a caller that
// needs to tell them apart calls Check.
func (s *Store) IsTrusted(dir, fingerprint string) bool {
	return s.Check(dir, fingerprint) == Trusted
}

// Trust records dir as trusted for exactly the content described by
// fingerprint, and persists the store. Re-running it after the content moves
// is how a Stale grant is renewed. fingerprint must be non-empty: an empty one
// writes back the pre-P66.25 shape, which Check reads as Stale forever, so
// every caller goes through config.SecurityFingerprint rather than passing "".
func (s *Store) Trust(dir, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[normalize(dir)] = Entry{TrustedAt: time.Now().UTC(), Fingerprint: fingerprint}
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
