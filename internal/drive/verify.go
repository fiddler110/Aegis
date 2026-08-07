package drive

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

// MaxVerifyRounds bounds the P39.6 verify-and-fix loop: after the drive's
// PENDING markers hit zero, the bundled phase-6 checks run; each failing round
// feeds the failure text back for an in-place fix and re-runs, but only this
// many times before stopping with the still-failing output surfaced. Small
// because a model that cannot clear the mechanical checks in a few tries is not
// going to, and each round is a full model turn.
const MaxVerifyRounds = 3

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
//
// `inventory.py --check` deliberately stays in the triple even though the
// sidecar it validates is now regenerated from the same documents moments
// earlier (see regenerateInventorySidecar) — see that function's comment for why
// the comparison is not entirely tautological.
var threatModelVerifyScripts = []verifyScript{
	{file: "verify.py"},
	{file: "lint_dfd.py"},
	{file: inventoryScript, args: []string{"--check"}},
}

// inventoryScript is the bundled builder for the `inventory.yaml` sidecar. It
// has two modes: bare `<run-dir>` derives and writes the sidecar from the run's
// markdown, `--check` validates an existing sidecar against that same markdown.
// The drive runs both, in that order, on every verify round.
const inventoryScript = "inventory.py"

// VerifySkillOutputs runs a preloaded skill's bundled verification scripts
// against its completed run directory and returns any failure text. ran is
// false when there is nothing to verify — the skill ships no verify.py, no run
// directory exists yet, or no python interpreter is on PATH — in which case the
// caller falls back to the pre-P39.6 behaviour of treating "all markers filled"
// as done. When ran is true and failures is empty, the suite verified clean.
//
// Before the checks run it regenerates the derived `inventory.yaml` sidecar
// (regenerateInventorySidecar), so what the checks read is always what the
// markdown says — never what the model typed into the sidecar by hand.
func VerifySkillOutputs(skillName, skillDir, cwd string) (failures string, ran bool) {
	if skillName == "" || skillDir == "" {
		return "", false
	}
	// Only the threat-modeling skill ships the mechanical phase-6 checks today.
	// Gate on the presence of its verifier so other multi-phase skills (e.g.
	// deep-research) keep the pre-P39.6 "markers cleared = done" behaviour.
	if _, err := os.Stat(filepath.Join(skillDir, "verify.py")); err != nil {
		return "", false
	}
	runDir := LatestRunDir(cwd)
	if runDir == "" {
		return "", false
	}
	py := pythonExe()
	if py == "" {
		return "", false
	}
	const maxFailureBytes = 6000
	var b strings.Builder
	// Rebuild the derived sidecar first, so every check below reads a sidecar
	// that provably came from the markdown on disk. This sits
	// inside VerifySkillOutputs rather than beside normalizeSkillIDs in drive.go
	// on purpose — see regenerateInventorySidecar. Unlike the ID normalizer, a
	// failure here is NOT best-effort: it is folded into the failure report so
	// the drive's bounded fix loop acts on it.
	if _, regenErr := regenerateInventorySidecar(skillDir, runDir, py); regenErr != nil {
		// ran=true even if every script below is somehow skipped: we are
		// returning failure text, and a caller told ran=false discards it.
		ran = true
		fmt.Fprintf(&b, "$ %s <run-dir>\n%v\n\n", inventoryScript, regenErr)
	}
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

// normalizeSkillIDs runs the bundled deterministic ID canonicalizer
// (normalize_ids.py, P50.2) against the run directory in write mode, best
// effort. It strips invented `T<n>.<suffix>` threat-ID forms back to the bare
// `T<n>` the analysis defines and renumbers `FIND-##` to a gapless sequence,
// rewriting every cross-reference in lockstep — the two defects that otherwise
// cost extra verify rounds (invented IDs) and let the quality pass regress a
// clean suite (hand-renumber). Running it before verify.py turns that drift into
// a deterministic auto-fix instead of a model round. It is idempotent (a
// canonical suite is a no-op). ran reports whether the normalizer actually
// executed; err carries a non-zero exit (a usage/IO error — a canonicalization
// no-op exits 0) for the caller to log, never to fail the drive on. The whole
// thing degrades to a no-op when the script isn't bundled (older skill build)
// or python is absent, so it is safe to call unconditionally.
func normalizeSkillIDs(skillName, skillDir, cwd string) (ran bool, err error) {
	if skillName == "" || skillDir == "" {
		return false, nil
	}
	script := filepath.Join(skillDir, "normalize_ids.py")
	if _, e := os.Stat(script); e != nil {
		return false, nil // not bundled in this skill build
	}
	runDir := LatestRunDir(cwd)
	if runDir == "" {
		return false, nil
	}
	py := pythonExe()
	if py == "" {
		return false, nil
	}
	if out, e := exec.Command(py, script, runDir).CombinedOutput(); e != nil {
		return true, fmt.Errorf("normalize_ids.py: %v: %s", e, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// regenerateInventorySidecar rebuilds the run's `inventory.yaml` from the run's
// own markdown by running the bundled inventory.py in generate mode (no
// `--check`), immediately before the phase-6 checks read it.
//
// Why this exists. `inventory.yaml` is a *derived* artifact: inventory.py's own
// docstring says it builds the sidecar "directly from the run's own markdown
// files, never from the model's memory", and phasePromptAssessment already tells
// the model in bold terms to run inventory.py and to NOT hand-write the sidecar.
// That prompt-level fix demonstrably failed. In an instrumented 142-minute
// unattended threat-model drive the model hand-edited inventory.yaml anyway (six
// `edit_file` calls, alongside three inventory.py runs and seven ad-hoc
// `python3 -c` sys.path hacks), and the resulting sidecar/doc drift —
//
//	FAIL  components match 0.1-architecture.md
//	  present in inventory, not in docs: Azure
//
// — was a large part of a phase-6 verify tail that cost 49 minutes, 35% of the
// whole run, to clear a purely mechanical inconsistency that should never have
// been expressible. A prompt cannot make an artifact underivable; regenerating
// it can. Whatever the model did to the sidecar is now irrelevant: it is
// overwritten before anything reads it.
//
// Where it runs, and why here. Two constraints fix the position:
//
//   - After ID normalization. normalize_ids.py canonicalizes threat/finding IDs
//     *in the markdown*; a sidecar built before it would be derived from
//     IDs that are about to change. drive.go calls normalizeSkillIDs at the top
//     of each phase-6 iteration, immediately before VerifySkillOutputs, and this
//     runs as the first step of VerifySkillOutputs, so normalize-then-regenerate
//     holds by construction on every round.
//   - Before every check round, not once. normalizeSkillIDs is called once per
//     phase-6 iteration from drive.go, but VerifySkillOutputs is called from
//     seven places as the bounded fix loop and the P47.9 content re-entries
//     iterate — and each of those rounds is a model turn that just edited the
//     markdown. Regenerating once would leave the sidecar stale again the moment
//     the first fix round landed, reintroducing exactly the drift this removes,
//     only later in the run. Living inside VerifySkillOutputs makes "the checks
//     never read a stale sidecar" a property of the function rather than of
//     seven call sites, and needs no drive.go change. The cost is trivial:
//     inventory.py is a stdlib-only parse of a handful of markdown files, it is
//     deterministic (every field comes from the docs or from git, none from the
//     clock) and therefore idempotent, so a regeneration that changes nothing
//     writes byte-identical bytes and leaves SuiteFingerprint — and the quality
//     stamp keyed to it — unmoved.
//
// On the tautology. Regenerating the sidecar from the docs immediately before
// `inventory.py --check` compares sidecar to docs does make most of that check
// unfailable. That is the intent, not an oversight: the drift it was detecting
// was hand-authoring of a derived file, which is now structurally impossible, so
// comparing a derivation against its own source is tautological *by design*. The
// check nonetheless stays in the triple, because two of its ten checks are
// assertions about the *markdown*, not about the sidecar, and survive
// regeneration intact:
//
//   - "analysis file (2-*-analysis.md) present" — generate mode only writes a
//     stderr warning and exits 0 when no analysis file parses, so without
//     --check a suite with an unparseable analysis file would regenerate an
//     empty threats list and sail through.
//   - "references resolve to existing ids" — after regeneration this compares
//     the generated sidecar against itself, which makes it a *cross-document*
//     consistency check: a flow in 1-model.md naming an endpoint that
//     0.1-architecture.md's Key Components table does not define still fails.
//
// The remaining checks become fast no-ops. Dropping --check to buy that would
// trade real signal for a few hundred milliseconds.
//
// What is not tautological is a regeneration that *fails* — a python error, a
// table the parser cannot read, a non-zero exit — because then nothing knows
// what the docs say. That is why err here is surfaced by the caller as a verify
// failure the bounded fix loop acts on, deliberately unlike normalizeSkillIDs,
// whose failure is genuinely non-fatal (a suite with un-canonicalized IDs is
// still a suite, and verify.py still gates it). The returned error carries the
// script's own output plus a pointer at the real remedy, since the usual cause
// is a malformed table in the markdown and the usual wrong instinct is to
// "fix" the sidecar.
//
// Degradation matches the rest of this file: an older skill build that bundles
// no inventory.py, no run directory, and no python interpreter are all silent
// no-ops (ran=false, err=nil), so this is safe to call unconditionally. It takes
// runDir and py already resolved rather than re-deriving them like
// normalizeSkillIDs does, because its only caller has just resolved both and
// re-probing `python --version` on every verify round would be waste.
func regenerateInventorySidecar(skillDir, runDir, py string) (ran bool, err error) {
	if skillDir == "" || runDir == "" || py == "" {
		return false, nil
	}
	script := filepath.Join(skillDir, inventoryScript)
	if _, e := os.Stat(script); e != nil {
		return false, nil // not bundled in this skill build
	}
	if out, e := exec.Command(py, script, runDir).CombinedOutput(); e != nil {
		return true, fmt.Errorf("FAIL regenerating inventory.yaml from the documents: %v\n%s\n"+
			"inventory.yaml is DERIVED from the markdown and is rebuilt automatically before every check — "+
			"do not hand-write or edit it. This failure means the documents themselves could not be parsed: "+
			"fix the malformed table or section named above with `edit_file`.",
			e, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// suiteSnapshot captures the current contents of a run directory's suite report
// files (its top-level .md/.mmd/.yaml files — the same set SuiteFingerprint
// folds), keyed by basename. It backs the P50.3 quality-pass regression guard:
// the snapshot is taken at the moment the mechanical checks first pass (a
// known-clean state) so the drive can roll back to it if the quality pass edits
// the suite into a state the bounded fix rounds cannot re-clean. The
// `.quality-stamp.json` is excluded (its .json ext is not in the set), so a
// rollback never resurrects a stale stamp.
func suiteSnapshot(runDir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil, err
	}
	snap := make(map[string][]byte)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".md", ".mmd", ".yaml":
			data, err := os.ReadFile(filepath.Join(runDir, e.Name()))
			if err != nil {
				return nil, err
			}
			snap[e.Name()] = data
		}
	}
	return snap, nil
}

// restoreSuiteSnapshot writes a previously captured suite snapshot back over the
// run directory, undoing any edits made since it was taken. Used by the P50.3
// guard to roll the suite back to the known-clean pre-quality-pass state rather
// than ship a suite the quality pass regressed. It only rewrites files whose
// contents changed, so a rollback that changes nothing touches no mtimes.
func restoreSuiteSnapshot(runDir string, snap map[string][]byte) error {
	for name, want := range snap {
		path := filepath.Join(runDir, name)
		if cur, err := os.ReadFile(path); err == nil && string(cur) == string(want) {
			continue
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// LatestRunDir returns the most-recently-modified threat-model run
// directory under <cwd>/.aegis (the one the just-completed drive wrote), or ""
// if none exists. A run directory is identified by containing 0-assessment.md,
// the first suite file scaffold.py writes; picking newest-mtime handles a
// workspace that accumulated several runs.
func LatestRunDir(cwd string) string {
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

// RunDirResolver returns the function a drive uses to locate the directory its
// phase globs are relative to, given the skill's name and its declared
// `run_dir:` frontmatter (empty when it declares none).
//
// This is the completion oracle's half of the P52.12 generalization, and
// without it the other half is inert: PlanFor will happily build a plan from
// any skill's `phases:`, but every phase resolves its files under a run
// directory, and that lookup used to be LatestRunDir unconditionally — the
// threat-model layout, keyed off a threat-model sentinel file. A
// documentation-as-code or latex-report plan therefore resolved "" forever, so
// no phase could ever report itself complete and each would burn its whole
// turn budget before the drive moved on. Declared phases would have "worked"
// in the sense that they ran.
//
// Three cases, in the order they are decided:
//   - a declared glob resolves to its newest matching directory (the "each run
//     scaffolds a fresh dated directory" pattern, generalized);
//   - threat-modeling with nothing declared keeps LatestRunDir exactly, since
//     its built-in plan and its verifier both assume that layout;
//   - anything else treats the workspace root as the run directory, which is
//     what a skill writing to fixed paths means by a relative glob.
func RunDirResolver(skillName, declared string) func(cwd string) string {
	if declared = strings.TrimSpace(declared); declared != "" {
		return func(cwd string) string { return latestDirMatching(cwd, declared) }
	}
	if skillName == threatModelSkill {
		return LatestRunDir
	}
	return func(cwd string) string { return cwd }
}

// latestDirMatching returns the most-recently-modified directory matching a
// workspace-relative glob, or "" when none matches yet — which the drive reads
// as "not scaffolded", so the setup phase runs. A glob matching exactly one
// fixed path is the degenerate case and works the same way.
//
// A match outside the workspace is discarded. `run_dir:` comes from a skill
// file, which is content the model can write (`.aegis/skills/` is inside the
// workspace), so a `../..` prefix would otherwise aim the drive's phase prompts
// at a directory the workspace does not contain. The tools still enforce their
// own sandbox on every write; this keeps the drive from *naming* an escape in
// the first place.
func latestDirMatching(cwd, glob string) string {
	matches, err := filepath.Glob(filepath.Join(cwd, filepath.FromSlash(glob)))
	if err != nil {
		return ""
	}
	var best string
	var bestMod time.Time
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || !info.IsDir() || !withinRoot(cwd, m) {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = m, info.ModTime()
		}
	}
	return best
}

// withinRoot reports whether path is root itself or lies beneath it, comparing
// symlink-resolved forms so a workspace reached through a symlink (every /tmp
// or /var path on macOS) is not mistaken for an escape.
func withinRoot(root, path string) bool {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	root, path = resolve(root), resolve(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
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

// QualityReviewPrompt is the P38.1 final quality pass. The mechanical phase-6
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
func QualityReviewPrompt() string {
	return "The suite is structurally complete and the mechanical checks pass. Now do ONE final quality-and-sanity review of the finished threat model, reading each file, and fix any problem you find IN PLACE with `edit_file` (do not re-scaffold, do not add `<!-- PENDING -->` markers). " + phase6IncrementalEditRule + " Check specifically:\n" +
		"- Every threat and finding cites concrete evidence — a real `path:line` (or `path` + symbol) that exists in the codebase, not a vague reference like \"the code\" or \"various files\". Fix or remove any ungrounded claim.\n" +
		"- No filler, placeholder, TODO, \"lorem\", or restated-boilerplate text; each row says something specific to THIS system.\n" +
		"- Internal consistency: severities match the threat described; each finding's attack vector matches the component's real reachability (e.g. a background/internal-only component is not `AV:N` network-reachable); tiers match their prerequisites; summary-table counts match the actual rows.\n" +
		"- The DFD components, the architecture Key Components table, and the analysis sections name the same components — no orphan or missing component.\n" +
		"- No duplicate near-identical threats padding the counts.\n\n" +
		"Do NOT renumber `FIND-##` or `T#` identifiers or rewrite coverage/Related-Threats ID references by hand — a deterministic script (`normalize_ids.py`) canonicalizes all IDs automatically after this pass, and a manual renumber only risks a duplicate or a mismatched cross-reference. If a finding is mis-tiered, fix its Tier/CVSS content in place and leave its `FIND-##` heading alone. " +
		"This is a non-interactive run: make every fix now with `edit_file` and do not ask whether to proceed. If — after actually reading the files — everything is already correct, say so in one line and stop without editing."
}

// QualityStampFile is the completion stamp the phased drive writes inside a run
// directory once the suite has passed the expensive LLM quality pass and
// re-verified clean. It records a fingerprint of the exact on-disk suite so a
// re-run of an unchanged, already-reviewed suite can skip the ~25-30 minute
// quality pass instead of redoing it. It is deliberately invisible to every
// existing suite scanner: the `.json` extension is excluded from
// scanPendingMarkers / suiteFileCount / verify.py (which only read
// {.md,.mmd,.yaml,.yml,.txt}) and from SuiteFingerprint below, and the leading
// dot is extra safety. Its contents never contain the literal `<!-- PENDING`,
// so scanPendingMarkers can never trip on it either.
const QualityStampFile = ".quality-stamp.json"

// qualityStamp is the JSON body of QualityStampFile: the suite fingerprint at
// the moment the quality pass last verified clean, and when that happened.
type qualityStamp struct {
	Fingerprint string `json:"fingerprint"`
	ReviewedAt  string `json:"reviewed_at"`
}

// SuiteFingerprint returns a deterministic sha256 over the run directory's suite
// report files (its top-level `.md`, `.mmd`, and `.yaml` files). Each file
// contributes its name plus a sha256 of its contents, folded in sorted-name
// order so the result is stable across calls and independent of directory
// iteration order. Any edit to any suite file changes the fingerprint, so a
// stamp taken before the edit no longer matches and the quality pass re-fires.
// The `.quality-stamp.json` stamp itself is excluded (its `.json` ext is not in
// the set), so writing the stamp cannot change the fingerprint it records.
func SuiteFingerprint(runDir string) (string, error) {
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

// ReadQualityStamp reads and parses the run directory's completion stamp. ok is
// false when the stamp is absent, unreadable, malformed, or carries an empty
// fingerprint — in every such case the caller treats the suite as un-reviewed.
func ReadQualityStamp(runDir string) (stamp qualityStamp, ok bool) {
	data, err := os.ReadFile(filepath.Join(runDir, QualityStampFile))
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

// WriteQualityStamp records the given fingerprint as the run directory's
// completion stamp. Callers compute the fingerprint from the FINAL on-disk suite
// (after any quality-pass edits) immediately before writing so the stamp
// reflects exactly what was reviewed.
func WriteQualityStamp(runDir, fingerprint string) error {
	data, err := json.Marshal(qualityStamp{
		Fingerprint: fingerprint,
		ReviewedAt:  time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, QualityStampFile), data, 0o644)
}

// ShouldSkipQualityPass reports whether the run directory already carries a valid
// completion stamp whose fingerprint matches the current on-disk suite — i.e.
// the suite has already been quality-reviewed and nothing has changed since, so
// the expensive LLM quality pass can be skipped. It is the decision helper the
// phased drive's phase-6 loop gates the quality pass on (see
// runPhasedVerifyAndQuality). A missing/mismatched/unreadable stamp or an
// unreadable run dir returns false so the pass runs as it does today.
func ShouldSkipQualityPass(runDir string) bool {
	if runDir == "" {
		return false
	}
	stamp, ok := ReadQualityStamp(runDir)
	if !ok {
		return false
	}
	fp, err := SuiteFingerprint(runDir)
	if err != nil {
		return false
	}
	return stamp.Fingerprint == fp
}

// VerifyFixPrompt is the P39.6 continuation turn: the markers are all cleared
// but the bundled checks still fail, so name the exact failures and tell the
// model to fix them in place (not re-scaffold) so the next iteration re-runs the
// checks. Mirrors SKILL.md §5's "fix what any script flags, then re-run it until
// clean" round, done autonomously.
func VerifyFixPrompt(failures string) string {
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
