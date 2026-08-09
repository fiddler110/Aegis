package drive

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/skills"
)

// The file tools refuse a write into the materialized built-in skill tree
// (see TestSkillAssetsAreReadOnlyToTools), but the shell tool can still reach
// it — a redirect or a `python -c` has no such gate. The drive repairs that at
// every phase boundary, because a phase starting against corrupted tooling is
// precisely the case that cannot recover on its own: observed live in the
// P38.1 re-test (2026-08-09), where a clobbered recon.py left every subsequent
// `python recon.py` raising SyntaxError with nothing pointing at the cause.
func TestRepairSkillAssetsRestoresClobberedScript(t *testing.T) {
	workDir := t.TempDir()
	if err := skills.MaterializeBuiltinsToProject(workDir, []string{"threat-modeling"}); err != nil {
		t.Fatal(err)
	}
	recon := filepath.Join(workDir, ".aegis", "builtin-skills", "threat-modeling", "recon.py")
	original, err := os.ReadFile(recon)
	if err != nil {
		t.Fatalf("expected threat-modeling to materialize recon.py: %v", err)
	}

	// Exactly what the model did: write the command line it meant to run.
	if err := os.WriteFile(recon, []byte("python recon.py <workdir>"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	st := &State{Cwd: workDir, ErrOut: &out, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	st.repairSkillAssets()

	got, err := os.ReadFile(recon)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("recon.py was not restored:\n got: %.80q\nwant: %.80q", got, original)
	}
	if !strings.Contains(out.String(), "recon.py") {
		t.Errorf("expected the notice to name the restored file, got %q", out.String())
	}

	// A second pass has nothing to do and must stay silent, so a clean drive
	// does not emit a notice at every phase boundary.
	out.Reset()
	st.repairSkillAssets()
	if out.Len() != 0 {
		t.Errorf("expected no notice when nothing is stale, got %q", out.String())
	}
}

// A blank Cwd (a host that never set one) must not panic or touch the disk.
func TestRepairSkillAssetsNoopWithoutCwd(t *testing.T) {
	var out bytes.Buffer
	st := &State{ErrOut: &out, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	st.repairSkillAssets()
	if out.Len() != 0 {
		t.Errorf("expected no output, got %q", out.String())
	}
}
