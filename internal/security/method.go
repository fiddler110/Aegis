package security

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// hostGOOS is a seam over runtime.GOOS so a test can exercise a
// platform-specific resolution rule (ScannerDescriptor.HostBroken) on any
// machine. Resolve can't read runtime.GOOS directly anyway — its `runtime`
// named return shadows the package inside the function body — but the real
// reason this is a var is P34.7's lesson: a test that asserts what the host
// happens to be is testing the host, not the rule.
var hostGOOS = runtime.GOOS

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
	// behavior); false for opt-in-only tools — the language-targeted SAST
	// engines (gosec/bandit/brakeman/njsscan), which only make sense for a
	// project in that specific language.
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
	// HostBroken maps a GOOS to the reason this scanner's *host* binary
	// cannot produce a usable result there, even when it's installed and on
	// PATH. Distinct from "not installed" (an operator can fix that) and from
	// RelevanceChecker (about the workspace, not the platform): this is the
	// binary being present and non-functional, which Resolve otherwise has no
	// way to see — lookPath only proves a file exists.
	//
	// Resolve treats a HostBroken platform as "no host binary": under "auto"
	// it falls through to the container (which is Linux, so it's unaffected),
	// and under an explicit method: host it reports this reason rather than
	// running the binary. Deliberately not a blanket skip of the tool — the
	// container method runs these tools fine on the same machine, so refusing
	// the tool outright would under-report a scan Aegis can actually do.
	//
	// Keyed by GOOS rather than probing the tool because the breakage is a
	// hardcoded platform branch in the tool itself (see njsscan), not a
	// missing dependency a probe could detect.
	HostBroken map[string]string
}

