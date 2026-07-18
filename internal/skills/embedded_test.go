package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

func TestMaterializeBuiltinsToProjectOnlyWritesEnabledNames(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"threat-modeling"}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(workDir, ".aegis", "builtin-skills")
	if _, err := os.Stat(filepath.Join(dest, "threat-modeling", "SKILL.md")); err != nil {
		t.Fatalf("expected threat-modeling to be materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "security-audit")); !os.IsNotExist(err) {
		t.Errorf("expected security-audit to NOT be materialized, stat err = %v", err)
	}
}

// TestMaterializeBuiltinsToProjectPlacesAssetsReachableByReadFile is the
// literal regression test for the bug this fix addresses: before this
// change, a builtin's reference/skeleton assets lived outside any project
// workspace, so sandbox.ValidatePath (what the read_file tool uses) always
// rejected them with "escapes the workspace root". Once materialized under
// workDir, the same validation must succeed.
func TestMaterializeBuiltinsToProjectPlacesAssetsReachableByReadFile(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"threat-modeling"}); err != nil {
		t.Fatal(err)
	}

	rel := filepath.Join(".aegis", "builtin-skills", "threat-modeling", "references", "stride.md")
	abs, err := sandbox.ValidatePath(workDir, rel)
	if err != nil {
		t.Fatalf("read_file's path validation should succeed for a project-materialized builtin asset, got: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("validated path does not exist on disk: %v", err)
	}
}

func TestMaterializeBuiltinsToProjectIdempotentNoRewrite(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workDir, ".aegis", "builtin-skills", "security-audit", "SKILL.md")
	info1, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	// Re-materialize with unchanged embedded content; the write-if-different
	// guard should leave the file's mtime untouched so skillsDirSignature's
	// cache isn't invalidated on every repeat activation.
	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("expected mtime to stay stable across a no-op re-materialize, got %v -> %v", info1.ModTime(), info2.ModTime())
	}
}

func TestMaterializeBuiltinsToProjectPicksUpUpgrade(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workDir, ".aegis", "builtin-skills", "security-audit", "SKILL.md")
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate stale pre-upgrade content sitting on disk from an older binary.
	stale := time.Now().Add(-time.Hour)
	if err := os.WriteFile(target, []byte("stale content from an old binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, stale, stale); err != nil {
		t.Fatal(err)
	}

	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("expected stale content to be overwritten back to the embedded content")
	}
}

func TestMaterializeBuiltinsToProjectNoopOnEmptyWorkdirOrNames(t *testing.T) {
	if err := MaterializeBuiltinsToProject("", []string{"security-audit"}); err != nil {
		t.Errorf("expected no-op (nil error) on empty workDir, got %v", err)
	}
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, nil); err != nil {
		t.Errorf("expected no-op (nil error) on empty names, got %v", err)
	}
	if entries, _ := os.ReadDir(workDir); len(entries) != 0 {
		t.Errorf("expected workDir to stay empty, got %v", entries)
	}
}
