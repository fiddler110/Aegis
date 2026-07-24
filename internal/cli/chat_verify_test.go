package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// verifyFixPrompt must name the failures and forbid re-scaffolding / new markers
// so the model fixes in place and the next round re-verifies.
func TestVerifyFixPrompt(t *testing.T) {
	p := verifyFixPrompt("$ verify.py <run-dir>\nFAIL threat-coverage 3-findings.md:12")
	for _, want := range []string{"FAIL threat-coverage", "edit_file", "do not re-scaffold", "verification"} {
		if !strings.Contains(p, want) {
			t.Errorf("verifyFixPrompt missing %q in:\n%s", want, p)
		}
	}
}

// qualityReviewPrompt (P38.1 final pass) must ask for a substantive review the
// mechanical scripts can't do — groundedness, filler, internal consistency —
// fixed in place via edit_file, non-interactively.
func TestQualityReviewPrompt(t *testing.T) {
	p := qualityReviewPrompt()
	for _, want := range []string{"edit_file", "evidence", "consistency", "non-interactive", "PENDING"} {
		if !strings.Contains(p, want) {
			t.Errorf("qualityReviewPrompt missing %q in:\n%s", want, p)
		}
	}
}

// verifySkillOutputs must report ran=false (drive falls back to markers-cleared
// = done) when there is nothing to verify: no skill, no skill dir, or a skill
// dir with no verify.py.
func TestVerifySkillOutputsGate(t *testing.T) {
	cwd := t.TempDir()
	if _, ran := verifySkillOutputs("", "", cwd); ran {
		t.Error("empty skill name should not run verification")
	}
	emptySkill := t.TempDir()
	if _, ran := verifySkillOutputs("threat-modeling", emptySkill, cwd); ran {
		t.Error("skill dir without verify.py should not run verification")
	}
}

// latestThreatModelRunDir finds the newest run directory (one containing
// 0-assessment.md) under .aegis and ignores non-run directories; a missing tree
// is "".
func TestLatestThreatModelRunDir(t *testing.T) {
	cwd := t.TempDir()
	if got := latestThreatModelRunDir(cwd); got != "" {
		t.Errorf("no .aegis tree should yield \"\", got %q", got)
	}

	base := filepath.Join(cwd, ".aegis", "security", "threat-model")
	mk := func(name string, withAssessment bool, mod time.Time) string {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if withAssessment {
			p := filepath.Join(dir, "0-assessment.md")
			if err := os.WriteFile(p, []byte("# Assessment\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, mod, mod); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(dir, mod, mod); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	now := time.Now()
	mk("stride-old-2026-07-20-1000", true, now.Add(-2*time.Hour))
	newest := mk("stride-new-2026-07-21-1200", true, now)
	mk("not-a-run", false, now.Add(time.Hour)) // newer mtime but no 0-assessment.md

	if got := latestThreatModelRunDir(cwd); got != newest {
		t.Errorf("latestThreatModelRunDir = %q, want newest run %q", got, newest)
	}
}

// verifySkillOutputs runs the bundled scripts and surfaces failure text. Uses a
// stub verify.py so the test needs only a python interpreter (skipped if none).
func TestVerifySkillOutputsRuns(t *testing.T) {
	if pythonExe() == "" {
		t.Skip("no python interpreter on PATH")
	}
	cwd := t.TempDir()
	runDir := filepath.Join(cwd, ".aegis", "security", "threat-model", "stride-app-2026-07-21-0900")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "0-assessment.md"), []byte("# Assessment\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	skillDir := t.TempDir()
	// A stub verify.py that fails loudly; only verify.py exists, so the other
	// two scripts are skipped as "not bundled".
	stub := "import sys\nprint('FAIL demo-check run/x.md:1')\nsys.exit(1)\n"
	if err := os.WriteFile(filepath.Join(skillDir, "verify.py"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}

	failures, ran := verifySkillOutputs("threat-modeling", skillDir, cwd)
	if !ran {
		t.Fatal("expected verification to run")
	}
	if !strings.Contains(failures, "FAIL demo-check") {
		t.Errorf("expected failure text passed through, got:\n%s", failures)
	}

	// A passing verify.py yields ran=true, no failures.
	pass := "print('PASS all')\n"
	if err := os.WriteFile(filepath.Join(skillDir, "verify.py"), []byte(pass), 0o644); err != nil {
		t.Fatal(err)
	}
	failures, ran = verifySkillOutputs("threat-modeling", skillDir, cwd)
	if !ran || failures != "" {
		t.Errorf("clean verify: ran=%v failures=%q, want ran=true, no failures", ran, failures)
	}
}
