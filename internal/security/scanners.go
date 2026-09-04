package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// lookPath reports whether a binary is on PATH.
//
// A var rather than a plain func so a test can assert a resolution *rule*
// instead of asserting what happens to be installed on the machine running it —
// P34.7's lesson, and the same reason hostGOOS and detectRuntime are seams.
// "the container beat the host binary" is only a meaningful assertion if the
// host binary is known to be there.
var lookPath = func(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// runJSON runs a command and returns stdout. A non-zero exit is tolerated as
// long as output was produced, because scanners exit non-zero when they find
// issues.
//
// The converse — non-zero exit with *empty* stdout — is reported as a scan
// error, which is only correct if no scanner uses that combination to say
// "nothing here to analyze". That is the P34.6 failure shape: brakeman's
// accurate refusal on a non-Rails repo rendered as `brakeman: error: exit
// status 4`. P34.6 swept the language-targeted tools; the SCA/secrets half was
// swept separately (P54.2) by running each tool at its pinned version against
// an empty directory and a docs/C/shell tree with no dependency manifests:
//
//	trivy 0.72.0        exit 0, valid SARIF          — no gate needed
//	grype 0.115.0       exit 0, valid SARIF          — no gate needed
//	syft 1.46.0         exit 0, valid CycloneDX      — no gate needed
//	gitleaks 8.30.1     exit 0 (forced --exit-code 0, report file read
//	                    independently of Run()'s error)
//	trufflehog 3.95.9   exit 0, empty stdout — JSON Lines with zero lines is
//	                    how it says "no secrets"; empty+zero is the success
//	                    branch here, so it parses to zero findings
//	osv-scanner 2.4.0   exit 128, empty stdout — the one tool that does refuse
//	                    this way; interpreted in osv.go (P34.12), not here
//
// So osv-scanner remains the only SCA/secrets scanner needing interpretation,
// as brakeman was the only language-targeted one. Re-measure before adding a
// tool to DefaultScanners rather than assuming the pattern holds.
func runJSON(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	return runBoundedOutput(cmd)
}

func firstLine(s string) string {
	before, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(before)
}

// --- opengrep (P11.3: the SAST engine) ---
//
// Packs are pinned explicitly — never "auto" — for reproducibility and
// supply-chain hygiene: `--config auto` needs network egress, nudges toward a
// platform login, and resolves whatever the registry currently serves for that
// pack name rather than a fixed rule set.
var sastRulePacks = []string{"p/owasp-top-ten", "p/security-audit"}

func sastScanArgs() []string {
	args := []string{"--sarif", "--quiet"}
	for _, pack := range sastRulePacks {
		args = append(args, "--config", pack)
	}
	return args
}

type opengrepScanner struct{}

func (opengrepScanner) Name() string { return "opengrep" }
func (opengrepScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "opengrep", opts)
}
func (opengrepScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	var out []byte
	var err error
	args := sastScanArgs()
	switch method {
	case MethodContainer:
		out, err = runScannerImage(ctx, rt, image, dir, opts, "opengrep", append(sastScanArgsFor(opts, image), "/src")...)
	case MethodWSL:
		out, err = sandbox.RunWSLCommand(ctx, dir, opts.WSLDistro, "opengrep", append(args, ".")...)
	default:
		out, err = runJSON(ctx, dir, "opengrep", append(args, ".")...)
	}
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "opengrep")
}

// --- trivy ---

type trivyScanner struct{}

func (trivyScanner) Name() string { return "trivy" }
func (trivyScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "trivy", opts)
}

