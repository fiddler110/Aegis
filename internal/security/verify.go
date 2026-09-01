package security

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// P55.3: proving the built image's scanners actually work.
//
// MultiscannerTools(profile) is a static list. Everything downstream — which
// tools Resolve routes to the container, what `aegis security status` claims,
// what a scan reports — trusts it, and nothing has ever checked it against the
// image. Two failure modes were live simultaneously in the image shipped before
// this existed: grype was on the list and absent from the image, and kubescape
// was on the list, present, and fatal on every invocation.
//
// A version probe alone is not the fix, and the Containerfile now says so in
// prose next to njsscan: `njsscan --version` passed for the entire duration of
// a breakage that made every real njsscan run die, because --version never
// reaches the subprocess that was broken. The dangerous shape this package
// already documents for gosec and osv-scanner is the same one — a tool that
// exits 0 and reports zero findings because it never loaded its rules or its
// database. Nothing distinguishes that from a clean repo except running the
// tool against a tree that is definitely not clean.
//
// So verification is two steps per tool, and the second one is the point:
// probe the version (catches absence), then scan the embedded canary fixture
// and assert a NON-ZERO finding count (catches everything else).

// canaryFS is the deliberately-vulnerable fixture every canary scan runs
// against, embedded so `aegis security verify-image` works from an installed
// binary with no checkout — the same reason the Containerfile is embedded.
//
// Deliberately not under testdata/: the go tool excludes testdata directories
// from embed patterns, so a fixture that lived there would compile away to
// nothing and every canary would report "no files scanned" as a tool failure.
//
//go:embed canary
var canaryFS embed.FS

// canaryToolTimeout bounds one tool's canary scan. The fixture is a dozen tiny
// files, so any tool taking minutes on it is hung rather than working — and a
// verification command that can hang forever is not usable as a provisioning
// gate, which is the job.
const canaryToolTimeout = 5 * time.Minute

// VerifyStatus is one tool's outcome.
type VerifyStatus string

const (
	// VerifyPass means the tool ran and reported at least the expected number
	// of findings against the canary fixture.
	VerifyPass VerifyStatus = "pass"
	// VerifyFail means the tool is missing, errored, or — the case this whole
	// command exists for — ran cleanly and found nothing.
	VerifyFail VerifyStatus = "fail"
	// VerifySkip means no canary is possible for this tool, with a stated
	// reason. Never a silent omission (P11.1's posture).
	VerifySkip VerifyStatus = "skip"
	// VerifyBlocked means the tool couldn't be verified through no fault of its
	// own — currently only "its database isn't in the cache volume yet".
	// Distinct from VerifyFail so an operator who simply hasn't run
	// `aegis security update-db` isn't told their image is broken.
	VerifyBlocked VerifyStatus = "blocked"
)

// VerifyResult is one row of the verification table.
type VerifyResult struct {
	Tool string
	// Status is the outcome. Only VerifyFail is a gate failure.
	Status VerifyStatus
	// Version is the first line the tool's version probe printed, or "" when
	// no probe ran (an excluded tool) or it failed.
	Version string
	// Findings is what the canary scan reported; Expected is the minimum that
	// counted as working. Both zero when no canary ran.
	Findings int
	Expected int
	// Detail explains a non-pass status in the operator's terms: what was
	// expected, what happened, and what to do about it.
	Detail string
}

// canaryExpectation declares, as data, how one tool is verified. Kept as a
// table rather than a switch so adding a scanner to the image is one entry
// here plus whatever trips it in the fixture — and so the "is every tool
// covered?" question is a map lookup a test can enforce.
type canaryExpectation struct {
	// versionArgs is how this tool prints its version. Not uniformly
	// "--version": gitleaks/syft/grype/kubescape take a bare `version`
	// subcommand and nuclei takes single-dash `-version`.
	versionArgs []string
	// minFindings is the number of findings the canary fixture must produce.
	// One is the honest floor for almost everything — the assertion is "this
	// tool can still see something", not a detection-quality benchmark that
	// would go red on every upstream rule-set change.
	minFindings int
	// canarySkip, when non-empty, means this tool gets a version probe and no
	// canary, for the stated reason. A skip is reported, never silent.
	canarySkip string
}

