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
func (opengrepScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	var out []byte
	var err error
	args := sastScanArgs()
	if method == MethodContainer {
		out, err = runContainerImage(ctx, rt, image, dir, append(args, "/src")...)
	} else {
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
func (semgrepScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	var out []byte
	var err error
	args := sastScanArgs()
	if method == MethodContainer {
		out, err = runContainerImage(ctx, rt, image, dir, append(args, "/src")...)
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

func (trivyScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	var out []byte
	var err error
	args := append([]string{"fs", "--format", "sarif", "--quiet"}, trivyScanArgs...)
	if method == MethodContainer {
		out, err = runContainerImage(ctx, rt, image, dir, append(args, "/src")...)
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
func (gitleaksScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	if method == MethodContainer {
		// /dev/stdout avoids needing a second bind mount for the report file:
		// every scanner container is a Linux image (Docker Desktop/Podman run
		// Linux containers even on a Windows/macOS host), so /dev/stdout
		// always exists there regardless of host OS.
		out, err := runContainerImage(ctx, rt, image, dir, "detect", "--source", "/src", "--no-git",
			"--report-format", "json", "--report-path", "/dev/stdout", "--exit-code", "0")
		if err != nil {
			return nil, err
		}
		return parseGitleaks(out)
	}

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

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseGitleaks(data)
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

// kubescape's --output flag writes a file rather than stdout (unlike
// semgrep/trivy, whose SARIF flag writes directly to stdout), so this
// mirrors gitleaks' report-file pattern: a real temp file on the host,
// /dev/stdout inside the container (every scanner container is Linux).
func (kubescapeScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	if method == MethodContainer {
		out, err := runContainerImage(ctx, rt, image, dir, "scan", "--format", "sarif", "--output", "/dev/stdout", "/src")
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

func (hadolintScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	files, err := findDockerfiles(dir)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, f := range files {
		var data []byte
		var err error
		if method == MethodContainer {
			data, err = runContainerImage(ctx, rt, image, dir, "--format", "sarif", "/src/"+f)
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

func (osvScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	var out []byte
	var err error
	args := append([]string{}, osvScanArgs...)
	if method == MethodContainer {
		out, err = runContainerImage(ctx, rt, image, dir, append(args, "/src")...)
	} else {
		out, err = runJSON(ctx, dir, "osv-scanner", append(args, ".")...)
	}
	if err != nil {
		return nil, err
	}
	return parseOSVScanner(out)
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
		out, err := runContainerImage(ctx, rt, image, dir, "dir:/src", "-o", "sarif")
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
func (gosecScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	if method == MethodContainer {
		out, err := runContainerImage(ctx, rt, image, dir, "-fmt=sarif", "-out=/dev/stdout", "./...")
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
func (banditScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	if method == MethodContainer {
		out, err := runContainerImage(ctx, rt, image, dir, "-r", "/src", "-f", "sarif", "-o", "/dev/stdout")
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
func (brakemanScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	if method == MethodContainer {
		out, err := runContainerImage(ctx, rt, image, dir, "/src", "-f", "sarif", "-o", "/dev/stdout")
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
func (njsscanScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string, _ Options) ([]Finding, error) {
	var out []byte
	var err error
	if method == MethodContainer {
		out, err = runContainerImage(ctx, rt, image, dir, "--sarif", "/src")
	} else {
		out, err = runJSON(ctx, dir, "njsscan", "--sarif", ".")
	}
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "njsscan")
}
