package drive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRunDirResolverDefaultsToWorkspace is the regression for the half of the
// P52.12 generalization that is invisible until a declared plan actually runs:
// a skill other than threat-modeling resolves its phase globs against the
// workspace root, not the threat-model run directory. Resolving "" instead —
// what LatestRunDir returns for any other layout — makes every phase
// permanently incomplete, so the drive burns each phase's whole turn budget
// while reporting nothing wrong.
func TestRunDirResolverDefaultsToWorkspace(t *testing.T) {
	cwd := t.TempDir()
	if got := RunDirResolver("documentation-as-code", "")(cwd); got != cwd {
		t.Errorf("run dir = %q, want the workspace root %q", got, cwd)
	}
}

// TestRunDirResolverKeepsThreatModelLayout: the built-in plan's own layout is
// unchanged, since its phase globs and its verifier both assume the dated run
// directory under .aegis/security/threat-model.
func TestRunDirResolverKeepsThreatModelLayout(t *testing.T) {
	cwd := t.TempDir()
	run := filepath.Join(cwd, ".aegis", "security", "threat-model", "2026-08-01")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "0-assessment.md"), []byte("# assessment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RunDirResolver("threat-modeling", "")(cwd); got != run {
		t.Errorf("run dir = %q, want the dated run dir %q", got, run)
	}
}

// TestRunDirResolverDeclaredGlobPicksNewest covers the pattern the frontmatter
// key exists for: a setup phase scaffolds a fresh dated directory each run, so
// the later phases must resolve against the newest one rather than the first
// found. "" before anything is scaffolded is the correct answer, not a failure
// — the drive reads it as "setup hasn't run yet".
func TestRunDirResolverDeclaredGlobPicksNewest(t *testing.T) {
	cwd := t.TempDir()
	resolve := RunDirResolver("documentation-as-code", ".aegis/docs/*")

	if got := resolve(cwd); got != "" {
		t.Errorf("before scaffolding, run dir = %q, want \"\"", got)
	}

	older := filepath.Join(cwd, ".aegis", "docs", "2026-07-01")
	newer := filepath.Join(cwd, ".aegis", "docs", "2026-08-01")
	for _, d := range []string{older, newer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A plain file matching the glob must not win over a directory.
	if err := os.WriteFile(filepath.Join(cwd, ".aegis", "docs", "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older, old, old); err != nil {
		t.Fatal(err)
	}
	if got := resolve(cwd); got != newer {
		t.Errorf("run dir = %q, want the newest matching directory %q", got, newer)
	}
}

// TestRunDirResolverRefusesEscape: `run_dir:` is read from a skill file, and
// `.aegis/skills/` is inside the workspace the model can write to — so a
// `../..` prefix must not aim the drive's phase prompts outside it.
func TestRunDirResolverRefusesEscape(t *testing.T) {
	parent := t.TempDir()
	cwd := filepath.Join(parent, "ws")
	outside := filepath.Join(parent, "elsewhere")
	for _, d := range []string{cwd, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := RunDirResolver("evil", "../elsewhere")(cwd); got != "" {
		t.Errorf("run dir = %q, want \"\" — a run_dir outside the workspace must be refused", got)
	}
}

// TestStateRunDirFallsBackToThreatModelLayout: State.RunDir left nil must keep
// the pre-P52.12 behaviour, since that is what every caller written before
// declared plans existed meant by leaving it unset.
func TestStateRunDirFallsBackToThreatModelLayout(t *testing.T) {
	cwd := t.TempDir()
	run := filepath.Join(cwd, ".aegis", "security", "threat-model", "2026-08-01")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "0-assessment.md"), []byte("# assessment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &State{Cwd: cwd}
	if got := st.runDir(); got != run {
		t.Errorf("run dir = %q, want %q", got, run)
	}
}