// canaryExpectations is the single table driving verification. Every tool any
// profile's MultiscannerTools returns must appear here (enforced by test).
var canaryExpectations = map[string]canaryExpectation{
	"trivy":       {versionArgs: []string{"--version"}, minFindings: 1},
	"gitleaks":    {versionArgs: []string{"version"}, minFindings: 1},
	"trufflehog":  {versionArgs: []string{"--version"}, minFindings: 1},
	"syft":        {versionArgs: []string{"version"}, minFindings: 1},
	"osv-scanner": {versionArgs: []string{"--version"}, minFindings: 1},
	"grype":       {versionArgs: []string{"version"}, minFindings: 1},
	"kubescape":   {versionArgs: []string{"version"}, minFindings: 1},
	"hadolint":    {versionArgs: []string{"--version"}, minFindings: 1},
	"opengrep":    {versionArgs: []string{"--version"}, minFindings: 1},
	"bandit":      {versionArgs: []string{"--version"}, minFindings: 1},
	// gosec's canary is the one this whole command was designed around: it is
	// the tool that reports zero rather than failing when it cannot load
	// packages, so "did it find something in a module full of planted issues"
	// is the only question worth asking of it. The fixture trips half a dozen
	// independent rules, so 1 is a floor with a wide margin, not a tight
	// detection benchmark.
	"gosec":    {versionArgs: []string{"--version"}, minFindings: 1},
	"brakeman": {versionArgs: []string{"--version"}, minFindings: 1},
	"njsscan":  {versionArgs: []string{"--version"}, minFindings: 1},
	// nmap and nuclei take a network target, not a directory. There is no file
	// that can be planted in a fixture tree to make them report something, and
	// standing up a listener inside a --network none container to scan would be
	// testing the container's loopback rather than the tool. A version probe is
	// genuinely all that's available for these two, which is stated rather than
	// quietly counted as a pass.
	"nmap":   {versionArgs: []string{"--version"}, canarySkip: "nmap scans network targets, not a directory — no filesystem fixture can trip it, so only its version probe is verified"},
	"nuclei": {versionArgs: []string{"-version"}, canarySkip: "nuclei scans network targets, not a directory — no filesystem fixture can trip it, so only its version probe is verified"},
}

// canaryFindingCounter runs one tool against the materialized fixture and
// returns how many findings it reported.
type canaryFindingCounter func(ctx context.Context, dir string, rt sandbox.ContainerRuntime, image string, opts Options) (int, error)

// canaryRunnerFor is a seam over canaryRunner so a test can exercise the
// verification flow — statuses, messages, the gate's arithmetic — without a
// container runtime. The real canary is by definition an integration test; the
// reporting around it must not be.
var canaryRunnerFor = canaryRunner

// canaryRunner resolves the counter for one tool. Every entry routes through
// the same Scan implementation a real scan uses, so a canary pass is evidence
// about the path Aegis actually takes — not about a second, parallel
// invocation that could drift from it.
func canaryRunner(name string) (canaryFindingCounter, bool) {
	if name == "syft" {
		// syft has no Scanner implementation (it produces an SBOM, not
		// findings), so its canary counts catalogued components instead. Same
		// assertion in spirit: zero components from a tree with two lockfiles
		// means syft cataloged nothing.
		return func(ctx context.Context, dir string, _ sandbox.ContainerRuntime, _ string, opts Options) (int, error) {
			sbom, _, err := GenerateSBOM(ctx, dir, opts)
			if err != nil {
				return 0, err
			}
			return countSBOMComponents(sbom)
		}, true
	}
	for _, sc := range DefaultScanners() {
		if sc.Name() != name {
			continue
		}
		scanner := sc
		return func(ctx context.Context, dir string, rt sandbox.ContainerRuntime, image string, opts Options) (int, error) {
			findings, err := scanner.Scan(ctx, dir, MethodContainer, rt, image, opts)
			if err != nil {
				return 0, err
			}
			return len(findings), nil
		}, true
	}
	return nil, false
}

