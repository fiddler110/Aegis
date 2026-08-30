package config

// SecurityConfig configures contextual security policies.
type SecurityConfig struct {
	EgressThenWrite  bool     `koanf:"egress_then_write"` // require approval for writes after network egress
	NetworkAllowList []string `koanf:"network_allowlist"` // restrict network calls to these domains (empty = no restriction)

	// RedactSecrets runs a read-capability tool's output through the same
	// gitleaks-backed secret detection used for PR title/body scanning
	// (security.ScanText, P24.6 / FIND-13) before it's appended to the
	// conversation sent to the configured model provider (P24.12 /
	// FIND-09). Any detected secret pattern is masked as
	// "[REDACTED:<rule>]". Defaults to true (P27.3/FIND-05): sending file/
	// conversation content carrying an unredacted secret to a cloud model
	// provider is a real exposure with no other default control. It
	// remains a best-effort, gitleaks-only pass (needs the binary on PATH,
	// only catches its known secret patterns, fails open if the binary is
	// missing) and shells out per read-tool call, adding latency — set
	// redact_secrets: false only when that cost is unacceptable and
	// content is otherwise known-safe, or prefer local Ollama usage, which
	// never sends file content off the machine at all and remains the
	// strongest mitigation for genuinely sensitive codebases. See
	// docs/providers.md "Data Exposure & Redaction".
	RedactSecrets bool `koanf:"redact_secrets"`

	// Tools configures per-scanner behavior for `aegis scan`/the security_scan
	// tool (P11.11): whether it's enabled, how it runs (host binary vs
	// container image), and its digest-pinned image override. Keyed by
	// scanner name (opengrep, trivy, gitleaks, ...); a name with no entry uses
	// DefaultMethod and runs enabled with no image override.
	Tools map[string]SecurityToolConfig `koanf:"tools"`
	// DefaultMethod is the resolver method for any scanner with no entry in
	// Tools: "host" (never fall back to a container), "container" (always
	// prefer the container image), or "auto"/"" (host if present, else
	// container) — the default.
	DefaultMethod string `koanf:"default_method"`

	// Multiscanner configures the single locally-built image that carries
	// every bundled scanner, so an operator provisions once instead of
	// installing each tool (or configuring a container image per tool).
	// Written by `aegis security build-image`; see MultiscannerConfig.
	Multiscanner MultiscannerConfig `koanf:"multiscanner"`

	// Netscanner points at the second locally-built image, the one for tools
	// that scan a remote target rather than local source. Written by
	// `aegis security build-image --netscanner`; see NetscannerConfig.
	Netscanner NetscannerConfig `koanf:"netscanner"`

	// WSLDistro names a specific registered WSL distro (e.g. "kali-linux") to
	// target for every WSLCapable scanner (nmap, nuclei, opengrep, kubescape;
	// P14.x), instead of whatever `wsl --set-default` currently points at.
	// Empty uses WSL's own default-distro selection. On Windows, a Linux
	// distro purpose-built for security tooling (Kali) is the recommended
	// target for red-team/recon work — see docs/security.md.
	WSLDistro string `koanf:"wsl_distro"`

	// DAST configures the dast_scan tool's target-authorization policy
	// (P11.7) — enforced unconditionally inside the tool itself, not just
	// advisory permission rules, since an agent pointing an active scanner
	// at an arbitrary host is an abuse primitive.
	DAST DASTConfig `koanf:"dast"`

	// Debate gates the two opt-in integration points between the P12
	// multi-agent-debate mechanism and existing security workflows (P12.5):
	// threat-model entries and audit-triage findings. Both default false —
	// debate multiplies model calls per item, so it's a deliberate opt-in,
	// never a silent behavior change to the existing single-pass workflows.
	Debate DebateIntegrationConfig `koanf:"debate"`
}

