package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// selfPath resolves to the test binary's own executable — a real, stable
// file on disk usable as a stdio "server binary" without spawning a real MCP
// server. Different test binaries have different content, which is exactly
// what the digest tests need.
func selfPath(t *testing.T) string {
	t.Helper()
	p, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	return p
}

func TestCheckBinaryTrustsOnFirstUse(t *testing.T) {
	dir := t.TempDir()
	store := OpenTrustStore(filepath.Join(dir, "trust.json"))
	bin := selfPath(t)

	abs, ok, reason := store.CheckBinary("srv", bin)
	if !ok {
		t.Fatalf("first connection should be trusted, reason: %q", reason)
	}
	if abs == "" {
		t.Error("expected a resolved absolute path")
	}

	// Persisted: a fresh store instance over the same file sees the entry.
	reloaded := OpenTrustStore(filepath.Join(dir, "trust.json"))
	if _, ok2, _ := reloaded.CheckBinary("srv", bin); !ok2 {
		t.Error("expected the second connection (same binary) to still be trusted")
	}
}

func TestCheckBinaryRefusesChangedDigest(t *testing.T) {
	dir := t.TempDir()
	store := OpenTrustStore(filepath.Join(dir, "trust.json"))

	// Record a baseline against a real file, then mutate its content so the
	// digest no longer matches — the same shape a shimmed/replaced binary on
	// PATH would produce. exec.LookPath on Windows requires a PATHEXT
	// extension (.exe) when given a path directly; POSIX only cares about
	// the executable bit, which .exe doesn't interfere with there.
	binPath := filepath.Join(dir, "server-binary.exe")
	if err := os.WriteFile(binPath, []byte("original content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok, reason := store.CheckBinary("srv", binPath); !ok {
		t.Fatalf("baseline connection should be trusted, reason: %q", reason)
	}

	if err := os.WriteFile(binPath, []byte("REPLACED content — not what was approved"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, ok, reason := store.CheckBinary("srv", binPath)
	if ok {
		t.Fatal("a changed binary digest must be refused, not silently re-trusted")
	}
	if reason == "" {
		t.Error("expected a reason explaining the refusal")
	}
}

func TestCheckBinaryUnresolvableCommandFails(t *testing.T) {
	dir := t.TempDir()
	store := OpenTrustStore(filepath.Join(dir, "trust.json"))
	if _, ok, reason := store.CheckBinary("srv", "this-command-should-not-exist-anywhere-xyz"); ok {
		t.Errorf("expected failure for an unresolvable command, got ok with reason %q", reason)
	}
}

func TestCheckToolNamesNoBaselineReportsNothingGrown(t *testing.T) {
	dir := t.TempDir()
	store := OpenTrustStore(filepath.Join(dir, "trust.json"))
	// No CheckBinary/Approve call yet for this server at all.
	if grown := store.CheckToolNames("srv", []string{"mcp__srv__a", "mcp__srv__b"}); grown != nil {
		t.Errorf("expected nil (nothing to compare against yet), got %v", grown)
	}
}

func TestCheckToolNamesAfterApproveDetectsGrowth(t *testing.T) {
	dir := t.TempDir()
	store := OpenTrustStore(filepath.Join(dir, "trust.json"))
	if err := store.Approve("srv", []string{"mcp__srv__a", "mcp__srv__b"}); err != nil {
		t.Fatal(err)
	}

	grown := store.CheckToolNames("srv", []string{"mcp__srv__a", "mcp__srv__b", "mcp__srv__c"})
	if len(grown) != 1 || grown[0] != "mcp__srv__c" {
		t.Errorf("grown = %v, want exactly [mcp__srv__c]", grown)
	}

	// A shrunk or identical set reports nothing grown.
	if grown := store.CheckToolNames("srv", []string{"mcp__srv__a"}); grown != nil {
		t.Errorf("a shrunk set should report nothing grown, got %v", grown)
	}
}

func TestFilterApprovedWithholdsGrownNamesOnly(t *testing.T) {
	dir := t.TempDir()
	store := OpenTrustStore(filepath.Join(dir, "trust.json"))
	if err := store.Approve("srv", []string{"mcp__srv__a"}); err != nil {
		t.Fatal(err)
	}

	approved, held := store.FilterApproved("srv", []string{"mcp__srv__a", "mcp__srv__evil"})
	if len(approved) != 1 || approved[0] != "mcp__srv__a" {
		t.Errorf("approved = %v, want [mcp__srv__a]", approved)
	}
	if len(held) != 1 || held[0] != "mcp__srv__evil" {
		t.Errorf("held = %v, want [mcp__srv__evil]", held)
	}
}

func TestFilterApprovedFirstEverSetApprovesEverything(t *testing.T) {
	dir := t.TempDir()
	store := OpenTrustStore(filepath.Join(dir, "trust.json"))

	approved, held := store.FilterApproved("srv", []string{"mcp__srv__a", "mcp__srv__b"})
	if len(approved) != 2 {
		t.Errorf("approved = %v, want both names on first sighting", approved)
	}
	if held != nil {
		t.Errorf("held = %v, want nil on first sighting", held)
	}
}

func TestApprovePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	store := OpenTrustStore(path)
	if err := store.Approve("srv", []string{"mcp__srv__a", "mcp__srv__b"}); err != nil {
		t.Fatal(err)
	}

	reloaded := OpenTrustStore(path)
	grown := reloaded.CheckToolNames("srv", []string{"mcp__srv__a", "mcp__srv__b", "mcp__srv__new"})
	if len(grown) != 1 || grown[0] != "mcp__srv__new" {
		t.Errorf("grown after reload = %v, want [mcp__srv__new]", grown)
	}
}

func TestOpenTrustStoreMissingFileStartsEmpty(t *testing.T) {
	store := OpenTrustStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if grown := store.CheckToolNames("srv", []string{"x"}); grown != nil {
		t.Errorf("a fresh store should have no baseline: %v", grown)
	}
}

// TestServerTrustFilePermissions checks the store file lands with an
// owner-only mode on POSIX, mirroring workspacetrust's own posture — skipped
// on Windows, where fsguard.RestrictToOwner uses ACLs rather than a mode bit
// (see fsguard_windows.go; the daemon.crt/token precedent this pattern
// follows is tested by internal/fsguard's own suite, not duplicated here).
func TestServerTrustFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ACL-based restriction, not a mode bit — covered by internal/fsguard's own tests")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	store := OpenTrustStore(path)
	if err := store.Approve("srv", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("trust store file mode = %v, want no group/other permissions", perm)
	}
}