// trivyScanArgs are shared between host and container invocations: fs mode
// with all three trivy scanners explicit (P11.6) — vuln (SCA), secret, and
// misconfig (IaC across Terraform/CloudFormation/Kubernetes/Helm/Dockerfile/
// ARM) — rather than relying on whatever trivy's own version-dependent
// default scanner set happens to be.
//
// --include-dev-deps exists for the same reason the scanner list is explicit:
// trivy's default silently narrows the scan. It skips npm/yarn/gradle *dev*
// dependencies, which on this repo's own frontend lockfile means cataloging
// 1 of 140 packages (139 devDependencies + preact) — see P34.10.
//
// The usual argument for the default (dev deps don't ship, so their CVEs
// matter less) doesn't survive contact with the rest of the pipeline:
// osv-scanner already includes dev deps, so those findings reach the report
// regardless. Leaving trivy's default alone therefore buys no quiet — it only
// makes the two SCA scanners disagree about what the scan even covers, and
// costs the corroboration that dedup (P11.8) is built to merge. Measured on a
// lockfile with known-vulnerable dev deps (lodash 4.17.15, minimist 1.2.0):
// trivy reports 0 by default and 9 with this flag — including a CRITICAL
// (CVE-2021-44906) that osv-scanner was already reporting alone.
//
// Note this is npm/yarn/gradle-only upstream; it's a no-op for every other
// ecosystem trivy catalogs.
var trivyScanArgs = []string{"--scanners", "vuln,secret,misconfig", "--include-dev-deps"}

func (trivyScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	var out []byte
	var err error
	args := append([]string{"fs", "--format", "sarif", "--quiet"}, trivyScanArgs...)
	if method == MethodContainer {
		out, err = runScannerImage(ctx, rt, image, dir, opts, "trivy", append(args, "/src")...)
	} else {
		out, err = runJSON(ctx, dir, "trivy", append(args, ".")...)
	}
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "trivy")
}

// --- gitleaks ---

type gitleaksScanner struct{}

func (gitleaksScanner) Name() string { return "gitleaks" }
func (gitleaksScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "gitleaks", opts)
}
func (gitleaksScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		// Write the report to a real path inside the container and cat it,
		// rather than --report-path /dev/stdout the way gosec/bandit/brakeman
		// do. Those three genuinely stream to the pipe; gitleaks 8.30 does not
		// — it produces its report file by writing a temporary file and
		// renaming it into place, and a rename onto /dev/stdout replaces the
		// symlink instead of writing to the pipe. It succeeds, exits 0, and
		// emits nothing, so Aegis parsed empty output as "gitleaks: 0 secrets"
		// on a tree full of them: the silent all-clear this package exists to
		// rule out. Found by P55.3's gitleaks canary (0 findings against a
		// fixture with planted tokens gitleaks reports fine on the CLI).
		//
		// Same `;` rather than `&&` reasoning as kubescape: if gitleaks fails
		// for real, no report file exists, cat exits non-zero with empty
		// stdout, and runContainerCLI surfaces that as a scan error.
		out, err := runScannerImage(ctx, rt, image, dir, opts, "sh", "-c",
			"gitleaks detect --source /src --no-git --report-format json --report-path /tmp/gitleaks.json --exit-code 0 >/dev/null 2>&1; cat /tmp/gitleaks.json")
		if err != nil {
			return nil, err
		}
		return parseGitleaks(out)
	}

	return scanGitleaksHostDir(ctx, dir)
}

// scanGitleaksHostDir runs gitleaks natively against a directory on the host
// and parses its report. Factored out of gitleaksScanner.Scan's MethodHost
// case so ScanText (below) can point it at a scratch directory containing
// arbitrary text — e.g. a PR title/body — rather than a real project tree.
func scanGitleaksHostDir(ctx context.Context, dir string) ([]Finding, error) {
	data, err := runGitleaksHostDirReport(ctx, dir)
	if err != nil {
		return nil, err
	}
	return parseGitleaks(data)
}

// runGitleaksHostDirReport runs gitleaks natively against a directory on the
// host and returns the raw JSON report bytes, without parsing them into
// []Finding. Factored out of scanGitleaksHostDir (same command, same temp
// file dance) so RedactText (redact.go, P24.12 / FIND-09) can additionally
// recover the literal matched-secret text — gitleaks' "Secret" field, which
// parseGitleaks/Finding deliberately don't carry — needed to mask it.
func runGitleaksHostDirReport(ctx context.Context, dir string) ([]byte, error) {
	report, err := os.CreateTemp("", "gitleaks-*.json")
	if err != nil {
		return nil, err
	}
	path := report.Name()
	report.Close()
	defer os.Remove(path)

	cmd := exec.CommandContext(ctx, "gitleaks", "detect", "--source", dir, "--no-git",
		"--report-format", "json", "--report-path", path, "--exit-code", "0")
	_ = cmd.Run() // gitleaks writes findings to the report file regardless

	return os.ReadFile(path)
}

