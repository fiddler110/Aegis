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

// --- semgrep ---

type semgrepScanner struct{}

func (semgrepScanner) Name() string { return "semgrep" }
func (semgrepScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "semgrep", opts)
}
func (semgrepScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string) ([]Finding, error) {
	var out []byte
	var err error
	if method == MethodContainer {
		out, err = runContainerImage(ctx, rt, image, dir, "--sarif", "--quiet", "--config", "auto", "/src")
	} else {
		out, err = runJSON(ctx, dir, "semgrep", "--sarif", "--quiet", "--config", "auto", ".")
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

func (trivyScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string) ([]Finding, error) {
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
func (gitleaksScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string) ([]Finding, error) {
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
func (kubescapeScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string) ([]Finding, error) {
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

func (hadolintScanner) Scan(ctx context.Context, dir string, method Method, rt sandbox.ContainerRuntime, image string) ([]Finding, error) {
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
