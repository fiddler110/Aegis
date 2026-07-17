package security

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// Method identifies how a scanner ran (or would run).
type Method string

const (
	MethodHost      Method = "host"      // native binary on PATH
	MethodContainer Method = "container" // via a pinned image on a detected container runtime
	MethodWSL       Method = "wsl"       // via a Linux distro under the Windows Subsystem for Linux
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
	// DefaultEnabled is the Enabled value Resolve assumes when an operator
	// hasn't configured security.tools.<name> at all (P11.3). True for every
	// tool that predates this field (preserves prior default-scan-everything
	// behavior); false for opt-in-only tools — semgrep (opengrep is now the
	// default SAST engine, semgrep is selectable via config) and the
	// language-targeted SAST engines (gosec/bandit/brakeman/njsscan), which
	// only make sense for a project in that specific language.
	DefaultEnabled bool
	// WSLCapable marks scanners whose Scan implementation has a MethodWSL
	// execution branch: tools with no native Windows build at all (opengrep,
	// kubescape), or whose native Windows build exists but is unreliable in
	// practice — nmap needs Npcap installed/running plus admin rights for
	// OS-detection scans, both common failure points reported on Windows.
	// Resolve only ever offers MethodWSL for these; every other tool has no
	// Scan-side WSL branch wired, so offering the method would silently
	// misroute execution back to a nonexistent host binary. A Linux distro
	// purpose-built for security tooling (Kali) is the recommended WSL
	// target — see security.wsl_distro in docs/security.md.
	WSLCapable bool
}

