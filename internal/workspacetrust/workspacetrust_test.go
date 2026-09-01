package workspacetrust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fp is a stand-in for a config.SecurityFingerprint value. This package never
// computes one (it must not import internal/config), so the tests only care
// that it is an opaque non-empty string.
const fp = "sha256:aaaa"

func TestTrustPersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if s.IsTrusted(dir, fp) {
		t.Fatal("fresh store should not trust anything")
	}
	if err := s.Trust(dir, fp); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if !s.IsTrusted(dir, fp) {
		t.Fatal("directory should be trusted after Trust")
	}

	reopened := Open(path)
	if !reopened.IsTrusted(dir, fp) {
		t.Fatal("trust decision should persist across Open")
	}
	// P66.25/SEC-07: the fingerprint has to survive the round-trip too — a
	// grant that persisted its timestamp but dropped its fingerprint would
	// read back as a pre-fingerprint grant and be permanently stale.
	if got := reopened.Check(dir, fp); got != Trusted {
		t.Fatalf("reopened Check = %v, want Trusted", got)
	}
}

func TestRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.Trust(dir, fp); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if err := s.Revoke(dir); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if s.IsTrusted(dir, fp) {
		t.Fatal("directory should not be trusted after Revoke")
	}
	if got := s.Check(dir, fp); got != Untrusted {
		t.Fatalf("Check after Revoke = %v, want Untrusted", got)
	}
}

func TestMissingStoreFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	s := Open(path)
	if s.IsTrusted(t.TempDir(), fp) {
		t.Fatal("missing store file should not trust anything")
	}
}

// TestMovedFingerprintIsStale is P66.25/SEC-07's core assertion: a grant is
// bound to the content it was granted against, so content that has moved since
// (the `git pull` that adds a hooks: block) reports Stale rather than trusted —
// and re-granting against the new content restores it.
func TestMovedFingerprintIsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.Trust(dir, "sha256:before"); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if got := s.Check(dir, "sha256:after"); got != Stale {
		t.Fatalf("Check with a moved fingerprint = %v, want Stale", got)
	}
	if s.IsTrusted(dir, "sha256:after") {
		t.Error("a grant whose content moved must not report trusted")
	}
	// Stale must stay distinguishable from Untrusted — the operator-facing
	// message depends on telling "you never approved this" from "what you
	// approved has changed".
	if got := s.Check(t.TempDir(), "sha256:after"); got != Untrusted {
		t.Errorf("Check for a directory with no grant = %v, want Untrusted", got)
	}

	if err := s.Trust(dir, "sha256:after"); err != nil {
		t.Fatalf("re-Trust: %v", err)
	}
	if got := s.Check(dir, "sha256:after"); got != Trusted {
		t.Fatalf("Check after re-granting = %v, want Trusted", got)
	}
}

// TestPreFingerprintGrantIsStaleNotTrusted pins P66.25/SEC-07's migration rule.
// A grant written before this item carries no fingerprint; it must re-prompt
// once rather than adopt whatever is on disk, because "it still matches" is not
// a fact anybody recorded — adopting would bless a hooks: block added between
// the grant and the upgrade. The store format is the on-disk shape of a real
// pre-P66.25 file, written here by hand so the migration is tested against the
// format that actually shipped rather than against a re-serialization of the
// current struct.
func TestPreFingerprintGrantIsStaleNotTrusted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	legacy := map[string]map[string]string{
		normalize(dir): {"trusted_at": time.Now().UTC().Format(time.RFC3339Nano)},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	s := Open(path)
	if got := s.Check(dir, fp); got != Stale {
		t.Fatalf("pre-P66.25 grant Check = %v, want Stale (re-prompt once)", got)
	}
	if s.IsTrusted(dir, fp) {
		t.Fatal("a pre-P66.25 grant must not be silently treated as a fingerprint match")
	}
	// The old timestamp is still readable — the migration re-prompts, it does
	// not discard the record.
	if _, ok := s.TrustedAt(dir); !ok {
		t.Error("a pre-P66.25 grant should still be present in the store")
	}
	// One prompt, not a prompt every time: re-granting writes a fingerprint.
	if err := s.Trust(dir, fp); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if got := Open(path).Check(dir, fp); got != Trusted {
		t.Fatalf("Check after re-granting a migrated entry = %v, want Trusted", got)
	}
}

