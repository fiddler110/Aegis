package security

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// Method identifies how a scanner ran (or would run).
type Method string

const (
	MethodHost      Method = "host"      // native binary on PATH
	MethodContainer Method = "container" // via a pinned image on a detected container runtime
	MethodNone      Method = ""          // unavailable — see the reason string returned alongside it
)

// ScannerDescriptor documents one scanner for provisioning and configuration
// purposes (P11.10/P11.11): a plain-language summary an operator sees before
// any install, the host binary name, and — when configured — a container
// image to fall back to when that binary isn't installed.
//
// DefaultImage is deliberately empty for every built-in scanner. A scanner
// container image is itself supply-chain attack surface (P7.6's
// content-hash / trust-on-first-use posture applies equally here), so it
// must be pinned by digest — and this codebase has no way to verify a
// current, correct digest at commit time (digests rotate with every image
// republish; baking in a guessed or stale one would be worse than requiring
// explicit configuration, since a wrong digest either fails loudly on pull
// or — worse — silently pins something no longer maintained). An operator
// enables container fallback for a tool by setting
// security.tools.<name>.image to a verified `image@sha256:...` reference
// (see docs/security.md for how to obtain one); until then, Resolve reports
// MethodNone with a reason that says exactly that, never a silent skip.
type ScannerDescriptor struct {
	Name         string
	Binary       string
	Summary      string
	Category     string
	DefaultImage string
	Install      map[string]string // OS ("darwin"/"linux"/"windows") -> human install command
}

// descriptors is keyed by scanner name so provisioning/config code (P11.10,
// P11.11) can look one up without depending on the Scanner implementations.
var descriptors = map[string]ScannerDescriptor{
	"semgrep": {
		Name:     "semgrep",
		Binary:   "semgrep",
		Category: "SAST",
		Summary:  "Static analysis — scans source code for security bugs and anti-patterns using pattern-based rules across 30+ languages.",
		Install: map[string]string{
			"darwin":  "brew install semgrep",
			"linux":   "pipx install semgrep",
			"windows": "pipx install semgrep",
		},
	},
	"trivy": {
		Name:     "trivy",
		Binary:   "trivy",
		Category: "SCA / IaC / secrets",
		Summary:  "Scans dependencies for known CVEs, Terraform/Kubernetes/Dockerfile configs for misconfigurations, and the filesystem for hardcoded secrets.",
		Install: map[string]string{
			"darwin":  "brew install trivy",
			"linux":   "curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin",
			"windows": "scoop install trivy",
		},
	},
	"gitleaks": {
		Name:     "gitleaks",
		Binary:   "gitleaks",
		Category: "Secrets",
		Summary:  "Scans the working tree for hardcoded credentials (API keys, tokens, private keys) using regex + entropy detection rules.",
		Install: map[string]string{
			"darwin":  "brew install gitleaks",
			"linux":   "install the release binary from https://github.com/gitleaks/gitleaks/releases",
			"windows": "scoop install gitleaks",
		},
	},
	"kubescape": {
		Name:     "kubescape",
		Binary:   "kubescape",
		Category: "IaC / Kubernetes",
		Summary:  "Scans Kubernetes manifests/Helm charts for misconfigurations against NSA/MITRE/CIS framework controls, with proper severity (unlike lint-style tools such as kube-linter).",
		Install: map[string]string{
			"darwin": "brew install kubescape",
			"linux":  "curl -s https://raw.githubusercontent.com/kubescape/kubescape/master/install.sh | /bin/bash",
		},
	},
	"hadolint": {
		Name:     "hadolint",
		Binary:   "hadolint",
		Category: "Container",
		Summary:  "Lints Dockerfiles for best-practice violations (e.g. missing pinned base image tags, unsafe ADD usage, running as root).",
		Install: map[string]string{
			"darwin":  "brew install hadolint",
			"linux":   "install the release binary from https://github.com/hadolint/hadolint/releases",
			"windows": "scoop install hadolint",
		},
	},
	"grype": {
		Name:     "grype",
		Binary:   "grype",
		Category: "Container",
		Summary:  "Scans a container image's layers for known CVEs in installed packages (Anchore's vulnerability database).",
		Install: map[string]string{
			"darwin":  "brew install grype",
			"linux":   "curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin",
			"windows": "scoop install grype",
		},
	},
	"dockle": {
		Name:     "dockle",
		Binary:   "dockle",
		Category: "Container",
		Summary:  "Checks a container image against CIS Docker Benchmark / best-practice rules (e.g. no secrets baked in, non-root user, minimal layers).",
		Install: map[string]string{
			"darwin":  "brew install goodwithtech/r/dockle",
			"linux":   "install the release binary from https://github.com/goodwithtech/dockle/releases",
			"windows": "scoop install dockle",
		},
	},
}

// Descriptors returns every built-in scanner descriptor, sorted by name.
func Descriptors() []ScannerDescriptor {
	out := make([]ScannerDescriptor, 0, len(descriptors))
	for _, d := range descriptors {
		out = append(out, d)
	}
	sortDescriptors(out)
	return out
}

// DescriptorFor looks up one scanner's descriptor by name.
func DescriptorFor(name string) (ScannerDescriptor, bool) {
	d, ok := descriptors[name]
	return d, ok
}

func sortDescriptors(d []ScannerDescriptor) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j].Name < d[j-1].Name; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

// ToolPolicy controls how one scanner may run. Deliberately decoupled from
// internal/config the way internal/sandbox is decoupled from it — the caller
// (the security_scan tool, `aegis scan`/`aegis security`) translates
// config.SecurityToolConfig into this.
type ToolPolicy struct {
	// Enabled defaults to true; set false to always skip this tool.
	Enabled bool
	// Method is "host" (never fall back to a container), "container" (always
	// prefer the container image over a present host binary), or "auto"/""
	// (host if present, else container) — the default.
	Method string
	// Image overrides DefaultImage when set. Must be a digest-pinned
	// reference (image@sha256:...) for the same reason DefaultImage is never
	// baked in — see ScannerDescriptor's doc comment.
	Image string
}