// MultiscannerConfig points at the locally-built image that bundles every
// scanner Aegis can drive against a directory. It exists because the
// alternative — ScannerDescriptor.DefaultImage — is deliberately empty for
// every tool (a scanner image is supply-chain attack surface, so Aegis never
// ships an unverified default), which left the container path resolving to
// MethodNone out of the box unless an operator pinned 16 separate images.
//
// The pin here is an image *ID*, not a registry digest, and that difference is
// the point. A locally-built image has no registry digest at all — RepoDigests
// stays empty until a push or pull — so the `image@sha256:...` reference the
// per-tool Image field requires cannot exist for one. Rather than weaken that
// rule, this path replaces it with a stronger check: `<runtime> image inspect`
// is asked for the image's real ID before the first container run of a scan
// and compared against ImageID. An image rebuilt or retagged behind Aegis's
// back fails closed with a specific reason, instead of a regex on a reference
// string vouching for content it never looked at.
type MultiscannerConfig struct {
	// Enabled turns the shared image on as the container-method image for
	// every bundled scanner. False (the default) leaves resolution exactly as
	// it was: per-tool images only.
	Enabled bool `koanf:"enabled"`
	// Image is the local reference `aegis security build-image` tagged, e.g.
	// "localhost/aegis-multiscanner:v1". Never pulled — it must already exist
	// in the runtime's local storage, and its ID must match ImageID.
	Image string `koanf:"image"`
	// Runtime is the container runtime that built the image ("podman",
	// "docker", ...), recorded because a locally-built image exists only in
	// the storage of the runtime that built it. Without it, resolution would
	// fall back to auto-detection and could pick a different runtime than the
	// build did — DetectBest returns the first available engine in priority
	// order, not the one that built anything, so on a machine with both
	// installed a docker-built image would be reported missing even though it
	// exists, because podman answered first. Empty falls back to
	// auto-detection (the pre-multiscanner behavior).
	Runtime string `koanf:"runtime"`
	// ImageID is the full "sha256:..." ID of the image as built, recorded by
	// `aegis security build-image` and re-verified before use. Empty means
	// "never built" — resolution reports that rather than running whatever
	// currently answers to Image.
	ImageID string `koanf:"image_id"`
	// SourceFingerprint is a hash of the Containerfile and scripts the image
	// was built from, recorded alongside ImageID. It closes the gap ImageID
	// cannot see: an image can match its pin perfectly and still predate the
	// source it claims to be built from, which is how a pinned image went two
	// commits stale and silently lacked a scanner entirely.
	//
	// Empty means "unknown", not "drift" — configs written before this field
	// existed have a perfectly good image, and flagging every one of them on
	// upgrade would be noise, not a finding.
	SourceFingerprint string `koanf:"source_fingerprint"`
	// Concurrency bounds how many scanners run at once during a scan. Each
	// container-method scanner is one container, so this is how many run in
	// parallel. 0 means the built-in default (multiscannerDefaultConcurrency);
	// 1 restores strictly sequential execution.
	Concurrency int `koanf:"concurrency"`
	// Tools optionally restricts which scanners resolve to the shared image.
	// Empty (the default) means every scanner the image is known to carry.
	// A per-tool security.tools.<name>.image always wins over this.
	Tools []string `koanf:"tools"`
}

// NetscannerConfig points at the second locally-built scanner image (P55.7),
// the one carrying tools that scan a *remote* target — nmap and nuclei against
// a host list, trivy/grype against a container image reference.
//
// It is a separate image from the multiscanner, and separate for exactly one
// reason: mount posture. Every tool here needs network egress and none of them
// needs the workspace, so this image runs with network ON and no workspace
// mounted, ever — while the multiscanner keeps --network none with the
// workspace mounted. The two runners are separate functions with separate
// signatures (the netscanner's has no directory parameter at all), so the
// invariant is structural rather than a convention someone has to remember.
//
// Pinning works exactly as MultiscannerConfig's does, for the same reason: a
// locally-built image has no registry digest, so its real image ID is recorded
// and re-verified before every run.
type NetscannerConfig struct {
	// Enabled turns the network-facing image on as the container path for the
	// tools it carries. False (the default) leaves image scanning and network
	// recon host-binary-only, exactly as they were before this image existed.
	Enabled bool `koanf:"enabled"`
	// Image is the local reference `aegis security build-image --netscanner`
	// tagged, e.g. "localhost/aegis-netscanner:v1". Never pulled.
	Image string `koanf:"image"`
	// Runtime is the container runtime that built the image, recorded because a
	// locally-built image exists only in that engine's storage.
	Runtime string `koanf:"runtime"`
	// ImageID is the full "sha256:..." ID as built, re-verified before use.
	// Empty means "never built".
	ImageID string `koanf:"image_id"`
	// SourceFingerprint is a hash of the build context the image came from.
	// Both images are built from one context, so this is the same hash the
	// multiscanner records. Empty means "unknown", not "drift".
	SourceFingerprint string `koanf:"source_fingerprint"`
	// Tools optionally restricts which scanners resolve to this image. Empty
	// (the default) means every scanner the image is known to carry.
	Tools []string `koanf:"tools"`
}