// descriptors is keyed by scanner name so provisioning/config code (P11.10,
// P11.11) can look one up without depending on the Scanner implementations.
var descriptors = map[string]ScannerDescriptor{
	"opengrep": {
		Name:           "opengrep",
		Binary:         "opengrep",
		Category:       "SAST",
		Summary:        "Static analysis — scans source code for security bugs and anti-patterns using pattern-based rules across 30+ languages. Community-governed semgrep fork: no login/telemetry, openly-licensed rules. The SAST engine (P11.3).",
		DefaultEnabled: true,
		WSLCapable:     true, // no native Windows build; runs under WSL there (P14.x)
		Install: map[string]string{
			"darwin": "curl -fsSL https://raw.githubusercontent.com/opengrep/opengrep/main/install.sh | bash",
			"linux":  "curl -fsSL https://raw.githubusercontent.com/opengrep/opengrep/main/install.sh | bash",
		},
	},
	"gosec": {
		Name:           "gosec",
		Binary:         "gosec",
		Category:       "SAST (Go)",
		Summary:        "Go-specific static analysis: scans for insecure use of crypto, SQL injection, hardcoded credentials, and other Go-idiomatic security anti-patterns. Opt-in — enable for Go projects (P11.3). Compile-assisted, so the container path runs it in two phases (P55.8): a networked `go mod download` with the workspace mounted read-only, then the analysis itself with --network none, exactly as every other scanner runs. A failed warm phase aborts the tool rather than letting it scan a cold cache, because gosec does not fail on one — it silently drops every type-aware rule and still exits clean.",
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
		Summary:        "Node.js-specific static analysis: semantic-aware scanning for insecure code patterns in JavaScript/TypeScript server code. Opt-in — enable for Node.js projects (P11.3). No usable Windows host build (see HostBroken); the container method runs it there.",
		DefaultEnabled: false,
		// njsscan's engine (libsast) hardcodes `if platform.system() ==
		// 'Windows': return None` in invoke_semgrep, then calls .get() on that
		// None in SemanticGrep.format_output — so every run on a Windows host
		// dies with an AttributeError traceback, identically with and without
		// JS files present, on every project (P34.9).
		//
		// This is *not* njsscan's underlying engine being unsupported on
		// Windows — it runs there fine: libsast never asks whether the engine
		// is available, so gating on that would let njsscan through to the
		// same crash. The condition here is the one libsast itself branches
		// on, which is why this is keyed by GOOS.
		HostBroken: map[string]string{
			"windows": "njsscan has no working Windows host build: its libsast engine stubs out its own analysis call on Windows and then crashes on the stub (AttributeError), on every project regardless of language",
		},
		// No "windows" entry, deliberately: pipx would install a binary that
		// can only ever produce the traceback above. `aegis security install
		// njsscan` on Windows says so and points at the container instead.
		Install: map[string]string{
			"darwin": "pipx install njsscan",
			"linux":  "pipx install njsscan",
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
	// Netscanner is the second locally-built image (P55.7): network on, no
	// workspace mount, for the tools that scan a remote target rather than
	// local source. Read only by ResolveNetwork — the directory-scanner resolver
	// never consults it, and vice versa, which is what keeps "container" from
	// meaning two postures at one call site. Zero value = disabled, leaving
	// image scanning and network recon exactly as they were.
	Netscanner NetscannerPolicy
	// WSLDistro names a specific registered WSL distro (e.g. "kali-linux") to
	// target for every WSLCapable scanner (P14.x), instead of whatever `wsl
	// --set-default` currently points at. Empty uses WSL's own default-distro
	// selection. A distro purpose-built for security tooling (Kali) is the
	// recommended target on Windows — see docs/security.md.
	WSLDistro string

	// runtimeCheck memoizes container-runtime detection for the duration of a
	// resolution pass. A pointer so it survives Options being copied by value
	// through Resolve (and through AutoEnableLanguageScanners, which rebuilds
	// Tools but keeps the rest of the struct). Nil is valid and means "probe on
	// every call" — the shape a test constructing Options directly gets, and
	// the shape that keeps the seam-swapping tests below honest. Allocated only
	// by OptionsFromConfig, exactly like MultiscannerPolicy.check.
	runtimeCheck *runtimeProbeCache
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
// configuring e.g. security.tools.bandit.method without an explicit
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
		Netscanner:    NetscannerPolicyFromConfig(cfg.Netscanner),
		runtimeCheck:  &runtimeProbeCache{},
	}
}

// detectRuntime is a seam over sandbox.DetectBest so tests can inject a
// deterministic container-runtime result without needing a real docker/
// podman install.
//
// Never call this directly from resolve — go through Options.detectRuntime,
// which memoizes it. See runtimeProbeCache.
var detectRuntime = sandbox.DetectBest

// runtimeDetectTTL bounds how long one container-runtime probe result is
// reused. Deliberately the same window as multiscannerVerifyTTL, and for the
// same reason: the two memos guard consecutive steps of the same decision, so
// a scan that trusts a 15s-old image verification has no business insisting on
// a fresher answer to "is podman up".
//
// A process-lifetime cache would be wrong here. Options is built once per
// loaded config and held for the daemon's lifetime, and "no runtime available"
// is precisely the state an operator fixes from outside the process (`podman
// machine start`) — caching that forever would mean a daemon restart before
// scans worked again. 15s collapses a resolution pass into one probe and still
// notices the machine coming up before the next one.
//
// A var rather than a const only so a test can compress or extend the window
// and assert expiry behaviour through Resolve, instead of reaching into the
// cache's fields. Never assigned outside tests.
var runtimeDetectTTL = multiscannerVerifyTTL

// runtimeProbeCache memoizes container-runtime detection across the ~16
// Resolve calls that make up a single resolution pass (a `security status`
// render, a scan plan).
//
// Why this exists: P55.4 inverted the "auto" branch to prefer the multiscanner
// container over an available host binary, which moved detectRuntime from
// "reached only when no host binary exists" to "reached once per
// multiscanner-covered tool". detectRuntime shells out (`podman version`,
// `docker version`) under a 3s-per-candidate timeout, so one status render went
// from roughly one probe to fourteen — and the *slow* case is the negative one,
// where every candidate has to time out before the answer is "no". Caching the
// negative result matters at least as much as caching the positive one.
//
// Why it lives here rather than in internal/sandbox: sandbox.DetectBest's other
// callers are diagnostics (`aegis sandbox detect`, `aegis doctor`, sandbox
// backend selection) whose whole job is to report the runtime's state *right
// now* — silently serving them a 15s-old answer would make a diagnostic lie
// about the thing it was run to check. The duplication is specific to this
// resolver's fan-out over a tool list, so the memo belongs where that fan-out
// is. DetectBest stays a straight probe; anyone who wants memoization opts in.
//
// Why it hangs off Options rather than a package-level var: it follows the
// existing MultiscannerPolicy.check/.cacheCheck convention exactly, including
// the deliberate property that an Options built directly in code (as tests do)
// gets nil and probes every time. Package state would leak one test's canned
// probe result into the next.
type runtimeProbeCache struct {
	mu      sync.Mutex
	entries map[string]*runtimeProbeEntry
}

// runtimeProbeEntry is one cached probe result, plus the singleflight latch
// that keeps concurrent callers from all missing and all shelling out.
type runtimeProbeEntry struct {
	// done is closed when the in-flight probe that created this entry
	// completes. Waiters block on it rather than starting a probe of their own.
	done chan struct{}
	// pending is true until that probe writes its result. Distinct from
	// `done == nil` so a completed entry keeps a closed channel and a late
	// waiter's receive returns immediately instead of blocking forever.
	pending bool
	at      time.Time
	rt      sandbox.ContainerRuntime
	ok      bool
}

// runtimeProbeKey identifies one probe by the priority list it was asked for.
// A map rather than a single slot because a pass legitimately mixes two
// priorities — the multiscanner's recorded runtime for the tools it covers, nil
// (OS default order) for a tool with its own pinned image — and a single slot
// would thrash between them, reintroducing the very re-probing this removes.
func runtimeProbeKey(priority []sandbox.ContainerRuntime) string {
	parts := make([]string, len(priority))
	for i, rt := range priority {
		parts[i] = string(rt)
	}
	return strings.Join(parts, ",")
}

// detectRuntime resolves a container runtime for priority, reusing a recent
// result and collapsing concurrent callers onto a single probe.
//
// The singleflight half is not decoration: engine.runTools runs read-capability
// tools concurrently and scanners resolve in parallel, so without it N
// goroutines starting together all see a cold cache and all shell out — the
// exact cost this removes, just moved from serial to parallel. Note this is
// stricter than the existing multiscanner memo, which holds its mutex only
// across the cache read/write and lets concurrent misses duplicate the
// `image inspect` (correct, since the work is idempotent, but not collapsed).
//
// A nil cache means "no memo": probe every time, exactly as before.
func (o Options) detectRuntime(ctx context.Context, priority []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
	c := o.runtimeCheck
	if c == nil {
		return detectRuntime(ctx, priority)
	}
	key := runtimeProbeKey(priority)
	for {
		c.mu.Lock()
		if c.entries == nil {
			c.entries = map[string]*runtimeProbeEntry{}
		}
		e := c.entries[key]
		if e != nil {
			if e.pending {
				done := e.done
				c.mu.Unlock()
				select {
				case <-done:
					// The leader finished; loop round and read its result
					// (rather than reaching into e, whose TTL we still owe a
					// check — the leader may have been slow enough to expire).
					continue
				case <-ctx.Done():
					// Same answer the un-memoized path gives for a cancelled
					// context: the probe could not confirm a runtime.
					return "", false
				}
			}
			if time.Since(e.at) < runtimeDetectTTL {
				rt, ok := e.rt, e.ok
				c.mu.Unlock()
				return rt, ok
			}
		}
		// Claim the probe: publish a pending entry so concurrent callers wait
		// on it instead of duplicating the work.
		e = &runtimeProbeEntry{done: make(chan struct{}), pending: true}
		c.entries[key] = e
		c.mu.Unlock()

		rt, ok := detectRuntime(ctx, priority)

		c.mu.Lock()
		e.rt, e.ok, e.at, e.pending = rt, ok, time.Now(), false
		c.mu.Unlock()
		close(e.done)
		return rt, ok
	}
}

// wslBinaryAvailable is a seam over sandbox.WSLBinaryAvailable so tests can
// inject a deterministic WSL-availability result without needing a real WSL
// install. Always false off Windows (sandbox.WSLDistroAvailable itself
// short-circuits on GOOS), so callers never need their own OS check.
var wslBinaryAvailable = sandbox.WSLBinaryAvailable

// Resolution is Resolve's full result, including information that has no
// place in the four-value form every Scanner.Resolve implementation forwards.
//
// It exists because Resolve's signature is a contract, in both directions.
// `reason` is read by every caller as "why this tool is not going to run":
// they branch on MethodNone and only then print it, and on a successful
// resolution they overwrite it outright ("on PATH", "via podman"). So an
// advisory about a *successful* resolution put into `reason` would be both a
// misuse of the field's documented meaning and, in practice, invisible.
// Widening the tuple isn't available either — the four-value shape is the
// Scanner interface's (internal/security/security.go, recon.go), forwarded by
// a dozen per-scanner implementations. Hence: a separate type, and Resolve
// stays byte-for-byte the function every existing caller already compiles
// against.
type Resolution struct {
	Method  Method
	Runtime sandbox.ContainerRuntime
	Image   string
	// Reason explains a MethodNone, and is empty for every other method —
	// exactly as Resolve's fourth return value always has been.
	Reason string
	// Note is advisory information about a *successful* resolution. Today it
	// carries one case: P55.4's container-preferred-but-unavailable fallback,
	// where the scan is about to run on an unpinned host binary and the
	// operator deserves to know rather than discovering it by diffing two
	// machines' findings. Empty means "nothing to add"; it never means
	// failure, so a caller that ignores it behaves exactly as it did before
	// this field existed.
	//
	// It is a complete sentence about *this one tool*, which is the right
	// shape for a caller resolving a single scanner and the wrong shape for
	// one resolving all sixteen — see FallbackWhy and HostFallbackAdvisory.
	Note string
	// FallbackWhy is the machine-usable half of Note: the bare reason the
	// container was unavailable, with no per-tool preamble wrapped around it.
	//
	// It exists because every display site in this codebase resolves the whole
	// descriptor list at once, and on a machine whose container runtime simply
	// isn't running, *every* covered tool falls back for the identical reason.
	// Printing Note per row would be fourteen paragraphs that differ only in a
	// tool name — the exact noise that teaches an operator to skip the warning
	// block. Callers collect this instead and hand the set to
	// HostFallbackAdvisory, which prints one line per distinct cause.
	FallbackWhy string
}

// ResolveDetailed is Resolve with Resolution.Note attached — the entry point
// for callers that want to surface the advisory (status tables, scan reports)
// rather than only decide whether a tool runs.
func ResolveDetailed(ctx context.Context, name string, opts Options) Resolution {
	var extra Resolution
	method, rt, image, reason := resolve(ctx, name, opts, &extra)
	extra.Method, extra.Runtime, extra.Image, extra.Reason = method, rt, image, reason
	return extra
}

// HostFallbackAdvisory collapses the per-tool fallback reasons gathered across
// a whole scanner list (Resolution.FallbackWhy, keyed by tool name) into a
// single advisory — one clause per *distinct cause*, naming the tools it
// affects, rather than one paragraph per tool.
//
// Two properties keep this from nagging, and both are load-bearing:
//
//   - It can only fire when the operator has actually built and pinned the
//     multiscanner image, because Resolve only prefers the container for tools
//     MultiscannerPolicy.Covers reports — which requires Enabled and a pinned
//     Image. An operator who has no container runtime and never ran
//     `aegis security build-image` sees nothing at all, which is correct: they
//     never asked for the container path, so a host binary isn't a fallback
//     for them, it's the plan.
//   - When it does fire it is one line, not one per tool, and it ends in the
//     specific thing to fix (a stopped podman machine, an unpopulated DB
//     cache) rather than a general exhortation.
//
// Returns "" for an empty map, so a caller can print unconditionally.
func HostFallbackAdvisory(fallbacks map[string]string) string {
	if len(fallbacks) == 0 {
		return ""
	}
	byWhy := map[string][]string{}
	names := make([]string, 0, len(fallbacks))
	for tool, why := range fallbacks {
		byWhy[why] = append(byWhy[why], tool)
		names = append(names, tool)
	}
	sort.Strings(names)

	const consequence = " running from host binaries instead of the multiscanner container: unpinned (whatever version is on PATH), unverified, and outside the container's --network none confinement, so another machine can report different findings for the same tree."

	whys := make([]string, 0, len(byWhy))
	for why := range byWhy {
		whys = append(whys, why)
	}
	sort.Strings(whys)
	if len(whys) == 1 {
		verb := "is"
		if len(names) > 1 {
			verb = "are"
		}
		return joinNames(names) + " " + verb + consequence + " The container is unavailable — " + whys[0]
	}
	// With more than one cause the tools are named in the bullets, so the head
	// counts them instead of listing them — naming each tool twice in one
	// advisory is the repetition this whole function exists to remove.
	var b strings.Builder
	fmt.Fprintf(&b, "%d scanners are%s The container is unavailable:", len(names), consequence)
	for _, why := range whys {
		tools := byWhy[why]
		sort.Strings(tools)
		fmt.Fprintf(&b, "\n  - %s: %s", strings.Join(tools, ", "), why)
	}
	return b.String()
}

// joinNames renders a sorted name list the way a sentence wants it, so the
// advisory reads as prose rather than as a serialized slice.
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// Resolve is the availability resolver every scanner calls (P11.11's
// unifying seam): given a scanner's binary name and image, and the caller's
// policy, it decides host-binary vs container-image vs unavailable — the
// single lookup that replaces "binary on PATH or silently skip." The
// returned image is only meaningful when method == MethodContainer.
func Resolve(ctx context.Context, name string, opts Options) (method Method, runtime sandbox.ContainerRuntime, image string, reason string) {
	return resolve(ctx, name, opts, nil)
}

// resolve is the implementation both entry points share. extra is an out-param
// rather than extra return values deliberately: its fields are written in
// exactly one branch, and threading them through every return here would
// spread empty strings across two dozen statements to say nothing. nil means
// the caller doesn't want them. resolve never sets extra's Method/Runtime/
// Image/Reason — those are the tuple, and ResolveDetailed copies them in.
func resolve(ctx context.Context, name string, opts Options, extra *Resolution) (method Method, runtime sandbox.ContainerRuntime, image string, reason string) {
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
	// A HostBroken platform means the host binary can't produce a usable
	// result even if it's sitting on PATH, so it doesn't count as available
	// (P34.9). hostUnavailable is the phrase every "can't run it natively"
	// message below opens with: "not installed" is the wrong story to tell
	// about a binary that is installed and simply cannot work here, and it
	// would also mislead AvailabilityNote, which keys on that exact phrase.
	hostBroken := d.HostBroken[hostGOOS]
	hostAvailable := lookPath(d.Binary) && hostBroken == ""
	hostUnavailable := d.Binary + " not installed"
	if hostBroken != "" {
		hostUnavailable = hostBroken
	}

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
		if hostBroken != "" {
			return MethodNone, "", "", hostBroken + " (security.tools." + name + ".method is \"host\") — set security.tools." + name + ".method: container (or remove the method pin to let it fall back automatically) to run " + name + " in the Linux scanner image instead"
		}
		return MethodNone, "", "", d.Binary + " not installed on PATH (security.tools." + name + ".method is \"host\", no container fallback)"
	case "container":
		if image == "" {
			return MethodNone, "", "", noContainerImageReason(name, opts)
		}
		if reason := imageUsable(); reason != "" {
			return MethodNone, "", "", reason
		}
		rt, ok := opts.detectRuntime(ctx, runtimePriority)
		if !ok {
			if viaMultiscanner {
				return MethodNone, "", "", multiscannerNoRuntimeReason(opts)
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
		// Resolution order under "auto" is container → host → WSL, and the
		// container half of that is an inversion of the original host → container
		// → WSL (P55.4).
		//
		// Host-first was right while the container was a *fallback* for tools
		// the operator hadn't installed: any host binary beat no scan at all.
		// It became wrong once `aegis security build-image` made the container
		// the supported path, because it quietly discarded everything the image
		// exists to provide — an ID-verified, reproducible build, a known
		// scanner version with a known rule set, and --network none confinement
		// — in favour of whatever version happened to be on PATH. Measured on a
		// machine with both: of the scanners that ran, seven took the host and
		// exactly one took a container path, so an image that had been built,
		// pinned and cache-populated went almost entirely unused and two
		// machines could report different findings for the same tree with
		// nothing saying why.
		//
		// The trade-off is deliberate and it is not free: a container run costs
		// more to start than a host binary, and this now probes for a runtime
		// (and verifies the image, TTL-memoized) even for tools whose host
		// binary is right there on PATH. That is the price of reproducibility,
		// which is the reason the image exists.
		//
		// Scoped to viaMultiscanner on purpose. The same pinned/confined
		// argument does apply to a per-tool security.tools.<name>.image, but
		// that image is the operator's own explicit artifact rather than one
		// Aegis provisions and verifies by ID, and an operator who wants it to
		// win over a host binary already has an unambiguous way to say so —
		// method: container. Inverting it too would silently redirect a
		// configuration that predates this change and was never measured, so it
		// keeps the old host-first order below.
		//
		// A refused container never fails the tool: an unpinned host scan beats
		// no scan, so every path here falls through to host (then WSL) and only
		// reports MethodNone when nothing at all can run it.
		//
		// multiscannerReason is held rather than returned immediately, exactly
		// as before: under "auto" a broken shared image shouldn't preempt a
		// working WSL path (nor, now, a working host binary). If nothing else
		// runs the tool either, it's the most actionable reason to report, so
		// it still wins over the generic no-image message at the tail.
		multiscannerReason := ""
		// containerFallbackWhy is the note's half of the same information: it
		// covers the missing-runtime case too, which is not a MethodNone reason
		// (the tail's generic message is better there) but is very much
		// something to say when the answer turns out to be an unpinned host run.
		containerFallbackWhy := ""
		if viaMultiscanner {
			if rt, ok := opts.detectRuntime(ctx, runtimePriority); ok {
				multiscannerReason = verifyMultiscannerImage(ctx, rt, opts.Multiscanner)
				if multiscannerReason == "" {
					multiscannerReason = verifyMultiscannerCache(ctx, rt, name, opts.Multiscanner)
				}
				if multiscannerReason == "" {
					return MethodContainer, rt, image, ""
				}
				containerFallbackWhy = multiscannerReason
			} else {
				containerFallbackWhy = multiscannerNoRuntimeReason(opts)
			}
		}
		if hostAvailable {
			if containerFallbackWhy != "" && extra != nil {
				extra.Note = name + " will run from the host binary, which is unpinned (whatever version is on PATH), unverified, and not confined to --network none: the multiscanner container was preferred but is unavailable — " + containerFallbackWhy
				extra.FallbackWhy = containerFallbackWhy
			}
			return MethodHost, "", "", ""
		}
		if !viaMultiscanner && image != "" {
			if reason := imageUsable(); reason != "" {
				return MethodNone, "", "", reason
			}
			if rt, ok := opts.detectRuntime(ctx, runtimePriority); ok {
				return MethodContainer, rt, image, ""
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
				return MethodNone, "", "", hostUnavailable + ", and " + noContainerImageReason(name, opts)
			}
			msg := hostUnavailable + " and no container image configured — set security.tools." + name + ".image (digest-pinned) to enable container fallback, run `aegis security build-image` for the shared multiscanner image, or `aegis security install " + name + "` for a guided host install"
			if hostBroken != "" {
				// No "guided host install" suffix here: installing the binary
				// is precisely what doesn't help on this platform.
				msg = hostUnavailable + ", and no container image is configured to run it instead — run `aegis security build-image` for the shared multiscanner image, or set security.tools." + name + ".image (digest-pinned)"
			} else if d.WSLCapable {
				msg = d.Binary + " not installed (no native Windows build) and not found inside WSL either — run `aegis security install " + name + "` for a guided install, or `aegis security build-image` for the shared multiscanner image"
			}
			return MethodNone, "", "", msg
		}
		if hostBroken != "" {
			return MethodNone, "", "", hostUnavailable + ", and no container runtime is available (docker/podman) to run it instead — start one (on Windows with Podman: `podman machine start`)"
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

// multiscannerNoRuntimeReason explains that the runtime which built the shared
// image isn't answering. Shared by the explicit method: container branch and
// the "auto" fallback note so the two never drift — a locally-built image lives
// only in the storage of the engine that built it, so "no runtime" here always
// means that specific engine, never "install docker".
func multiscannerNoRuntimeReason(opts Options) string {
	return "the multiscanner image was built with " + string(opts.Multiscanner.Runtime) + ", which isn't available now — start it (on Windows with Podman: `podman machine start`), or re-run `aegis security build-image` to rebuild with an available runtime"
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
	if binary == "gosec" {
		// The one compile-assisted scanner in the image needs its module cache
		// too, warmed by the phase that ran before this one (P55.8).
		cliArgs = withVolume(cliArgs, image, MultiscannerGoCacheVolume+":"+multiscannerGoCacheMount)
	}
	return runContainerCLI(ctx, rt, image, cliArgs)
}

// withCacheVolume inserts the scanner cache mount just before the image
// reference, which is where a run's flags have to stop and its command begins.
func withCacheVolume(cliArgs []string, image string) []string {
	return withVolume(cliArgs, image, MultiscannerCacheVolume+":"+multiscannerCacheMount)
}

// withVolume inserts a -v mount just before the image reference — the point
// where a run's flags have to stop and its command begins. Returns cliArgs
// unchanged if the reference isn't there, so a caller can't accidentally append
// a mount flag into the container's argv.
func withVolume(cliArgs []string, image, spec string) []string {
	for i, a := range cliArgs {
		if a == image {
			out := make([]string, 0, len(cliArgs)+2)
			out = append(out, cliArgs[:i]...)
			out = append(out, "-v", spec)
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
	out, err := runBoundedOutput(cmd)
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
	cliArgs = append(cliArgs, sandbox.OCIHardeningFlags(rt)...)
	cliArgs = append(cliArgs, "-v", sandbox.HostMountPath(rt, dir)+":/src", "-w", "/src", image)
	return append(cliArgs, args...)
}
