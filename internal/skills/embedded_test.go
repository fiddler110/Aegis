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

// RefreshProjectBuiltins must repair a stale already-materialized skill even
// though it's given no skill names — the "skill was materialized by an older
// binary and this run doesn't invoke it" case. It also must REPORT what it
// rewrote so the caller can surface a notice.
func TestRefreshProjectBuiltinsRepairsStalePresentSkill(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"threat-modeling"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workDir, ".aegis", "builtin-skills", "threat-modeling", "verify.py")
	fresh, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Hour)
	if err := os.WriteFile(target, []byte("stale verify.py from an old binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, stale, stale); err != nil {
		t.Fatal(err)
	}

	changed, err := RefreshProjectBuiltins(workDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(fresh) {
		t.Errorf("stale verify.py should have been restored to the embedded content")
	}
	found := false
	for _, c := range changed {
		if c == "threat-modeling/verify.py" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected refreshed file list to include threat-modeling/verify.py, got %v", changed)
	}
}

// RefreshProjectBuiltins must NOT create a skill that was never materialized —
// it only reconciles what's already on disk, so an unrelated run doesn't
// suddenly extract every builtin into the project.
func TestRefreshProjectBuiltinsDoesNotCreateAbsentSkills(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshProjectBuiltins(workDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".aegis", "builtin-skills", "threat-modeling")); !os.IsNotExist(err) {
		t.Errorf("threat-modeling should NOT be created by a refresh, stat err = %v", err)
	}
}

// A no-op refresh of an unchanged suite reports zero rewrites and leaves mtimes
// stable (so the Discover cache isn't invalidated), and a project with no
// materialized skills at all is a clean no-op.
func TestRefreshProjectBuiltinsNoopWhenNothingStale(t *testing.T) {
	empty := t.TempDir()
	if changed, err := RefreshProjectBuiltins(empty); err != nil || len(changed) != 0 {
		t.Errorf("expected clean no-op on a project with no materialized skills, got changed=%v err=%v", changed, err)
	}

	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}
	changed, err := RefreshProjectBuiltins(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Errorf("expected no rewrites for an up-to-date skill, got %v", changed)
	}
}

// A user's own bundled skill sitting under .aegis/builtin-skills (not a shipped
// builtin) must be left completely untouched by the refresh.
func TestRefreshProjectBuiltinsLeavesUserContentAlone(t *testing.T) {
	workDir := t.TempDir()
	custom := filepath.Join(workDir, ".aegis", "builtin-skills", "my-custom-skill")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("my own skill, hands off\n")
	if err := os.WriteFile(filepath.Join(custom, "SKILL.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := RefreshProjectBuiltins(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Errorf("a non-builtin skill dir should not be refreshed, got %v", changed)
	}
	got, err := os.ReadFile(filepath.Join(custom, "SKILL.md"))
	if err != nil || string(got) != string(body) {
		t.Errorf("user skill content changed: got %q err %v", string(got), err)
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