// DebateIntegrationConfig toggles routing specific existing security
// workflows through a P12 debate round before they finalize their output.
// This only controls the instruction text injected into the system prompt
// (server.effectiveSystem) — the model still decides per-item whether a
// debate is warranted; the toggle controls whether it's told to consider one
// at all.
type DebateIntegrationConfig struct {
	// ThreatModel enables the security-architect persona's threat-modeling
	// workflow to route each identified threat/mitigation pair through a
	// debate round before writing it into the threat model document.
	ThreatModel bool `koanf:"threat_model"`
	// Triage enables the security-audit skill's triage loop to route a
	// borderline or disputed-severity finding through a debate round before
	// deciding whether to suppress it via the baseline (P11.8).
	Triage bool `koanf:"triage"`
}

// DASTConfig is the hard authorization gate for DAST scanning (P11.7): a
// dast_scan call always resolves its target's host against this policy
// before ever launching ZAP, regardless of permission mode. Loopback and
// RFC-1918 private addresses are always allowed (the common "scan my
// locally running app" case needs no config); anything else must be
// explicitly declared here.
type DASTConfig struct {
	// AllowedTargets is a list of exact hostnames, ".suffix" subdomain
	// wildcards, or CIDR ranges an operator has explicitly authorized for
	// scanning, in addition to the built-in loopback/RFC-1918 default-allow.
	// Hostnames are matched as literal strings, never DNS-resolved — a
	// target's declared identity can't be silently changed by whatever it
	// happens to resolve to at scan time (ZAP does its own resolution inside
	// the container, outside Aegis's control).
	//
	// Sourced from user/global config only (P27.9/FIND-11): Load()
	// unconditionally overwrites this field with the project-excluded
	// baseline after unmarshalling, so a project .aegis/config.yaml can
	// never widen it — not even once the directory is `aegis trust`-ed,
	// unlike the P27.1 trust gate's other frozen-until-trusted keys. An
	// active scanner authorized against arbitrary Internet hosts via a
	// cloned repo's config is a materially different risk than that repo
	// merely widening its own permission mode.
	AllowedTargets []string `koanf:"allowed_targets"`
	// AllowActive gates active/api scan modes (which send real attack
	// payloads, not just passive observation) behind an explicit one-time
	// opt-in, separate from the per-call approval prompt every dast_scan
	// call already gets from its execute capability. Default false.
	AllowActive bool `koanf:"allow_active"`
}

// SecurityToolConfig configures one security scanner (P11.11).
type SecurityToolConfig struct {
	// Enabled defaults to true (the zero value); set false to always skip
	// this tool. A *bool (not bool) so "unset" is distinguishable from an
	// explicit false when merging config layers.
	Enabled *bool `koanf:"enabled"`
	// Method overrides SecurityConfig.DefaultMethod for this tool: "host",
	// "container", or "auto".
	Method string `koanf:"method"`
	// Install controls whether Aegis may install this tool automatically
	// when missing (P11.10): "prompt" (default — ask before installing),
	// "always" (pre-authorized, no prompt), or "never" (use only if already
	// present, don't offer to install).
	Install string `koanf:"install"`
	// Image is a digest-pinned container image reference
	// (image@sha256:...) used for this tool's container fallback. Required
	// to enable container execution — see security.ScannerDescriptor's doc
	// comment for why Aegis ships no built-in default.
	Image string `koanf:"image"`
	// TemplatesVersion pins the nuclei scanner's nuclei-templates release tag
	// (P13.5.6) — meaningless for every other tool. Templates are executable
	// network-probe logic, so nuclei never runs against an unpinned "latest"
	// template set the same way a scanner container image is never used
	// without a digest pin.
	TemplatesVersion string `koanf:"templates_version"`
	// Verify enables trufflehog's live credential verification (P13.2):
	// each detected secret is confirmed against the real provider API
	// (AWS/GitHub/etc.) instead of just pattern/entropy-matched. Meaningless
	// for every other tool. Default false — verification makes real calls to
	// third-party services using the actual discovered secret, and is
	// host-only (security.Resolve refuses container mode when this is set,
	// the same host-only carve-out image scanning already has).
	Verify bool `koanf:"verify"`
}

// ToolEnabled reports whether c enables the tool (default true).
func (c SecurityToolConfig) ToolEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}