// Options bundles per-tool policy for a scan run (the P11.11 config surface).
type Options struct {
	Tools map[string]ToolPolicy // keyed by scanner name; a missing entry uses DefaultMethod
	// DefaultMethod applies to any tool with no entry in Tools; "" means "auto".
	DefaultMethod string
}

func (o Options) policyFor(name string) ToolPolicy {
	if p, ok := o.Tools[name]; ok {
		return p
	}
	return ToolPolicy{Enabled: true, Method: o.DefaultMethod}
}

// OptionsFromConfig translates the user-facing config.SecurityConfig (P11.11)
// into the resolver's Options. The single place this shape conversion
// happens, so every caller (the security_scan tool, `aegis scan`, `aegis
// security status`) resolves tools identically.
func OptionsFromConfig(cfg config.SecurityConfig) Options {
	tools := make(map[string]ToolPolicy, len(cfg.Tools))
	for name, tc := range cfg.Tools {
		tools[name] = ToolPolicy{Enabled: tc.ToolEnabled(), Method: tc.Method, Image: tc.Image}
	}
	return Options{Tools: tools, DefaultMethod: cfg.DefaultMethod}
}

// detectRuntime is a seam over sandbox.DetectBest so tests can inject a
// deterministic container-runtime result without needing a real docker/
// podman install.
var detectRuntime = sandbox.DetectBest

// Resolve is the availability resolver every scanner calls (P11.11's
// unifying seam): given a scanner's binary name and image, and the caller's
// policy, it decides host-binary vs container-image vs unavailable — the
// single lookup that replaces "binary on PATH or silently skip." The
// returned image is only meaningful when method == MethodContainer.
func Resolve(ctx context.Context, name string, opts Options) (method Method, runtime sandbox.ContainerRuntime, image string, reason string) {
	d, ok := descriptors[name]
	if !ok {
		return MethodNone, "", "", name + ": no scanner descriptor registered"
	}
	policy := opts.policyFor(name)
	if !policy.Enabled {
		return MethodNone, "", "", "disabled by configuration (security.tools." + name + ".enabled: false)"
	}

	image = policy.Image
	if image == "" {
		image = d.DefaultImage
	}
	wantMethod := strings.ToLower(strings.TrimSpace(policy.Method))
	hostAvailable := lookPath(d.Binary)

	switch wantMethod {
	case "host":
		if hostAvailable {
			return MethodHost, "", "", ""
		}
		return MethodNone, "", "", d.Binary + " not installed on PATH (security.tools." + name + ".method is \"host\", no container fallback)"
	case "container":
		if image == "" {
			return MethodNone, "", "", "no container image configured for " + name + "; set security.tools." + name + ".image (digest-pinned) — see docs/security.md"
		}
		rt, ok := detectRuntime(ctx, nil)
		if !ok {
			return MethodNone, "", "", "security.tools." + name + ".method is \"container\" but no container runtime is available (docker/podman) — run `aegis security install " + name + "` for guided setup"
		}
		return MethodContainer, rt, image, ""
	default: // "auto" or unset
		if hostAvailable {
			return MethodHost, "", "", ""
		}
		if image == "" {
			return MethodNone, "", "", d.Binary + " not installed and no container image configured — set security.tools." + name + ".image (digest-pinned) to enable container fallback, or run `aegis security install " + name + "` for a guided host install"
		}
		rt, ok := detectRuntime(ctx, nil)
		if !ok {
			return MethodNone, "", "", d.Binary + " not installed and no container runtime available (docker/podman) — run `aegis security install " + name + "` for guided setup"
		}
		return MethodContainer, rt, image, ""
	}
}

// runContainerImage runs image (ideally digest-pinned) against dir
// bind-mounted at /src, passing args directly to the image's entrypoint — no
// shell involved, matching how virtually every security scanner's official
// image is designed to be invoked. Network is disabled (scanners operate on
// the mounted filesystem only; DAST is the one exception, handled
// separately). Hardening flags mirror sandbox.ContainerBackend's OCI args
// (P4.7/P7 posture): no capabilities, no privilege escalation.
func runContainerImage(ctx context.Context, rt sandbox.ContainerRuntime, image, dir string, args ...string) ([]byte, error) {
	cliArgs := containerRunArgs(rt, image, dir, args...)
	cmd := exec.CommandContext(ctx, string(rt), cliArgs...)
	out, err := cmd.Output()
	// Scanners commonly exit non-zero when they find issues; tolerate that as
	// long as some output was produced, matching runJSON's host-exec behavior.
	if len(out) == 0 && err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s run %s: %w\n%s", rt, image, err, ee.Stderr)
		}
		return nil, fmt.Errorf("%s run %s: %w", rt, image, err)
	}
	return out, nil
}

// containerRunArgs builds the CLI arguments for a scanner container run,
// split out from runContainerImage so the hardening/mount flags are unit
// testable without actually invoking a container runtime.
func containerRunArgs(rt sandbox.ContainerRuntime, image, dir string, args ...string) []string {
	cliArgs := []string{"run", "--rm", "--network", "none"}
	if rt != sandbox.RuntimeAppleContainers {
		cliArgs = append(cliArgs, "--cap-drop=ALL", "--security-opt=no-new-privileges")
	}
	cliArgs = append(cliArgs, "-v", sandbox.HostMountPath(rt, dir)+":/src", "-w", "/src", image)
	return append(cliArgs, args...)
}