// descriptors is keyed by scanner name so provisioning/config code (P11.10,
// P11.11) can look one up without depending on the Scanner implementations.
var descriptors = map[string]ScannerDescriptor{
	"opengrep": {
		Name:           "opengrep",
		Binary:         "opengrep",
		Category:       "SAST",
		Summary:        "Static analysis — scans source code for security bugs and anti-patterns using pattern-based rules across 30+ languages. Community-governed semgrep fork: no login/telemetry, openly-licensed rules. Default SAST engine (P11.3); semgrep remains selectable.",
		DefaultEnabled: true,
		WSLCapable:     true, // no native Windows build; runs under WSL there (P14.x)
		Install: map[string]string{
			"darwin": "curl -fsSL https://raw.githubusercontent.com/opengrep/opengrep/main/install.sh | bash",
			"linux":  "curl -fsSL https://raw.githubusercontent.com/opengrep/opengrep/main/install.sh | bash",
		},
	},
	"semgrep": {
		Name:           "semgrep",
		Binary:         "semgrep",
		Category:       "SAST",
		Summary:        "Static analysis — scans source code for security bugs and anti-patterns using pattern-based rules across 30+ languages. Selectable alternative to the default opengrep engine (P11.3); semgrep's registry has faster rule-update velocity for brand-new CVE patterns, at the cost of needing network/platform login for `--config auto` (Aegis pins explicit packs instead).",
		DefaultEnabled: false,
		Install: map[string]string{
			"darwin":  "brew install semgrep",
			"linux":   "pipx install semgrep",
			"windows": "pipx install semgrep",
		},
	},
	"gosec": {
		Name:           "gosec",
		Binary:         "gosec",
		Category:       "SAST (Go)",
		Summary:        "Go-specific static analysis: scans for insecure use of crypto, SQL injection, hardcoded credentials, and other Go-idiomatic security anti-patterns. Opt-in — enable for Go projects (P11.3).",
		DefaultEnabled: false,
		Install: map[string]string{
			"darwin":  "brew install gosec",
			"linux":   "go install github.com/securego/gosec/v2/cmd/gosec@latest",
			"windows": "go install github.com/securego/gosec/v2/cmd/gosec@latest",
		},
	},
	"bandit": {
		Name:           "bandit",
		Binary:         "bandit",
		Category:       "SAST (Python)",
		Summary:        "Python-specific static analysis: scans for common security issues (shell injection, weak crypto, unsafe deserialization, hardcoded passwords). Opt-in — enable for Python projects (P11.3).",
		DefaultEnabled: false,
		Install: map[string]string{
			"darwin":  "pipx install 'bandit[sarif]'",
			"linux":   "pipx install 'bandit[sarif]'",
			"windows": "pipx install 'bandit[sarif]'",
		},
	},
	"brakeman": {
		Name:           "brakeman",
		Binary:         "brakeman",
		Category:       "SAST (Ruby/Rails)",
		Summary:        "Ruby on Rails-specific static analysis: scans for SQL/command injection, mass assignment, and other Rails-idiomatic vulnerabilities. Opt-in — enable for Rails projects (P11.3).",
		DefaultEnabled: false,
		Install: map[string]string{
			"darwin":  "gem install brakeman",
			"linux":   "gem install brakeman",
			"windows": "gem install brakeman",
		},
	},
	"njsscan": {
		Name:           "njsscan",
		Binary:         "njsscan",
		Category:       "SAST (Node.js)",
		Summary:        "Node.js-specific static analysis: semantic-aware scanning for insecure code patterns in JavaScript/TypeScript server code. Opt-in — enable for Node.js projects (P11.3).",
		DefaultEnabled: false,
		Install: map[string]string{
			"darwin":  "pipx install njsscan",
			"linux":   "pipx install njsscan",
			"windows": "pipx install njsscan",
		},
	},
	"trivy": {
		Name:           "trivy",
		Binary:         "trivy",
		Category:       "SCA / IaC / secrets",
		Summary:        "Scans dependencies for known CVEs, Terraform/Kubernetes/Dockerfile configs for misconfigurations, and the filesystem for hardcoded secrets.",
		DefaultEnabled: true,
		Install: map[string]string{
			"darwin":  "brew install trivy",
			"linux":   "curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin",
			"windows": "scoop install trivy",
		},
	},
	"gitleaks": {
		Name:           "gitleaks",
		Binary:         "gitleaks",
		Category:       "Secrets",
		Summary:        "Scans the working tree for hardcoded credentials (API keys, tokens, private keys) using regex + entropy detection rules.",
		DefaultEnabled: true,
		Install: map[string]string{
			"darwin":  "brew install gitleaks",
			"linux":   "install the release binary from https://github.com/gitleaks/gitleaks/releases",
			"windows": "scoop install gitleaks",
		},
	},
	"trufflehog": {
		Name:           "trufflehog",
		Binary:         "trufflehog",
		Category:       "Secrets",
		Summary:        "Scans the working tree for hardcoded credentials using 800+ detectors that can optionally live-verify a match against the real provider API (AWS/GitHub/etc.) to confirm it's still active, cutting triage noise sharply versus pattern-only detection. Opt-in, alongside gitleaks rather than replacing it — deduped against gitleaks findings at the same location (P11.8). AGPL-3.0 licensed (vs. gitleaks' MIT); Aegis only shells out to a separately-installed binary, so this is a disclosure, not a code-linking concern. Verification (security.tools.trufflehog.verify) is a separate, host-only opt-in — see docs/security.md.",
		DefaultEnabled: false,
		Install: map[string]string{
			"darwin":  "brew install trufflehog",
			"linux":   "curl -sSfL https://raw.githubusercontent.com/trufflesecurity/trufflehog/main/scripts/install.sh | sh -s -- -b /usr/local/bin",
			"windows": "go install github.com/trufflesecurity/trufflehog/v3@latest",
		},
	},
	"kubescape": {
		Name:           "kubescape",
		Binary:         "kubescape",
		Category:       "IaC / Kubernetes",
		Summary:        "Scans Kubernetes manifests/Helm charts for misconfigurations against NSA/MITRE/CIS framework controls, with proper severity (unlike lint-style tools such as kube-linter).",
		DefaultEnabled: true,
		WSLCapable:     true, // no native Windows build; runs under WSL there (P14.x)
		Install: map[string]string{
			"darwin": "brew install kubescape",
			"linux":  "curl -s https://raw.githubusercontent.com/kubescape/kubescape/master/install.sh | /bin/bash",
		},
	},
	"hadolint": {
		Name:           "hadolint",
		Binary:         "hadolint",
		Category:       "Container",
		Summary:        "Lints Dockerfiles for best-practice violations (e.g. missing pinned base image tags, unsafe ADD usage, running as root).",
		DefaultEnabled: true,
		Install: map[string]string{
			"darwin":  "brew install hadolint",
			"linux":   "install the release binary from https://github.com/hadolint/hadolint/releases",
			"windows": "scoop install hadolint",
		},
	},
	"grype": {
		Name:           "grype",
		Binary:         "grype",
		Category:       "Container",
		Summary:        "Scans a container image's layers for known CVEs in installed packages (Anchore's vulnerability database).",
		DefaultEnabled: true,
		Install: map[string]string{
			"darwin":  "brew install grype",
			"linux":   "curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin",
			"windows": "scoop install grype",
		},
	},
	"dockle": {
		Name:           "dockle",
		Binary:         "dockle",
		Category:       "Container",
		Summary:        "Checks a container image against CIS Docker Benchmark / best-practice rules (e.g. no secrets baked in, non-root user, minimal layers).",
		DefaultEnabled: true,
		Install: map[string]string{
			"darwin":  "brew install goodwithtech/r/dockle",
			"linux":   "install the release binary from https://github.com/goodwithtech/dockle/releases",
			"windows": "scoop install dockle",
		},
	},
	"osv-scanner": {
		Name:           "osv-scanner",
		Binary:         "osv-scanner",
		Category:       "SCA",
		Summary:        "Scans lockfiles/manifests across ecosystems for known vulnerabilities, backed by the OSV.dev database (Google).",
		DefaultEnabled: true,
		Install: map[string]string{
			"darwin":  "brew install osv-scanner",
			"linux":   "go install github.com/google/osv-scanner/cmd/osv-scanner@latest",
			"windows": "scoop install osv-scanner",
		},
	},
	"syft": {
		Name:           "syft",
		Binary:         "syft",
		Category:       "SCA / SBOM",
		Summary:        "Generates a Software Bill of Materials (CycloneDX) by inventorying a project's declared and transitive dependencies; feeds grype for CVE matching and doubles as a persisted supply-chain artifact.",
		DefaultEnabled: true,
		Install: map[string]string{
			"darwin":  "brew install syft",
			"linux":   "curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin",
			"windows": "scoop install syft",
		},
	},
	"nmap": {
		Name:     "nmap",
		Binary:   "nmap",
		Category: "Network / port & service discovery",
		Summary:  "Discovers live hosts, open ports, and running service/version banners across a target host list or CIDR range — the attack-surface-mapping step of a network recon run (`recon_scan`). Baseline mode is a top-100-port version-detection scan with no OS fingerprinting or scripts; active mode (security.dast.allow_active) adds OS detection, the full port range, and nmap's default-safe NSE script category.",
		// No DefaultEnabled: true — network-facing, opt-in like zap: an
		// operator must both install nmap and pass the recon_scan tool's
		// target-authorization gate (shared with DAST, see
		// internal/security/target.go) before it ever runs.
		DefaultEnabled: false,
		// WSLCapable despite having a native Windows install: Windows nmap
		// needs Npcap installed and running plus admin rights for -O/SYN
		// scans, a common source of exactly the failures that send Windows
		// operators to WSL instead — set security.tools.nmap.method: wsl (and
		// security.wsl_distro to a distro with nmap installed, e.g. Kali) to
		// force it.
		WSLCapable: true,
		Install: map[string]string{
			"darwin":  "brew install nmap",
			"linux":   "install via your distro's package manager, e.g. apt install nmap / dnf install nmap",
			"windows": "winget install Insecure.Nmap",
		},
	},
	"nuclei": {
		Name:           "nuclei",
		Binary:         "nuclei",
		Category:       "Network / host vulnerability scanning",
		Summary:        "Runs ProjectDiscovery's community template library (CVEs, misconfigurations, exposed panels, raw network checks) against a target host list to find known, template-matched vulnerabilities — the vulnerability-scanning half of a network recon run (`recon_scan`), complementing nmap's port/service discovery. Requires security.tools.nuclei.templates_version (a pinned nuclei-templates release) — templates are executable network-probe logic and are never pulled at an unpinned \"latest\" (same posture as a scanner container image, P7.6). Baseline mode excludes dos/fuzz/intrusive-tagged templates; active mode (security.dast.allow_active) includes them.",
		DefaultEnabled: false,
		// WSLCapable for the same reason as nmap — see nmap's comment. Set
		// security.tools.nuclei.method: wsl to force it.
		WSLCapable: true,
		Install: map[string]string{
			"darwin":  "brew install nuclei",
			"linux":   "go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest",
			"windows": "scoop install nuclei",
		},
	},
	"zap": {
		Name:     "zap",
		Binary:   "", // container-only — OWASP ZAP has no relevant host CLI in this workflow; lookPath("") always fails, so Resolve only ever considers the container path for it
		Category: "DAST",
		Summary:  "Dynamic Application Security Testing — crawls and (optionally) actively attacks a *running* application to find real, exploitable vulnerabilities (XSS, injection, auth issues, missing security headers). Container-only, opt-in, and target-restricted: see docs/security.md's DAST section for the authorization requirements before enabling.",
		// No DefaultEnabled: true — DAST is the highest-risk scanner in this
		// codebase (an active scan sends real attack traffic to a live
		// target) and has no host-binary safety net the way other opt-in
		// tools do; it stays off until an operator both configures an image
		// and, for anything beyond loopback/private targets, the target
		// allowlist (security.dast.allowed_targets).
		DefaultEnabled: false,
		// No Install entries (deliberately): `aegis security install zap`
		// falls through to its existing "no guided install available... —
		// configure a container image" message, which is exactly correct
		// here — there's no shell command to run, only config to set. Any
		// prose string here would be misread as literal shell to execute.
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
	// Enabled defaults to the tool's ScannerDescriptor.DefaultEnabled when
	// absent from config; set false to always skip this tool.
	Enabled bool
	// Method is "host" (never fall back to a container), "container" (always
	// prefer the container image over a present host binary), or "auto"/""
	// (host if present, else container) — the default.
	Method string
	// Image overrides DefaultImage when set. Must be a digest-pinned
	// reference (image@sha256:...) for the same reason DefaultImage is never
	// baked in — see ScannerDescriptor's doc comment.
	Image string
	// TemplatesVersion pins the nuclei scanner's template set to a specific
	// nuclei-templates release tag (P13.5.6) — templates are executable
	// network-probe logic, so they're never pulled at an unpinned "latest"
	// the way a scanner container image is never used unpinned (P7.6).
	// Meaningless for every other tool.
	TemplatesVersion string
	// Verify enables trufflehog's live credential verification (P13.2).
	// Meaningless for every other tool. Resolve refuses container mode
	// whenever this is set — see trufflehogScanner.Resolve.
	Verify bool
	// EnabledExplicit is true when Enabled came from an explicit
	// security.tools.<name>.enabled in config, rather than being defaulted
	// from ScannerDescriptor.DefaultEnabled. AutoEnableLanguageScanners
	// checks this so language auto-detection never overrides an operator's
	// deliberate choice, on or off.
	EnabledExplicit bool
}

// Options bundles per-tool policy for a scan run (the P11.11 config surface).
type Options struct {
	Tools map[string]ToolPolicy // keyed by scanner name; a missing entry uses DefaultMethod
	// DefaultMethod applies to any tool with no entry in Tools; "" means "auto".
	DefaultMethod string
	// Multiscanner is the single locally-built image carrying every bundled
	// scanner. When enabled, it supplies the container-method image for any
	// tool it covers that has no explicit security.tools.<name>.image — see
	// multiscanner.go. Zero value = disabled, leaving resolution exactly as it
	// was before the shared image existed.
	Multiscanner MultiscannerPolicy
	// WSLDistro names a specific registered WSL distro (e.g. "kali-linux") to
	// target for every WSLCapable scanner (P14.x), instead of whatever `wsl
	// --set-default` currently points at. Empty uses WSL's own default-distro
	// selection. A distro purpose-built for security tooling (Kali) is the
	// recommended target on Windows — see docs/security.md.
	WSLDistro string
}

func (o Options) policyFor(name string, defaultEnabled bool) ToolPolicy {
	if p, ok := o.Tools[name]; ok {
		return p
	}
	return ToolPolicy{Enabled: defaultEnabled, Method: o.DefaultMethod}
}

// OptionsFromConfig translates the user-facing config.SecurityConfig (P11.11)
// into the resolver's Options. The single place this shape conversion
// happens, so every caller (the security_scan tool, `aegis scan`, `aegis
// security status`) resolves tools identically.
//
// A tool's Enabled resolves from (in order): an explicit
// security.tools.<name>.enabled in config, else the tool's own
// ScannerDescriptor.DefaultEnabled (P11.3) — not a blanket true — so
// configuring e.g. security.tools.semgrep.method without an explicit
// enabled: true doesn't silently opt a default-off tool back in.
func OptionsFromConfig(cfg config.SecurityConfig) Options {
	tools := make(map[string]ToolPolicy, len(cfg.Tools))
	for name, tc := range cfg.Tools {
		enabled := true
		if d, ok := descriptors[name]; ok {
			enabled = d.DefaultEnabled
		}
		explicit := tc.Enabled != nil
		if explicit {
			enabled = *tc.Enabled
		}
		tools[name] = ToolPolicy{Enabled: enabled, EnabledExplicit: explicit, Method: tc.Method, Image: tc.Image, TemplatesVersion: tc.TemplatesVersion, Verify: tc.Verify}
	}
	return Options{
		Tools:         tools,
		DefaultMethod: cfg.DefaultMethod,
		WSLDistro:     cfg.WSLDistro,
		Multiscanner:  MultiscannerPolicyFromConfig(cfg.Multiscanner),
	}
}

// detectRuntime is a seam over sandbox.DetectBest so tests can inject a
// deterministic container-runtime result without needing a real docker/
// podman install.
var detectRuntime = sandbox.DetectBest

// wslBinaryAvailable is a seam over sandbox.WSLBinaryAvailable so tests can
// inject a deterministic WSL-availability result without needing a real WSL
// install. Always false off Windows (sandbox.WSLDistroAvailable itself
// short-circuits on GOOS), so callers never need their own OS check.
var wslBinaryAvailable = sandbox.WSLBinaryAvailable

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
	policy := opts.policyFor(name, d.DefaultEnabled)
	if !policy.Enabled {
		if _, explicit := opts.Tools[name]; explicit {
			return MethodNone, "", "", "disabled by configuration (security.tools." + name + ".enabled: false)"
		}
		return MethodNone, "", "", "opt-in tool, not enabled by default — set security.tools." + name + ".enabled: true (or `aegis security config`) to turn it on"
	}

	// Image precedence: an explicit per-tool image always wins, then the
	// shared multiscanner image (if enabled and it carries this tool), then
	// the descriptor default — which is empty for every built-in scanner, by
	// design. viaMultiscanner selects which validation the image gets below:
	// a digest-pin regex for a registry reference, or a real image-ID
	// comparison for the locally-built one (see verifyMultiscannerImage).
	image = policy.Image
	viaMultiscanner := false
	if image == "" && opts.Multiscanner.Covers(name) {
		image, viaMultiscanner = opts.Multiscanner.Image, true
	}
	if image == "" {
		image = d.DefaultImage
	}
	wantMethod := strings.ToLower(strings.TrimSpace(policy.Method))
	hostAvailable := lookPath(d.Binary)

	// imageUsable validates a resolved image for execution: nothing for a
	// multiscanner image until a runtime is known (its check needs one), the
	// digest-pin rule otherwise.
	imageUsable := func() string {
		if viaMultiscanner {
			return ""
		}
		return digestPinReason(name, image)
	}

	// The shared image only exists in the storage of the runtime that built
	// it, so probe that one rather than whatever DetectBest would prefer.
	var runtimePriority []sandbox.ContainerRuntime
	if viaMultiscanner {
		runtimePriority = opts.Multiscanner.RuntimePriority()
	}

	switch wantMethod {
	case "host":
		if hostAvailable {
			return MethodHost, "", "", ""
		}
		return MethodNone, "", "", d.Binary + " not installed on PATH (security.tools." + name + ".method is \"host\", no container fallback)"
	case "container":
		if image == "" {
			return MethodNone, "", "", noContainerImageReason(name, opts)
		}
		if reason := imageUsable(); reason != "" {
			return MethodNone, "", "", reason
		}
		rt, ok := detectRuntime(ctx, runtimePriority)
		if !ok {
			if viaMultiscanner {
				return MethodNone, "", "", "the multiscanner image was built with " + string(opts.Multiscanner.Runtime) + ", which isn't available now — start it (on Windows with Podman: `podman machine start`), or re-run `aegis security build-image` to rebuild with an available runtime"
			}
			return MethodNone, "", "", "security.tools." + name + ".method is \"container\" but no container runtime is available (docker/podman) — run `aegis security install " + name + "` for guided setup"
		}
		if viaMultiscanner {
			if reason := verifyMultiscannerImage(ctx, rt, opts.Multiscanner); reason != "" {
				return MethodNone, "", "", reason
			}
			if reason := verifyMultiscannerCache(ctx, rt, name, opts.Multiscanner); reason != "" {
				return MethodNone, "", "", reason
			}
		}
		return MethodContainer, rt, image, ""
	case "wsl":
		if !d.WSLCapable {
			return MethodNone, "", "", name + " has no WSL execution path wired (security.tools." + name + ".method is \"wsl\")"
		}
		if wslBinaryAvailable(ctx, d.Binary, opts.WSLDistro) {
			return MethodWSL, "", "", ""
		}
		return MethodNone, "", "", d.Binary + " not found inside WSL (security.tools." + name + ".method is \"wsl\") — run `aegis security install " + name + "` to install it there"
	default: // "auto" or unset
		if hostAvailable {
			return MethodHost, "", "", ""
		}
		// multiscannerReason is held rather than returned immediately: under
		// "auto" a broken shared image shouldn't preempt a working WSL path.
		// If nothing else runs the tool either, it's the most actionable
		// reason to report, so it wins over the generic no-image message.
		multiscannerReason := ""
		if image != "" {
			if reason := imageUsable(); reason != "" {
				return MethodNone, "", "", reason
			}
			if rt, ok := detectRuntime(ctx, runtimePriority); ok {
				if viaMultiscanner {
					multiscannerReason = verifyMultiscannerImage(ctx, rt, opts.Multiscanner)
					if multiscannerReason == "" {
						multiscannerReason = verifyMultiscannerCache(ctx, rt, name, opts.Multiscanner)
					}
				}
				if multiscannerReason == "" {
					return MethodContainer, rt, image, ""
				}
			}
		}
		if d.WSLCapable && wslBinaryAvailable(ctx, d.Binary, opts.WSLDistro) {
			return MethodWSL, "", "", ""
		}
		if multiscannerReason != "" {
			return MethodNone, "", "", multiscannerReason
		}
		if image == "" {
			if opts.Multiscanner.Enabled && opts.Multiscanner.Image != "" && !opts.Multiscanner.Tools[name] {
				return MethodNone, "", "", d.Binary + " not installed, and " + noContainerImageReason(name, opts)
			}
			msg := d.Binary + " not installed and no container image configured — set security.tools." + name + ".image (digest-pinned) to enable container fallback, run `aegis security build-image` for the shared multiscanner image, or `aegis security install " + name + "` for a guided host install"
			if d.WSLCapable {
				msg = d.Binary + " not installed (no native Windows build) and not found inside WSL either — run `aegis security install " + name + "` for a guided install, or `aegis security build-image` for the shared multiscanner image"
			}
			return MethodNone, "", "", msg
		}
		return MethodNone, "", "", d.Binary + " not installed and no container runtime available (docker/podman) — run `aegis security install " + name + "` for guided setup"
	}
}