// A directory trusted through one path spelling must stay trusted (and be
// revocable) through a symlinked spelling of the same directory. This is the
// exact mismatch that broke `aegis trust --revoke` on macOS, where os.Getwd
// returns the fully symlink-resolved real path (e.g. /var → /private/var) while
// a caller may hand normalize the symlink spelling.
func TestNormalizeResolvesSymlinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	s := Open(path)
	// Trust via the real path, look up / revoke via the symlink spelling.
	if err := s.Trust(real, fp); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if !s.IsTrusted(link, fp) {
		t.Fatal("symlink spelling of a trusted directory should also be trusted")
	}
	if err := s.Revoke(link); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if s.IsTrusted(real, fp) {
		t.Error("revoking via the symlink spelling must revoke the real directory too")
	}
}

func TestNormalizeRelativeVsAbsolute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.Trust(dir, fp); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	rel, err := filepath.Rel(".", dir)
	if err == nil {
		if !s.IsTrusted(rel, fp) {
			t.Errorf("relative path %q for the same directory should also be trusted", rel)
		}
	}
}

// TestTrustWithOriginRecordsOriginAndProcess pins FIND-27's origin stamp: a
// grant records which interface made it and a best-effort process
// identification, and both survive a reopen.
func TestTrustWithOriginRecordsOriginAndProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.TrustWithOrigin(dir, fp, "cli"); err != nil {
		t.Fatalf("TrustWithOrigin: %v", err)
	}
	if got := s.Check(dir, fp); got != Trusted {
		t.Fatalf("Check = %v, want Trusted", got)
	}

	reopened := Open(path)
	reopened.mu.Lock()
	e, ok := reopened.entries[normalize(dir)]
	reopened.mu.Unlock()
	if !ok {
		t.Fatal("grant did not survive reopen")
	}
	if e.GrantedVia != "cli" {
		t.Errorf("GrantedVia = %q, want %q", e.GrantedVia, "cli")
	}
	if e.GrantedByProcess == "" {
		t.Error("GrantedByProcess should be populated (os.Executable() should succeed in a go test binary)")
	}
	if e.MAC == "" {
		t.Error("entry should carry a MAC")
	}
	if got := reopened.Check(dir, fp); got != Trusted {
		t.Fatalf("reopened Check = %v, want Trusted", got)
	}
}

// TestTamperedEntryIsStale is FIND-27's core new assertion: a grant edited
// directly in the store file — the exact same-user-process attack the
// finding describes — fails MAC verification and is treated as Stale
// (re-prompt), not silently trusted.
func TestTamperedEntryIsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.TrustWithOrigin(dir, fp, "cli"); err != nil {
		t.Fatalf("TrustWithOrigin: %v", err)
	}

	// Simulate a same-user process inserting/editing a grant directly,
	// without going through Trust/TrustWithOrigin (so it never learns a
	// valid MAC for the tampered content).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	norm := normalize(dir)
	e := entries[norm]
	e.GrantedVia = "tui" // an attacker relabeling how the grant was made, without knowing the key
	entries[norm] = e
	tampered, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	reopened := Open(path)
	if got := reopened.Check(dir, fp); got != Stale {
		t.Fatalf("Check on a tampered entry = %v, want Stale", got)
	}
	if reopened.IsTrusted(dir, fp) {
		t.Error("a tampered entry must never report as Trusted")
	}
}

// TestMissingKeyFileMakesEveryEntryStale confirms an entry with a MAC but no
// readable key (the key file lost, deleted, or belonging to a different
// install) fails closed rather than being trusted on the strength of an
// unverifiable claim.
func TestMissingKeyFileMakesEveryEntryStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.TrustWithOrigin(dir, fp, "cli"); err != nil {
		t.Fatalf("TrustWithOrigin: %v", err)
	}
	if err := os.Remove(keyPath(path)); err != nil {
		t.Fatalf("remove key file: %v", err)
	}

	reopened := Open(path)
	if got := reopened.Check(dir, fp); got != Stale {
		t.Fatalf("Check with the key file missing = %v, want Stale", got)
	}
}

// TestTrustWithoutOriginLeavesGrantedViaEmpty confirms the plain Trust
// wrapper (used by the rest of this package's tests, and any caller that
// doesn't have an origin to report) still authenticates its entry with a
// MAC — it just records no interface.
func TestTrustWithoutOriginLeavesGrantedViaEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.Trust(dir, fp); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	s.mu.Lock()
	e := s.entries[normalize(dir)]
	s.mu.Unlock()
	if e.GrantedVia != "" {
		t.Errorf("GrantedVia = %q, want empty for the origin-less Trust wrapper", e.GrantedVia)
	}
	if e.MAC == "" {
		t.Error("Trust should still compute a MAC even with no origin")
	}
	if got := s.Check(dir, fp); got != Trusted {
		t.Fatalf("Check = %v, want Trusted", got)
	}
}
