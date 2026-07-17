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
func lookPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// runJSON runs a command and returns stdout. A non-zero exit is tolerated as
// long as output was produced, because scanners exit non-zero when they find
// issues.
func runJSON(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if len(out) == 0 && err != nil {
		return nil, err
	}
	return out, nil
}

func firstLine(s string) string {
	before, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(before)
}

// --- opengrep / semgrep (P11.3: pluggable SAST, opengrep default) ---
//
// Both engines are rule-syntax compatible and emit SARIF, so they share one
// arg builder and one pinned pack list. Packs are pinned explicitly — never
// "auto" — for reproducibility and supply-chain hygiene: `--config auto`
// needs network egress, nudges toward a platform login, and resolves
// whatever the registry currently serves for that pack name rather than a
// fixed rule set.
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

type semgrepScanner struct{}

func (semgrepScanner) Name() string { return "semgrep" }
func (semgrepScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "semgrep", opts)
}
func (semgrepScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	var out []byte
	var err error
	args := sastScanArgs()
	if method == MethodContainer {
		out, err = runScannerImage(ctx, rt, image, dir, opts, "semgrep", append(sastScanArgsFor(opts, image), "/src")...)
	} else {
		out, err = runJSON(ctx, dir, "semgrep", append(args, ".")...)
	}
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "semgrep")
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
var trivyScanArgs = []string{"--scanners", "vuln,secret,misconfig"}

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
		// /dev/stdout avoids needing a second bind mount for the report file:
		// every scanner container is a Linux image (Docker Desktop/Podman run
		// Linux containers even on a Windows/macOS host), so /dev/stdout
		// always exists there regardless of host OS.
		out, err := runScannerImage(ctx, rt, image, dir, opts, "gitleaks", "detect", "--source", "/src", "--no-git",
			"--report-format", "json", "--report-path", "/dev/stdout", "--exit-code", "0")
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
			Location:    fmt.Sprintf("%s:%d", filepath.ToSlash(d.File), d.StartLine),
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
			Location:     fmt.Sprintf("%s:%d", filepath.ToSlash(d.SourceMetadata.Data.Filesystem.File), d.SourceMetadata.Data.Filesystem.Line),
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
// semgrep/trivy, whose SARIF flag writes directly to stdout), so this
// mirrors gitleaks' report-file pattern: a real temp file on the host,
// /dev/stdout inside the container (every scanner container is Linux).
func (kubescapeScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		out, err := runScannerImage(ctx, rt, image, dir, opts, "kubescape", "scan", "--format", "sarif", "--output", "/dev/stdout", "/src")
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
		return nil, err
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
		out, err := runScannerImage(ctx, rt, image, dir, opts, "grype", "dir:/src", "-o", "sarif")
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

	out, err := runImageCmd(ctx, "grype", "dir:"+dir, "-o", "sarif")
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
// path in their own docs/examples (unlike semgrep/opengrep/njsscan, which
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
// also persist a copy to a file), so this follows the semgrep/opengrep/
// trivy stdout-SARIF pattern instead.

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