// countSBOMComponents counts the components in a CycloneDX SBOM.
func countSBOMComponents(sbom []byte) (int, error) {
	if len(strings.TrimSpace(string(sbom))) == 0 {
		return 0, fmt.Errorf("syft produced no output at all")
	}
	var doc struct {
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(sbom, &doc); err != nil {
		return 0, fmt.Errorf("parse syft SBOM: %w", err)
	}
	return len(doc.Components), nil
}

// canarySuffix marks a fixture file that must not exist under its real name
// inside this repository, and is renamed on materialization.
//
// Two unrelated reasons put files here, and both come down to the same thing: a
// manifest under its real name is read by tooling that has no idea this
// directory is a fixture.
//
//   - The Go toolchain (gosec's canary). The fixture needs a go.mod and a .go
//     file, and both are hostile to the tree they live in: a nested go.mod
//     excludes its directory from the parent module (and from the embed pattern
//     that has to reach it), and a .go file full of deliberate vulnerabilities
//     would otherwise be compiled — and scanned — as part of Aegis itself.
//
//   - GitHub's dependency graph (requirements.txt, package-lock.json). Those
//     are pinned to long-published advisories on purpose, so the graph reads
//     them as real dependencies and files a Dependabot alert per advisory —
//     eighteen of them, none actionable, since nothing ever installs from this
//     directory. Alerts are repo-wide advisory scanning and ignore
//     .github/dependabot.yml's `updates:` scoping, so excluding the path there
//     does not help; the manifest simply must not be recognizable. Bumping the
//     pins is the one fix that is actually wrong — it would leave the SCA
//     scanners finding nothing in the fixture built to prove they find things.
//
// Storing them under a suffix nothing recognizes and restoring the real name
// here keeps every one of those problems out of the repo without giving any
// scanner a fixture that differs from the real thing.
const canarySuffix = ".canary"

// MaterializeCanaryFixture writes the embedded canary tree into dir.
func MaterializeCanaryFixture(dir string) error {
	return fs.WalkDir(canaryFS, "canary", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel("canary", path)
		if relErr != nil {
			return relErr
		}
		if !d.IsDir() {
			rel = strings.TrimSuffix(rel, canarySuffix)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := canaryFS.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read embedded %s: %w", path, readErr)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// runCanaryVersion is a seam over the containerized version probe so tests can
// exercise the verification flow without a container runtime.
var runCanaryVersion = func(ctx context.Context, rt sandbox.ContainerRuntime, image, dir, binary string, args ...string) (string, error) {
	cliArgs := containerRunArgs(rt, image, dir, append([]string{binary}, args...)...)
	cmd := exec.CommandContext(ctx, string(rt), cliArgs...)
	// CombinedOutput rather than Output: trufflehog, njsscan and nuclei all
	// print their version banner to stderr, so a stdout-only read would report
	// an empty version for three working tools.
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, firstLine(text))
	}
	return firstVersionLine(text), nil
}

// ansiEscapeRe matches the SGR colour sequences njsscan and nuclei wrap their
// banners in. Left in, they turn a table column into terminal control codes.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// versionTokenRe matches a dotted version number, with or without a leading v.
var versionTokenRe = regexp.MustCompile(`v?\d+\.\d+`)

// firstVersionLine reduces a version banner to one short, printable line.
//
// "First line" alone isn't enough for any of the interesting cases: njsscan
// leads with a blank line, nuclei with an ANSI-coloured banner, and syft and
// grype both open with an "Application: <name>" line and put the version on the
// next one. So this prefers the first line that actually contains a version
// number and only falls back to the first non-empty line.
func firstVersionLine(text string) string {
	text = ansiEscapeRe.ReplaceAllString(text, "")
	fallback := ""
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if versionTokenRe.MatchString(line) {
			return truncateVersion(line)
		}
		if fallback == "" {
			fallback = line
		}
	}
	return truncateVersion(fallback)
}

// truncateVersion keeps a banner line to a table-sized column. Several tools
// append a build commit, a config path, or a copyright line to the same line as
// the version.
func truncateVersion(s string) string {
	const max = 40
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// VerifyMultiscanner runs the version probe and canary scan for every tool the
// configured image claims to carry, returning one result per tool.
//
// only, when non-empty, restricts verification to those tool names — the
// iteration loop for someone fixing one scanner, since a full pass is a dozen
// container runs. A name that isn't in the image's tool list is an error rather
// than an empty table, so a typo can't read as "nothing to check".
//
// progress, when non-nil, is called with each result as it completes — a tool
// canary is a container run, so a table printed only at the end would look
// hung for minutes.
//
// The error return is reserved for conditions that make verification itself
// impossible (no image configured, no runtime, image ID mismatch). A tool
// failing is data, not an error: the caller reports the whole table and then
// decides the exit code, so one broken scanner never hides the state of the
// other thirteen.
func VerifyMultiscanner(ctx context.Context, p MultiscannerPolicy, only []string, progress func(VerifyResult)) ([]VerifyResult, error) {
	if !p.Enabled || p.Image == "" {
		return nil, fmt.Errorf("no multiscanner image configured — run `aegis security build-image` first")
	}
	rt, ok := detectRuntime(ctx, p.RuntimePriority())
	if !ok {
		if p.Runtime != "" {
			return nil, fmt.Errorf("the multiscanner image was built with %s, which isn't available now — start it (on Windows with Podman: `podman machine start`)", p.Runtime)
		}
		return nil, fmt.Errorf("no container runtime available (docker/podman)")
	}
	if reason := verifyMultiscannerImageID(ctx, rt, p); reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}

	names := make([]string, 0, len(p.Tools))
	for name, on := range p.Tools {
		if on {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(only) > 0 {
		filtered, err := filterVerifyTools(names, only)
		if err != nil {
			return nil, err
		}
		names = filtered
	}

	dir, err := os.MkdirTemp("", "aegis-canary-*")
	if err != nil {
		return nil, fmt.Errorf("create canary fixture directory: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := MaterializeCanaryFixture(dir); err != nil {
		return nil, fmt.Errorf("write canary fixture: %w", err)
	}

	// The scanners are driven through their normal Scan path, which needs an
	// Options whose Multiscanner policy is this one — that's what makes
	// usesMultiscanner true and gets the binary name prepended, the cache
	// volume mounted, and the baked rule packs selected.
	opts := Options{Multiscanner: p}

	results := make([]VerifyResult, 0, len(names))
	for _, name := range names {
		res := verifyOneTool(ctx, rt, p, opts, dir, name)
		results = append(results, res)
		if progress != nil {
			progress(res)
		}
	}
	return results, nil
}

// filterVerifyTools narrows the image's tool list to the requested names,
// rejecting any name the image doesn't claim — an unrecognized --tool must not
// quietly verify nothing.
func filterVerifyTools(names, only []string) ([]string, error) {
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}
	out := make([]string, 0, len(only))
	seen := map[string]bool{}
	for _, want := range only {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		if !have[want] {
			return nil, fmt.Errorf("the configured multiscanner image doesn't claim %q — it carries: %s", want, strings.Join(names, ", "))
		}
		if !seen[want] {
			seen[want] = true
			out = append(out, want)
		}
	}
	sort.Strings(out)
	return out, nil
}

// verifyOneTool is one row's worth of work: excluded-tool check, version probe,
// database-cache check, canary scan.
func verifyOneTool(ctx context.Context, rt sandbox.ContainerRuntime, p MultiscannerPolicy, opts Options, dir, name string) VerifyResult {
	res := VerifyResult{Tool: name}

	if why, excluded := multiscannerExcludedTools[name]; excluded {
		// Configured to be in the image but deliberately never built into it.
		// Not a failure of the image; a failure of the config that claims it,
		// so the reason names the actual problem.
		res.Status = VerifySkip
		res.Detail = "the multiscanner image deliberately doesn't carry " + name + ": " + why + " — remove it from security.multiscanner.tools"
		return res
	}

	exp, known := canaryExpectations[name]
	if !known {
		res.Status = VerifyFail
		res.Detail = "no canary expectation is defined for " + name + ", so nothing here can establish that it works — add an entry to canaryExpectations (internal/security/verify.go) and whatever trips it to the canary fixture"
		return res
	}

	probeCtx, cancel := context.WithTimeout(ctx, canaryToolTimeout)
	version, err := runCanaryVersion(probeCtx, rt, p.Image, dir, name, exp.versionArgs...)
	cancel()
	if err != nil {
		res.Status = VerifyFail
		res.Detail = fmt.Sprintf("version probe `%s %s` failed inside %s (%v) — the tool is missing from the image or cannot start; rebuild with `aegis security build-image`",
			name, strings.Join(exp.versionArgs, " "), p.Image, err)
		return res
	}
	res.Version = version

	if exp.canarySkip != "" {
		res.Status = VerifySkip
		res.Detail = exp.canarySkip
		return res
	}

	// Asked after the version probe on purpose: an empty cache says nothing
	// about whether the binary is there, and reporting "blocked" for a tool
	// that isn't even in the image would send the operator to update-db for a
	// problem update-db cannot fix.
	if reason := verifyMultiscannerCache(ctx, rt, name, p); reason != "" {
		res.Status = VerifyBlocked
		res.Detail = "cache not populated — run `aegis security update-db` (" + reason + ")"
		return res
	}

	run, ok := canaryRunnerFor(name)
	if !ok {
		res.Status = VerifyFail
		res.Detail = name + " has a canary expectation but no way to run it — canaryRunner (internal/security/verify.go) has no entry for it"
		return res
	}

	res.Expected = exp.minFindings
	scanCtx, cancel := context.WithTimeout(ctx, canaryToolTimeout)
	count, err := run(scanCtx, dir, rt, p.Image, opts)
	cancel()
	if err != nil {
		res.Status = VerifyFail
		res.Detail = fmt.Sprintf("ran but errored on the canary fixture: %v", err)
		return res
	}
	res.Findings = count
	if count < exp.minFindings {
		// The failure this command was built for. A tool that exits clean and
		// reports nothing against a tree full of planted vulnerabilities has
		// not "found the code safe" — it never looked.
		res.Status = VerifyFail
		res.Detail = fmt.Sprintf("expected at least %d finding(s) on the canary fixture, got %d — it exited cleanly without detecting anything, which is the silent-all-clear shape (missing rules, missing database, or a broken invocation), not a clean fixture",
			exp.minFindings, count)
		return res
	}
	res.Status = VerifyPass
	return res
}

// VerifyCounts tallies a result set by status.
//
// Only Failed makes verification fail as a gate. A skip is a stated
// impossibility and a blocked row is an un-run `aegis security update-db`
// rather than a broken image — but neither is counted as passing either, since
// "verified" has to mean the canary actually fired.
func VerifyCounts(results []VerifyResult) (passed, failed, skipped, blocked int) {
	for _, r := range results {
		switch r.Status {
		case VerifyPass:
			passed++
		case VerifyFail:
			failed++
		case VerifySkip:
			skipped++
		case VerifyBlocked:
			blocked++
		}
	}
	return passed, failed, skipped, blocked
}
