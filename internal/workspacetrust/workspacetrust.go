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
//
// # Authentication (FIND-27/P81.27)
//
// The store file itself is ordinary same-user-readable/writable local state
// (fsguard.RestrictToOwner keeps *other* accounts out, not other processes
// running as this user). Without more, a same-user process could insert or
// edit a grant directly and it would be indistinguishable from a real
// operator decision. Each Entry therefore carries a MAC computed with a
// per-install key generated on first use and stored alongside the trust file
// (also owner-restricted); Check treats a missing or invalid MAC as Stale, the
// same safe default already used for a moved fingerprint or a pre-P66.25
// grant.
//
// This is a locally-derived key, not an OS-keychain-backed one: nothing in
// this codebase integrates with a platform credential store (Keychain,
// Credential Manager, Secret Service) to build on, and adding an external
// dependency speculatively for this one call site was judged worse than
// being explicit about what the simpler design covers. What it detects: a
// process that can write the trust store file but does *not* also have read
// access to the sibling key file — accidental corruption, a hand-edited or
// dropped-in JSON file, a grant copied from another machine or restored from
// a backup that didn't carry the key with it. What it does NOT detect: a
// fully-privileged same-user attacker, since the key sits right next to the
// store under the identical owner-only ACL/mode — anything that can read one
// can read the other and forge a valid MAC. Closing that gap is what an
// OS-keychain-backed key (one a same-user process cannot read without going
// through the OS's own access-control/consent prompt) would add; it is not
// what this does.
package workspacetrust

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	// GrantedVia names the interface that made this trust decision — one of
	// reqorigin's constants ("cli", "tui", "web", ...) — self-declared by this
	// package's own Go caller, exactly the way reqorigin's own doc describes
	// (P81.14/FIND-27): never taken from anything a remote or project-
	// controlled input could set. Empty in a grant written before this field
	// existed, or written through the origin-less Trust wrapper.
	GrantedVia string `json:"granted_via,omitempty"`
	// GrantedByProcess is a best-effort identification of the executable that
	// made the grant (path plus PID), recorded automatically rather than
	// supplied by the caller, for the same reason GrantedVia is self-declared:
	// it is a diagnostic breadcrumb, not something a caller could spoof to
	// look more trustworthy. Empty if os.Executable() failed.
	GrantedByProcess string `json:"granted_by_process,omitempty"`
	// MAC authenticates this entry against Store's local key (FIND-27): it is
	// an HMAC-SHA256 over the entry's fields, keyed by a secret generated on
	// first use and stored alongside the trust store with the same
	// owner-only restriction (see loadOrCreateKey). An entry a same-user
	// process inserted or edited by hand — without also reading that key
	// file — fails verification and Check reports it Stale rather than
	// Trusted, exactly like a fingerprint mismatch. This is deliberately
	// *not* an OS-keychain-backed MAC (no credential-store integration
	// exists anywhere in this codebase to build on; see the package doc
	// below for what this narrower, locally-keyed design does and does not
	// cover). Empty for any entry written before this field existed.
	MAC string `json:"mac,omitempty"`
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
	// key is the MAC secret, loaded from keyPath(path) at Open (best effort,
	// nil if it doesn't exist yet) and lazily created on first Trust/save.
	key []byte
}

// macKeySize is the HMAC secret length: 256 bits, matching the hash it keys.
const macKeySize = 32

// Open loads the trust store from path if it exists; a missing or unreadable
// file just starts empty (nothing is trusted yet), matching the fail-closed
// posture the rest of this feature relies on.
func Open(path string) *Store {
	s := &Store{path: path, entries: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &s.entries)
	}
	// Best effort, read-only: a missing key file is normal on first use (it
	// is created lazily by ensureKey when a grant is next written) and any
	// other read failure just means every existing entry's MAC fails
	// verification, which Check already treats as Stale rather than a crash.
	if key, err := os.ReadFile(keyPath(path)); err == nil && len(key) == macKeySize {
		s.key = key
	}
	return s
}

// keyPath is the MAC secret's location, a sibling of the trust store itself
// so both inherit the same directory's ACL/mode from fsguard.RestrictToOwner.
func keyPath(storePath string) string {
	return storePath + ".key"
}

