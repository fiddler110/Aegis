package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// qualityReviewPrompt is the P38.1 final quality pass. The mechanical phase-6
// scripts (verify.py/lint_dfd.py/inventory.py) verify structure and counts but
// cannot judge substance: whether every threat is grounded in real evidence,
// whether the prose is filler, whether the analysis is internally coherent. A
// content-rich local-model build reaches "scripts pass" with real quality gaps
// (vague evidence, copy-paste rows, severities that don't match the threat), so
// after the scripts are clean the drive runs exactly one substantive
// self-review turn that reads the finished suite and fixes what it finds in
// place. It is bounded to a single pass by the caller (a re-review every turn
// would never terminate), and the mechanical checks re-run afterward, so a
// review edit that breaks a script check is caught by the normal fix loop.
func qualityReviewPrompt() string {
	return "The suite is structurally complete and the mechanical checks pass. Now do ONE final quality-and-sanity review of the finished threat model, reading each file, and fix any problem you find IN PLACE with `edit_file` (do not re-scaffold, do not add `<!-- PENDING -->` markers). " + phase6IncrementalEditRule + " Check specifically:\n" +
		"- Every threat and finding cites concrete evidence — a real `path:line` (or `path` + symbol) that exists in the codebase, not a vague reference like \"the code\" or \"various files\". Fix or remove any ungrounded claim.\n" +
		"- No filler, placeholder, TODO, \"lorem\", or restated-boilerplate text; each row says something specific to THIS system.\n" +
		"- Internal consistency: severities match the threat described; each finding's attack vector matches the component's real reachability (e.g. a background/internal-only component is not `AV:N` network-reachable); tiers match their prerequisites; summary-table counts match the actual rows.\n" +
		"- The DFD components, the architecture Key Components table, and the analysis sections name the same components — no orphan or missing component.\n" +
		"- No duplicate near-identical threats padding the counts.\n\n" +
		"This is a non-interactive run: make every fix now with `edit_file` and do not ask whether to proceed. If — after actually reading the files — everything is already correct, say so in one line and stop without editing."
}

// qualityStampFile is the completion stamp the phased drive writes inside a run
// directory once the suite has passed the expensive LLM quality pass and
// re-verified clean. It records a fingerprint of the exact on-disk suite so a
// re-run of an unchanged, already-reviewed suite can skip the ~25-30 minute
// quality pass instead of redoing it. It is deliberately invisible to every
// existing suite scanner: the `.json` extension is excluded from
// scanPendingMarkers / suiteFileCount / verify.py (which only read
// {.md,.mmd,.yaml,.yml,.txt}) and from suiteFingerprint below, and the leading
// dot is extra safety. Its contents never contain the literal `<!-- PENDING`,
// so scanPendingMarkers can never trip on it either.
const qualityStampFile = ".quality-stamp.json"

// qualityStamp is the JSON body of qualityStampFile: the suite fingerprint at
// the moment the quality pass last verified clean, and when that happened.
type qualityStamp struct {
	Fingerprint string `json:"fingerprint"`
	ReviewedAt  string `json:"reviewed_at"`
}

// suiteFingerprint returns a deterministic sha256 over the run directory's suite
// report files (its top-level `.md`, `.mmd`, and `.yaml` files). Each file
// contributes its name plus a sha256 of its contents, folded in sorted-name
// order so the result is stable across calls and independent of directory
// iteration order. Any edit to any suite file changes the fingerprint, so a
// stamp taken before the edit no longer matches and the quality pass re-fires.
// The `.quality-stamp.json` stamp itself is excluded (its `.json` ext is not in
// the set), so writing the stamp cannot change the fingerprint it records.
func suiteFingerprint(runDir string) (string, error) {
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".md", ".mmd", ".yaml":
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\n%x\n", filepath.ToSlash(name), sum)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// readQualityStamp reads and parses the run directory's completion stamp. ok is
// false when the stamp is absent, unreadable, malformed, or carries an empty
// fingerprint — in every such case the caller treats the suite as un-reviewed.
func readQualityStamp(runDir string) (stamp qualityStamp, ok bool) {
	data, err := os.ReadFile(filepath.Join(runDir, qualityStampFile))
	if err != nil {
		return qualityStamp{}, false
	}
	if err := json.Unmarshal(data, &stamp); err != nil {
		return qualityStamp{}, false
	}
	if stamp.Fingerprint == "" {
		return qualityStamp{}, false
	}
	return stamp, true
}

// writeQualityStamp records the given fingerprint as the run directory's
// completion stamp. Callers compute the fingerprint from the FINAL on-disk suite
// (after any quality-pass edits) immediately before writing so the stamp
// reflects exactly what was reviewed.
func writeQualityStamp(runDir, fingerprint string) error {
	data, err := json.Marshal(qualityStamp{
		Fingerprint: fingerprint,
		ReviewedAt:  time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, qualityStampFile), data, 0o644)
}

// shouldSkipQualityPass reports whether the run directory already carries a valid
// completion stamp whose fingerprint matches the current on-disk suite — i.e.
// the suite has already been quality-reviewed and nothing has changed since, so
// the expensive LLM quality pass can be skipped. It is the decision helper the
// phased drive's phase-6 loop gates the quality pass on (see
// runPhasedVerifyAndQuality). A missing/mismatched/unreadable stamp or an
// unreadable run dir returns false so the pass runs as it does today.
func shouldSkipQualityPass(runDir string) bool {
	if runDir == "" {
		return false
	}
	stamp, ok := readQualityStamp(runDir)
	if !ok {
		return false
	}
	fp, err := suiteFingerprint(runDir)
	if err != nil {
		return false
	}
	return stamp.Fingerprint == fp
}

// verifyFixPrompt is the P39.6 continuation turn: the markers are all cleared
// but the bundled checks still fail, so name the exact failures and tell the
// model to fix them in place (not re-scaffold) so the next iteration re-runs the
// checks. Mirrors SKILL.md §5's "fix what any script flags, then re-run it until
// clean" round, done autonomously.
func verifyFixPrompt(failures string) string {
	return "The suite has no `<!-- PENDING -->` markers left, but the bundled phase-6 verification scripts still FAIL. Fix the exact problems below by editing the affected files in place — do not re-scaffold and do not add new `<!-- PENDING -->` markers — then stop; the run re-verifies automatically:\n\n" +
		failures +
		"\n\nEdit the named files with `edit_file` to resolve every failing check now. " + phase6IncrementalEditRule + " This is a non-interactive run: make the fixes and do not ask whether to proceed."
}

// phase6IncrementalEditRule carries the P39.14 anti-monolithic-write guardrail
// (the content-phase prompts' "one section, one edit … a monolithic write is
// slow and truncates" lesson) into the phase-6 verify-fix and quality prompts
// (P47.8). Without it the drive model, told only to "resolve every failing
// check", chose a single whole-file `write_file` of the ~400-line 3-findings.md
// to fill 15 empty finding bodies at once (2026-07-27, FirewallRiskRater) — the
// tool-call JSON truncated and overflowed the context. Fixing many sections is
// exactly when a monolithic rewrite is most tempting and most likely to
// truncate, so the phase-6 loop needs the same discipline the content phases
// carry. Pairs with the P47.7 overflow-reset: this reduces how often the
// overflow fires, P47.7 recovers when it still does.
const phase6IncrementalEditRule = "Make each fix as a small, targeted `edit_file` — one section or one row per edit; never regenerate a whole file in one call and never `write_file` a suite file (a monolithic write is slow and truncates into a malformed tool call)."