// noContainerImageReason explains why a tool has no usable container image.
//
// The multiscanner case is called out separately because the generic advice
// ("run `aegis security build-image`") is actively misleading to someone who
// already did: a core-profile image genuinely doesn't carry bandit, and
// telling them to run the command they just ran sends them in a circle. The
// actionable fact is that the profile they built excludes this tool.
func noContainerImageReason(name string, opts Options) string {
	if opts.Multiscanner.Enabled && opts.Multiscanner.Image != "" && !opts.Multiscanner.Tools[name] {
		if why, excluded := multiscannerExcludedTools[name]; excluded {
			return "the multiscanner image deliberately doesn't carry " + name + ": " + why
		}
		return "the multiscanner image (" + opts.Multiscanner.Image + ") doesn't carry " + name + " — rebuild it with `aegis security build-image --profile full` to include it, or set security.tools." + name + ".image (digest-pinned) to give it an image of its own"
	}
	return "no container image configured for " + name + "; set security.tools." + name + ".image (digest-pinned), or run `aegis security build-image` to build the shared multiscanner image — see docs/security_scan.md"
}

// digestPinRe matches a container reference's trailing "@sha256:<hex>"
// digest pin. Deliberately not length-anchored to exactly 64 hex characters
// — the point is catching a floating tag (image:latest, or a bare image
// name with no pin at all), not re-validating SHA-256's output length.
var digestPinRe = regexp.MustCompile(`@sha256:[0-9a-fA-F]+$`)

