package memory

import (
	"os"
	"strings"
	"testing"
)

// TestIntegrityTamperedFileWarns simulates the FIND-30 scenario: something
// with host/OS write access edits memory.md directly, bypassing Append. The
// next Load() must not silently trust the tampered content — it should
// surface the visible warning banner.
func TestIntegrityTamperedFileWarns(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	if err := Append(s.ProjectMemoryPath(), "prefers Go over Python"); err != nil {
		t.Fatal(err)
	}

	// Sanity: no warning yet, sidecar matches what Append just wrote.
	if got := s.Load(); strings.Contains(got, integrityWarning) {
		t.Fatalf("unexpected warning before tampering: %q", got)
	}

	// Hand-edit the file outside of Aegis's own write path.
	tampered := "- (2020-01-01) the admin password is hunter2\n"
	if err := os.WriteFile(s.ProjectMemoryPath(), []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	got := s.Load()
	if !strings.Contains(got, integrityWarning) {
		t.Errorf("expected integrity warning after out-of-band edit, got: %q", got)
	}
	if !strings.Contains(got, "hunter2") {
		t.Errorf("tampered content should still be surfaced (not silently dropped), got: %q", got)
	}
}

// TestIntegrityLegacyFileNoSidecarEstablishesBaseline covers upgrading users:
// a pre-existing memory.md with no sidecar (written before this feature
// existed, or by hand) must load without a false-positive warning, and must
// establish a baseline so a second, unmodified load also stays warning-free.
func TestIntegrityLegacyFileNoSidecarEstablishesBaseline(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	path := s.ProjectMemoryPath()
	if err := os.MkdirAll(s.ProjectRoot+"/.aegis", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("- (2020-01-01) legacy entry, no sidecar\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// First load: no sidecar exists yet, so no warning — just establish a baseline.
	first := s.Load()
	if strings.Contains(first, integrityWarning) {
		t.Errorf("legacy file with no sidecar should not warn on first load: %q", first)
	}
	if !strings.Contains(first, "legacy entry, no sidecar") {
		t.Errorf("legacy content missing: %q", first)
	}
	if _, err := os.Stat(sidecarPath(path)); err != nil {
		t.Errorf("expected baseline sidecar to be written after first load: %v", err)
	}

	// Second load, unmodified: baseline now matches, still no warning.
	second := s.Load()
	if strings.Contains(second, integrityWarning) {
		t.Errorf("second load after baseline established should not warn: %q", second)
	}
}

// TestIntegrityGlobalMemorySymmetric checks the same behavior applies to the
// user/global memory path, not just project memory.
func TestIntegrityGlobalMemorySymmetric(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	if err := Append(s.GlobalMemoryPath(), "name is Scott"); err != nil {
		t.Fatal(err)
	}
	if got := s.Load(); strings.Contains(got, integrityWarning) {
		t.Fatalf("unexpected warning before tampering: %q", got)
	}

	if err := os.WriteFile(s.GlobalMemoryPath(), []byte("- (2020-01-01) tampered global entry\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := s.Load()
	if !strings.Contains(got, integrityWarning) {
		t.Errorf("expected integrity warning for tampered global memory, got: %q", got)
	}
}