// ensureKey returns the store's MAC secret, generating and persisting one on
// first use. A concurrent Open elsewhere on the same install racing this is
// handled by writing with O_EXCL and re-reading on a collision, so the two
// processes converge on the same key rather than each keeping its own.
func (s *Store) ensureKey() ([]byte, error) {
	if len(s.key) == macKeySize {
		return s.key, nil
	}
	kp := keyPath(s.path)
	if existing, err := os.ReadFile(kp); err == nil && len(existing) == macKeySize {
		s.key = existing
		return s.key, nil
	}
	if err := os.MkdirAll(filepath.Dir(kp), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, macKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(kp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// Another process (or an earlier run of this one) won the race;
			// use what it wrote rather than two keys disagreeing.
			if existing, rerr := os.ReadFile(kp); rerr == nil && len(existing) == macKeySize {
				s.key = existing
				return s.key, nil
			}
		}
		return nil, err
	}
	_, werr := f.Write(key)
	cerr := f.Close()
	if werr != nil {
		return nil, werr
	}
	if cerr != nil {
		return nil, cerr
	}
	_ = fsguard.RestrictToOwner(kp) // best-effort, same posture as the store file
	s.key = key
	return s.key, nil
}

// macFor computes the authentication tag for one entry under dir (already
// normalized). Every field that identifies *what was granted and how* is
// covered, so editing any of them without knowing the key invalidates the
// entry rather than silently taking effect.
func macFor(key []byte, dir string, e Entry) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(dir))
	h.Write([]byte{0})
	h.Write([]byte(e.TrustedAt.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte{0})
	h.Write([]byte(e.Fingerprint))
	h.Write([]byte{0})
	h.Write([]byte(e.GrantedVia))
	h.Write([]byte{0})
	h.Write([]byte(e.GrantedByProcess))
	return hex.EncodeToString(h.Sum(nil))
}

// verifyMAC reports whether e's MAC is present, computable (a key is
// available) and correct for dir. Any other outcome — no MAC, no key, or a
// mismatch — is treated as unauthenticated by the caller (Check), which is
// the fail-closed direction: a legitimate entry can always be re-granted, an
// attacker's forged entry cannot be made to verify without the key.
func (s *Store) verifyMAC(dir string, e Entry) bool {
	if e.MAC == "" || len(s.key) != macKeySize {
		return false
	}
	want := macFor(s.key, dir, e)
	return hmac.Equal([]byte(want), []byte(e.MAC))
}

// currentProcess best-effort identifies the running executable for
// Entry.GrantedByProcess. Never fatal: an unresolvable path just leaves the
// field empty.
func currentProcess() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
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
	norm := normalize(dir)
	e, ok := s.entries[norm]
	switch {
	case !ok:
		return Untrusted
	case e.Fingerprint == "":
		return Stale // pre-P66.25 grant: content unknown, so never assumed equal
	case !s.verifyMAC(norm, e):
		// FIND-27: an entry with no MAC (written before this field existed,
		// or by anything that isn't this package's own Trust) or one whose
		// MAC doesn't match is not distinguishable from a directly-inserted
		// grant, so it gets the same re-prompt a moved fingerprint does
		// rather than being trusted on the strength of an unverifiable claim.
		return Stale
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
//
// Trust records no origin (FIND-27's GrantedVia stays empty); it exists for
// callers — mostly this package's own tests — that don't need to say which
// interface made the decision. Every production call site should prefer
// TrustWithOrigin.
func (s *Store) Trust(dir, fingerprint string) error {
	return s.TrustWithOrigin(dir, fingerprint, "")
}

// TrustWithOrigin is Trust plus FIND-27's origin stamp: origin should be one
// of reqorigin's constants, naming the interface that made this decision
// ("cli", "tui", "web", ...). This package cannot import internal/reqorigin
// (it would be a needless dependency on a leaf value type), so origin is
// passed as a plain string and callers are expected to use reqorigin's
// constants rather than inventing their own spellings.
func (s *Store) TrustWithOrigin(dir, fingerprint, origin string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	norm := normalize(dir)
	e := Entry{
		TrustedAt:        time.Now().UTC(),
		Fingerprint:      fingerprint,
		GrantedVia:       origin,
		GrantedByProcess: currentProcess(),
	}
	key, err := s.ensureKey()
	if err != nil {
		// A grant that can't be authenticated is worse than one that fails
		// loudly here: writing it anyway would silently produce an entry
		// that reads back as Stale (no/invalid MAC) on every future Check,
		// which looks like a bug rather than the key-file problem it is.
		return err
	}
	e.MAC = macFor(key, norm, e)
	s.entries[norm] = e
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