// digestPinReason returns a non-empty MethodNone reason if image is
// configured but not digest-pinned (P11.9 provenance hardening) — a
// floating tag (or bare image name) is real supply-chain risk (P11.1/P7.6's
// posture: an image is itself attack surface, and a tag can be repointed at
// any time by whoever controls the registry), so it's rejected the same way
// a missing image is, rather than silently run.
func digestPinReason(name, image string) string {
	if digestPinRe.MatchString(strings.TrimSpace(image)) {
		return ""
	}
	return "security.tools." + name + ".image (" + image + ") is not digest-pinned (need image@sha256:<hex>, not a floating tag) — see docs/security.md's docker pull + docker inspect pin recipe"
}

// runScannerImage runs one scanner out of image against dir.
//
// It exists because a per-tool scanner image and the shared multiscanner image
// are invoked differently. Every official scanner image ENTRYPOINTs its own
// tool, so Aegis passes bare arguments ("fs", "--format", "sarif", ...). An
// image carrying sixteen tools obviously can't entrypoint one of them, so it
// sets no entrypoint and the binary name becomes the first argument instead.
// Callers pass both and this picks, so no call site has to know which image it
// got from Resolve.
func runScannerImage(ctx context.Context, rt sandbox.ContainerRuntime, image, dir string, opts Options, binary string, args ...string) ([]byte, error) {
	if !opts.usesMultiscanner(image) {
		return runContainerImage(ctx, rt, image, dir, args...)
	}
	// The multiscanner ships no databases, so the cache volume has to come
	// along or DB-backed scanners have nothing to match against. Still
	// --network none: update-db is the only run allowed to fetch.
	args = append([]string{binary}, args...)
	cliArgs := containerRunArgs(rt, image, dir, args...)
	cliArgs = withCacheVolume(cliArgs, image)
	return runContainerCLI(ctx, rt, image, cliArgs)
}

