package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// maxVerifyRounds bounds the P39.6 verify-and-fix loop: after the drive's
// PENDING markers hit zero, the bundled phase-6 checks run; each failing round
// feeds the failure text back for an in-place fix and re-runs, but only this
// many times before stopping with the still-failing output surfaced. Small
// because a model that cannot clear the mechanical checks in a few tries is not
// going to, and each round is a full model turn.
const maxVerifyRounds = 3

// verifyScript is one bundled check the P39.6 drive-completion verifier runs
// against a completed run directory. args are appended after the <run-dir>
// positional (e.g. inventory.py's --check).
type verifyScript struct {
	file string
	args []string
}

// threatModelVerifyScripts is the phase-6 check triple the threat-modeling
// skill bundles (SKILL.md §5). P39.6 runs these when the drive's markers hit
// zero so the autonomous done-condition becomes "verifies clean" rather than
// merely "all markers filled": the duplicate threat ID, tier↔prerequisite
// mismatches, and stale counts that shipped uncaught in the 2026-07-21 P38.1
// re-test were every one of them flagged by verify.py — they only escaped
// because nothing ran it.
var threatModelVerifyScripts = []verifyScript{
	{file: "verify.py"},
	{file: "lint_dfd.py"},
	{file: "inventory.py", args: []string{"--check"}},
}

// verifySkillOutputs runs a preloaded skill's bundled verification scripts
// against its completed run directory and returns any failure text. ran is
// false when there is nothing to verify — the skill ships no verify.py, no run
// directory exists yet, or no python interpreter is on PATH — in which case the
// caller falls back to the pre-P39.6 behaviour of treating "all markers filled"
// as done. When ran is true and failures is empty, the suite verified clean.
func verifySkillOutputs(skillName, skillDir, cwd string) (failures string, ran bool) {
	if skillName == "" || skillDir == "" {
		return "", false
	}
	// Only the threat-modeling skill ships the mechanical phase-6 checks today.
	// Gate on the presence of its verifier so other multi-phase skills (e.g.
	// deep-research) keep the pre-P39.6 "markers cleared = done" behaviour.
	if _, err := os.Stat(filepath.Join(skillDir, "verify.py")); err != nil {
		return "", false
	}
	runDir := latestThreatModelRunDir(cwd)
	if runDir == "" {
		return "", false
	}
	py := pythonExe()
	if py == "" {
		return "", false
	}
	const maxFailureBytes = 6000
	var b strings.Builder
	for _, s := range threatModelVerifyScripts {
		script := filepath.Join(skillDir, s.file)
		if _, err := os.Stat(script); err != nil {
			continue // an optional check this skill build doesn't bundle
		}
		out, err := exec.Command(py, append([]string{script, runDir}, s.args...)...).CombinedOutput()
		ran = true
		if err != nil {
			// Non-zero exit means a check failed (exit 2 is a usage/IO error,
			// still worth surfacing). The script's own report names the failing
			// check with file:line evidence, so pass it through verbatim.
			cmdline := strings.TrimSpace(s.file + " <run-dir> " + strings.Join(s.args, " "))
			fmt.Fprintf(&b, "$ %s\n%s\n\n", cmdline, strings.TrimSpace(string(out)))
		}
	}
	failures = strings.TrimSpace(b.String())
	if len(failures) > maxFailureBytes {
		failures = failures[:maxFailureBytes] + "\n…(truncated)"
	}
	return failures, ran
}

// latestThreatModelRunDir returns the most-recently-modified threat-model run
// directory under <cwd>/.aegis (the one the just-completed drive wrote), or ""
// if none exists. A run directory is identified by containing 0-assessment.md,
// the first suite file scaffold.py writes; picking newest-mtime handles a
// workspace that accumulated several runs.
func latestThreatModelRunDir(cwd string) string {
	base := filepath.Join(cwd, ".aegis", "security", "threat-model")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var best string
	var bestMod time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "0-assessment.md")); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = dir, info.ModTime()
		}
	}
	return best
}

// pythonExe returns the path to a working python interpreter (python3
// preferred), or "" if none is usable — in which case the P39.6 verifier
// degrades to a no-op rather than failing the drive. Each candidate is probed
// with `--version`: this rejects Windows' `python`/`python3` App-execution-alias
// shim, which is on PATH even when no interpreter is installed and exits
// non-zero on every call — without this guard it would make every threat-model
// drive spuriously "fail verification" on a machine without real Python.
func pythonExe() string {
	for _, name := range []string{"python3", "python"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(p, "--version").Run(); err == nil {
			return p
		}
	}
	return ""
}

// verifyFixPrompt is the P39.6 continuation turn: the markers are all cleared
// but the bundled checks still fail, so name the exact failures and tell the
// model to fix them in place (not re-scaffold) so the next iteration re-runs the
// checks. Mirrors SKILL.md §5's "fix what any script flags, then re-run it until
// clean" round, done autonomously.
func verifyFixPrompt(failures string) string {
	return "The suite has no `<!-- PENDING -->` markers left, but the bundled phase-6 verification scripts still FAIL. Fix the exact problems below by editing the affected files in place — do not re-scaffold and do not add new `<!-- PENDING -->` markers — then stop; the run re-verifies automatically:\n\n" +
		failures +
		"\n\nEdit the named files with `edit_file` to resolve every failing check now. This is a non-interactive run: make the fixes and do not ask whether to proceed."
}