// ScanText runs the same gitleaks secret-detection machinery used for
// directory scans against an arbitrary in-memory string — e.g. a PR
// title/body composed by the model before it's sent to GitHub (P24.6 /
// FIND-13) — rather than a checked-out project tree. It is deliberately
// best-effort: if gitleaks isn't installed on the host, it returns (nil,
// nil) rather than an error, so callers never gain a hard dependency on
// gitleaks being present — the same tolerant posture as every other
// gitleaks invocation in this package (--exit-code 0, ignored Run() error).
func ScanText(ctx context.Context, text string) ([]Finding, error) {
	if !lookPath("gitleaks") {
		return nil, nil
	}

	dir, err := os.MkdirTemp("", "gitleaks-text-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "content.txt"), []byte(text), 0o600); err != nil {
		return nil, err
	}

	return scanGitleaksHostDir(ctx, dir)
}

func parseGitleaks(data []byte) ([]Finding, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, nil
	}
	var doc []struct {
		RuleID      string `json:"RuleID"`
		Description string `json:"Description"`
		File        string `json:"File"`
		StartLine   int    `json:"StartLine"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse gitleaks output: %w", err)
	}
	out := make([]Finding, 0, len(doc))
	for _, d := range doc {
		out = append(out, Finding{
			Tool:        "gitleaks",
			RuleID:      d.RuleID,
			Severity:    SevHigh, // leaked secrets are high severity by default
			Title:       firstNonEmpty(d.Description, "potential secret"),
			Location:    fmt.Sprintf("%s:%d", toForwardSlash(d.File), d.StartLine),
			Remediation: "rotate the exposed credential and remove it from the codebase",
		})
	}
	return out, nil
}

// --- trufflehog (P13.2) ---
//
// trufflehog's differentiator over gitleaks is live verification: 800+
// detectors can call the real provider API to confirm a found credential is
// still active. It runs alongside gitleaks (deduped via P11.8's
// DedupFindings), not as a replacement, and defaults to --no-verification —
// verification is a separate, explicit, host-only opt-in
// (security.tools.trufflehog.verify) since it makes real calls to
// third-party services using the actual discovered secret.

type trufflehogScanner struct{}

func (trufflehogScanner) Name() string { return "trufflehog" }

// Resolve wraps the generic resolver to add one hard constraint beyond what
// Resolve itself knows: verify:true must never run via a container. The
// container-scanner runner is network-isolated (--network none, matching
// every other scanner container's hardening posture), but verification's
// entire point is calling out to a live provider API — so rather than punch
// a network hole through that posture (the same call already made for image
// scanning, see imageContainerFallbackUnsupported), verify:true simply
// forces MethodHost or nothing.
func (trufflehogScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	method, rt, image, reason := Resolve(ctx, "trufflehog", opts)
	if method != MethodContainer {
		return method, rt, image, reason
	}
	if !opts.policyFor("trufflehog", false).Verify {
		return method, rt, image, reason
	}
	return MethodNone, "", "", "security.tools.trufflehog.verify is true, which requires host execution (verification calls real provider APIs and the scanner container runs with --network none) — set security.tools.trufflehog.method: host and install trufflehog natively, or disable verify"
}

func (trufflehogScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	verify := method == MethodHost && opts.policyFor("trufflehog", false).Verify

	args := []string{"filesystem", "--json"}
	if !verify {
		args = append(args, "--no-verification")
	}

	if method == MethodContainer {
		out, err := runScannerImage(ctx, rt, image, dir, opts, "trufflehog", append(args, "/src")...)
		if err != nil {
			return nil, err
		}
		return parseTrufflehog(out, verify)
	}

	out, err := runJSON(ctx, dir, "trufflehog", append(args, ".")...)
	if err != nil {
		return nil, err
	}
	return parseTrufflehog(out, verify)
}

// parseTrufflehog parses trufflehog's `--json` output: one JSON object per
// line (JSON Lines), not a single array/report file the way gitleaks/
// kubescape write — trufflehog streams a result the moment it's found.
// verifyAttempted must reflect whether the scan actually ran without
// --no-verification: trufflehog's own "Verified" field is always false when
// verification was never attempted, which is a different thing from "checked
// and confirmed inactive" — conflating the two would be a guessed claim the
// same way an unanalyzed Reachability must never render as "unreachable".
func parseTrufflehog(data []byte, verifyAttempted bool) ([]Finding, error) {
	var out []Finding
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var d struct {
			DetectorName   string `json:"DetectorName"`
			Verified       bool   `json:"Verified"`
			Redacted       string `json:"Redacted"`
			SourceMetadata struct {
				Data struct {
					Filesystem struct {
						File string `json:"file"`
						Line int    `json:"line"`
					} `json:"Filesystem"`
				} `json:"Data"`
			} `json:"SourceMetadata"`
		}
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			return nil, fmt.Errorf("parse trufflehog output: %w", err)
		}
		verification := VerificationUnknown
		if verifyAttempted {
			verification = VerificationUnverified
			if d.Verified {
				verification = VerificationVerified
			}
		}
		out = append(out, Finding{
			Tool:         "trufflehog",
			RuleID:       d.DetectorName,
			Severity:     SevHigh, // leaked secrets are high severity by default, same as gitleaks
			Title:        firstNonEmpty(d.DetectorName, "potential secret") + " (" + d.Redacted + ")",
			Location:     fmt.Sprintf("%s:%d", toForwardSlash(d.SourceMetadata.Data.Filesystem.File), d.SourceMetadata.Data.Filesystem.Line),
			Remediation:  "rotate the exposed credential and remove it from the codebase",
			Verification: verification,
		})
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// --- kubescape (P11.6) ---

type kubescapeScanner struct{}

func (kubescapeScanner) Name() string { return "kubescape" }
func (kubescapeScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "kubescape", opts)
}

// Relevant implements RelevanceChecker: kubescape analyzes Kubernetes
// manifests, so a repo with none has nothing for it to do.
func (kubescapeScanner) Relevant(dir string) (bool, string) {
	files, err := findK8sManifests(dir)
	if err != nil || len(files) == 0 {
		return false, "no Kubernetes manifests found in workspace"
	}
	return true, ""
}

// k8sManifestMaxFiles bounds how many YAML files findK8sManifests will open
// and read on a huge tree — a real k8s manifest usually turns up within the
// first few hundred YAML files; this is a safety cap, not a tuned budget.
const k8sManifestMaxFiles = 500

// k8sManifestMaxBytes bounds how large one of those YAML files may be before
// it is read (VULN-09/G122). k8sManifestMaxFiles bounds the count, which does
// nothing about a single enormous file: a rendered Helm chart or a CRD bundle
// runs to a few hundred KB, so anything past this is not a manifest we could
// usefully hand kubescape, and reading it whole to look for two substrings put
// it in the daemon's heap first.
const k8sManifestMaxBytes = 8 << 20

// findK8sManifests walks dir (bounded, skipping the same dependency/build/
// VCS directories DetectLanguages skips) for files kubescape would actually
// have something to analyze: a Helm/Kustomize marker file by name, or any
// .yaml/.yml file whose content contains both "apiVersion:" and "kind:" —
// the two fields every Kubernetes manifest declares, which a docker-compose
// file or other generic YAML config won't have together. Best-effort and
// approximate by design, same posture as DetectLanguages — good enough to
// decide whether kubescape has anything to do, not a manifest validator.
func findK8sManifests(dir string) ([]string, error) {
	var out []string
	seen := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if seen > k8sManifestMaxFiles {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if path != dir && detectLanguagesSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		lower := strings.ToLower(name)
		if lower == "chart.yaml" || lower == "kustomization.yaml" || lower == "kustomization.yml" {
			out = append(out, path)
			return nil
		}
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			return nil
		}
		seen++
		if info, infoErr := d.Info(); infoErr != nil || info.Size() > k8sManifestMaxBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), "apiVersion:") && strings.Contains(string(data), "kind:") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// kubescape's --output flag writes a file rather than stdout (unlike
// opengrep/trivy, whose SARIF flag writes directly to stdout), so this
// mirrors gitleaks' report-file pattern: a real temp file on the host,
// /dev/stdout inside the container (every scanner container is Linux).
func (kubescapeScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		// "framework allcontrols" rather than a bare scan, because the
		// container has no network and kubescape's policies come from the
		// cache baked into the image. Bare `kubescape scan` defaults to the
		// "workloadscan" framework, which fails from that cache with
		// "framework from file not matching" (fatal, exit 1, empty stdout)
		// even when workloadscan.json is present. allcontrols loads cleanly
		// offline and is the widest control set, so it's also the better
		// default: on a Dockerfile+manifest fixture it reported 24 results
		// where nsa reported 11 and mitre 1.
		// --output to a file inside the container and cat it, rather than
		// --output /dev/stdout: kubescape writes its human summary (a
		// box-drawing table and emoji) to stdout too, so /dev/stdout
		// interleaves that with the SARIF and the parse dies on the first
		// non-JSON byte ("invalid character 'â'"). Redirecting kubescape's own
		// stdout to /dev/null leaves only the report on the pipe.
		//
		// The trailing `;` rather than `&&` is deliberate: kubescape exits
		// non-zero whenever controls fail, which is the normal finding case.
		// If it fails for real the file won't exist, cat exits non-zero with
		// empty stdout, and runContainerCLI surfaces that as a scan error.
		out, err := runScannerImage(ctx, rt, image, dir, opts, "sh", "-c",
			"kubescape scan framework allcontrols --format sarif --output /tmp/ks.sarif /src >/dev/null 2>&1; cat /tmp/ks.sarif")
		if err != nil {
			return nil, err
		}
		return ParseSARIF(out, "kubescape")
	}
	if method == MethodWSL {
		out, err := sandbox.RunWSLCommand(ctx, dir, opts.WSLDistro, "kubescape", "scan", "--format", "sarif", "--output", "/dev/stdout", ".")
		if err != nil {
			return nil, err
		}
		return ParseSARIF(out, "kubescape")
	}

	report, err := os.CreateTemp("", "kubescape-*.sarif")
	if err != nil {
		return nil, err
	}
	path := report.Name()
	report.Close()
	defer os.Remove(path)

	cmd := exec.CommandContext(ctx, "kubescape", "scan", "--format", "sarif", "--output", path, dir)
	_ = cmd.Run() // kubescape exits non-zero when controls fail; it still writes the report

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	return ParseSARIF(data, "kubescape")
}

// --- hadolint (P11.5) ---

type hadolintScanner struct{}

func (hadolintScanner) Name() string { return "hadolint" }
func (hadolintScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "hadolint", opts)
}

// Relevant implements RelevanceChecker: hadolint lints Dockerfiles, so a
// repo with none has nothing for it to do.
func (hadolintScanner) Relevant(dir string) (bool, string) {
	files, err := findDockerfiles(dir)
	if err != nil || len(files) == 0 {
		return false, "no Dockerfile found in workspace"
	}
	return true, ""
}

// findDockerfiles walks dir for files hadolint should lint: "Dockerfile",
// any "Dockerfile.*" variant, and "*.dockerfile". Returned paths are
// relative to dir (forward-slash, so they work as both host args and
// container-mounted /src-relative paths).
func findDockerfiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.") || strings.HasSuffix(strings.ToLower(name), ".dockerfile") {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return nil
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (hadolintScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	files, err := findDockerfiles(dir)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, f := range files {
		var data []byte
		var err error
		if method == MethodContainer {
			data, err = runScannerImage(ctx, rt, image, dir, opts, "hadolint", "--format", "sarif", "/src/"+f)
		} else {
			data, err = runJSON(ctx, dir, "hadolint", "--format", "sarif", f)
		}
		if err != nil {
			return nil, err
		}
		findings, err := ParseSARIF(data, "hadolint")
		if err != nil {
			return nil, err
		}
		out = append(out, findings...)
	}
	return out, nil
}

// --- osv-scanner (P11.4, reachability P11.12) ---

type osvScanner struct{}

func (osvScanner) Name() string { return "osv-scanner" }
func (osvScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "osv-scanner", opts)
}

// osvScanArgs requests native JSON rather than SARIF: osv-scanner's
// --call-analysis reachability verdict (P11.12) is only present on the
// native JSON report's per-package groups, confirmed against the upstream
// google/osv-scanner source (see osv.go) — SARIF omits it entirely.
// --call-analysis=all is a no-op for ecosystems that don't support it
// (currently Go, on by default and govulncheck-backed; Rust and Java are
// experimental) rather than an error, so it's always safe to pass.
var osvScanArgs = []string{"--format", "json", "--call-analysis=all", "--recursive"}

func (osvScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	var out []byte
	var err error
	// root is what osv-scanner resolves its target to and therefore what its
	// report paths are prefixed with — the mount point inside a container, the
	// scanned directory on the host. parseOSVScanner trims it so locations
	// come out repo-relative like every SARIF scanner's (P34.8).
	args := append([]string{}, osvScanArgs...)
	root := dir
	if method == MethodContainer {
		// Without this the scan dies trying to reach api.osv.dev, since every
		// scanner container runs with --network none. Only the multiscanner
		// image is known to carry the offline database.
		if opts.usesMultiscanner(image) {
			args = append(args, multiscannerOfflineFlag)
		}
		root = "/src"
		out, err = runScannerImage(ctx, rt, image, dir, opts, "osv-scanner", append(args, "/src")...)
	} else {
		out, err = runJSON(ctx, dir, "osv-scanner", append(args, ".")...)
	}
	if err != nil {
		// Both runners surface an error only when stdout was empty, so
		// reaching here already means the "exit non-zero with output" case
		// osv-scanner uses for findings is ruled out. What's left is a
		// tree with nothing to scan, or a real failure (P34.12).
		return nil, interpretOSVError(err)
	}
	return parseOSVScanner(out, root)
}

// --- grype, directory/SBOM mode (P11.4) ---
//
// grype already exists as an ImageScanner (images.go, scans a built image by
// reference). This is the separate dir-oriented half: SCA over a source
// checkout's declared dependencies, sharing the same "grype" name since both
// report findings from the same underlying tool — they run in disjoint
// Report aggregations (RunWithOptions vs ScanImage) so the shared name never
// collides.

// scaBuildArtifactExcludes are the glob patterns grype and syft skip in dir:
// mode so a source SCA scan reports the project's *declared* dependencies, not
// the developer's local build output (P34.11).
//
// Both tools catalog dependencies partly by reading compiled binaries: syft
// inventories a Go executable's embedded module list, which is why a checkout
// carrying `go build` output cataloged 574 Go components against go.mod's 67 —
// and why grype then reported ~50 mostly-stdlib CVEs baked into those binaries
// by the toolchain, none of them a fact about the project's dependencies
// (P34.8). Excluding compiled artifacts collapses that back to the
// manifest-derived set: go.mod / package-lock.json etc. are not binaries, so
// real SCA coverage (the goldmark finding from go.mod) is untouched, while the
// machine-dependent binary-catalog noise disappears.
//
// Anchored with ./**/ so they match at any depth relative to the scan root, the
// form both tools' doublestar matcher expects. This catches binaries by
// extension; an extensionless Unix `go build` output can't be matched by a
// portable glob, but the measured noise on this repo was Windows .exe
// cross-build output, and build dirs are conventionally gitignored for the
// extensionless case.
var scaBuildArtifactExcludes = []string{
	"./**/*.exe",
	"./**/*.dll",
	"./**/*.so",
	"./**/*.dylib",
	"./**/*.test",
}

// scaExcludeArgs expands scaBuildArtifactExcludes into repeated --exclude
// flags, the form grype and syft both accept in dir: mode.
func scaExcludeArgs() []string {
	args := make([]string, 0, len(scaBuildArtifactExcludes)*2)
	for _, p := range scaBuildArtifactExcludes {
		args = append(args, "--exclude", p)
	}
	return args
}

type grypeDirScanner struct{}

func (grypeDirScanner) Name() string { return "grype" }
func (grypeDirScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "grype", opts)
}

// Scan prefers feeding grype a syft-generated CycloneDX SBOM over grype's own
// built-in directory cataloger (P11.4: "generate an SBOM with syft and feed
// grype from it, keeping the SBOM as a persisted supply-chain artifact") —
// this ties the CVE match to a standalone, reusable artifact written to
// dir/.aegis/sbom.cdx.json instead of a scan-only in-memory catalog, and lets
// other tooling (or a future re-scan) reuse the same SBOM without
// re-cataloging. Falls back to grype's direct "dir:" scan whenever syft isn't
// available, or if the SBOM-first run fails for any reason, so a missing/
// broken syft install never blackholes the whole SCA control.
//
// Scoped to grype's own host method: when grype itself resolves to a
// container, cross-mounting a host-generated SBOM into that container adds
// meaningful complexity for a currently-unconfigured combination (no built-in
// scanner ships a DefaultImage — P11.1), so the container path scans the
// bind-mounted directory directly instead. Revisit if container-mode grype
// becomes common.
func (grypeDirScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		args := append([]string{"dir:/src"}, scaExcludeArgs()...)
		args = append(args, "-o", "sarif")
		out, err := runScannerImage(ctx, rt, image, dir, opts, "grype", args...)
		if err != nil {
			return nil, err
		}
		return ParseSARIF(out, "grype")
	}

	if sbom, _, err := GenerateSBOM(ctx, dir, opts); err == nil && len(sbom) > 0 {
		WriteSBOMArtifact(dir, sbom)
		if findings, err := scanSBOMWithGrype(ctx, sbom); err == nil {
			return findings, nil
		}
	}

	args := append([]string{"dir:" + dir}, scaExcludeArgs()...)
	args = append(args, "-o", "sarif")
	out, err := runImageCmd(ctx, "grype", args...)
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "grype")
}

func scanSBOMWithGrype(ctx context.Context, sbom []byte) ([]Finding, error) {
	tmp, err := os.CreateTemp("", "sbom-*.cdx.json")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(sbom); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	out, err := runImageCmd(ctx, "grype", "sbom:"+path, "-o", "sarif")
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "grype")
}

// --- gosec, bandit, brakeman (P11.3: opt-in language-targeted SAST) ---
//
// All three document their SARIF formatter paired with an explicit output
// path in their own docs/examples (unlike opengrep/njsscan, which
// write SARIF straight to stdout), so these use the same "write to a real
// temp file on the host, /dev/stdout inside the container" pattern gitleaks
// and kubescape already established — every scanner container is Linux, so
// /dev/stdout always exists there regardless of host OS.

type gosecScanner struct{}

func (gosecScanner) Name() string { return "gosec" }
func (gosecScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "gosec", opts)
}
func (gosecScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		// Two phases, and the first one has to succeed: gosec cannot load
		// packages without a warm module cache, and it reports zero findings
		// rather than failing when it can't. See gosec.go for the full argument.
		if _, err := warmGoModuleCache(ctx, rt, image, dir); err != nil {
			return nil, err
		}
		out, err := runScannerImage(ctx, rt, image, dir, opts, "gosec", "-fmt=sarif", "-out=/dev/stdout", "./...")
		if err != nil {
			return nil, err
		}
		return ParseSARIF(out, "gosec")
	}
	return runHostToTempSARIF(ctx, dir, "gosec", func(path string) []string {
		return []string{"-fmt=sarif", "-out=" + path, "./..."}
	})
}

type banditScanner struct{}

func (banditScanner) Name() string { return "bandit" }
func (banditScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "bandit", opts)
}
func (banditScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		out, err := runScannerImage(ctx, rt, image, dir, opts, "bandit", "-r", "/src", "-f", "sarif", "-o", "/dev/stdout")
		if err != nil {
			return nil, err
		}
		return ParseSARIF(out, "bandit")
	}
	return runHostToTempSARIF(ctx, dir, "bandit", func(path string) []string {
		return []string{"-r", ".", "-f", "sarif", "-o", path}
	})
}

type brakemanScanner struct{}

func (brakemanScanner) Name() string { return "brakeman" }
func (brakemanScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "brakeman", opts)
}

// Relevant implements RelevanceChecker: brakeman only scans Rails
// applications, and unlike gosec/bandit/njsscan (which report zero findings
// on a project in the wrong language) it exits 4 with "Please supply the path
// to a Rails application" — an accurate refusal that reaches the report as
// "brakeman: error: exit status 4" and reads as a broken tool. Gating on the
// same thing brakeman itself checks turns that into an honest skip reason.
func (brakemanScanner) Relevant(dir string) (bool, string) {
	if !isRailsApp(dir) {
		return false, "no Rails application found in workspace"
	}
	return true, ""
}

// isRailsApp mirrors brakeman's own project check: a config/environment.rb,
// plus either config/application.rb (Rails 3+) or a Rakefile. Only the scan
// root is checked, deliberately — that's the path brakeman is handed, so a
// Rails app nested in a subdirectory is one brakeman would refuse too.
func isRailsApp(dir string) bool {
	if !isFile(filepath.Join(dir, "config", "environment.rb")) {
		return false
	}
	return isFile(filepath.Join(dir, "config", "application.rb")) || isFile(filepath.Join(dir, "Rakefile"))
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (brakemanScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		out, err := runScannerImage(ctx, rt, image, dir, opts, "brakeman", "/src", "-f", "sarif", "-o", "/dev/stdout")
		if err != nil {
			return nil, err
		}
		return ParseSARIF(out, "brakeman")
	}
	return runHostToTempSARIF(ctx, dir, "brakeman", func(path string) []string {
		return []string{".", "-f", "sarif", "-o", path}
	})
}

// runHostToTempSARIF runs toolName (binary name == toolName for all three
// callers) with cwd=dir, tolerating a non-zero exit — these tools commonly
// exit non-zero when findings exist, matching runJSON's/gitleaks'/
// kubescape's existing tolerance. buildArgs receives the temp report path
// and returns the full argument list (the caller decides where in that list
// the report-path flag belongs, since gosec/bandit/brakeman each place it
// differently).
func runHostToTempSARIF(ctx context.Context, dir, toolName string, buildArgs func(reportPath string) []string) ([]Finding, error) {
	report, err := os.CreateTemp("", toolName+"-*.sarif")
	if err != nil {
		return nil, err
	}
	path := report.Name()
	report.Close()
	defer os.Remove(path)

	cmd := exec.CommandContext(ctx, toolName, buildArgs(path)...)
	cmd.Dir = dir
	_ = cmd.Run() // these SAST tools exit non-zero when findings are found; the report file is still written

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	return ParseSARIF(data, toolName)
}

// --- njsscan (P11.3: opt-in Node.js SAST) ---
//
// Unlike gosec/bandit/brakeman above, njsscan's own docs show --sarif
// writing straight to stdout by default (an --output flag is only needed to
// also persist a copy to a file), so this follows the opengrep/trivy
// stdout-SARIF pattern instead.

type njsscanScanner struct{}

func (njsscanScanner) Name() string { return "njsscan" }
func (njsscanScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "njsscan", opts)
}
func (njsscanScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	var out []byte
	var err error
	if method == MethodContainer {
		out, err = runScannerImage(ctx, rt, image, dir, opts, "njsscan", "--sarif", "/src")
	} else {
		out, err = runJSON(ctx, dir, "njsscan", "--sarif", ".")
	}
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "njsscan")
}