// withCacheVolume inserts the scanner cache mount just before the image
// reference, which is where a run's flags have to stop and its command begins.
func withCacheVolume(cliArgs []string, image string) []string {
	for i, a := range cliArgs {
		if a == image {
			out := make([]string, 0, len(cliArgs)+2)
			out = append(out, cliArgs[:i]...)
			out = append(out, "-v", MultiscannerCacheVolume+":"+multiscannerCacheMount)
			return append(out, cliArgs[i:]...)
		}
	}
	return cliArgs
}

// usesMultiscanner reports whether image is the shared multiscanner image, as
// opposed to a per-tool image an operator pinned for this scanner.
func (o Options) usesMultiscanner(image string) bool {
	return o.Multiscanner.Enabled && image != "" && image == o.Multiscanner.Image
}

// runContainerImage runs image (ideally digest-pinned) against dir
// bind-mounted at /src, passing args directly to the image's entrypoint — no
// shell involved, matching how virtually every security scanner's official
// image is designed to be invoked. Network is disabled (scanners operate on
// the mounted filesystem only; DAST is the one exception, handled
// separately). Hardening flags mirror sandbox.ContainerBackend's OCI args
// (P4.7/P7 posture): no capabilities, no privilege escalation.
func runContainerImage(ctx context.Context, rt sandbox.ContainerRuntime, image, dir string, args ...string) ([]byte, error) {
	return runContainerCLI(ctx, rt, image, containerRunArgs(rt, image, dir, args...))
}

// runContainerCLI executes an already-built runtime command line, split out so
// a caller that needs to adjust the flags (see runScannerImage's cache mount)
// doesn't have to duplicate the exit-code handling.
func runContainerCLI(ctx context.Context, rt sandbox.ContainerRuntime, image string, cliArgs []string) ([]byte, error) {
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
