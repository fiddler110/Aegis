package security

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// ReconOptions is one recon_scan call's parameters (P13.5): network-target
// reconnaissance and vulnerability scanning across a host/CIDR list, for
// attack-surface mapping against an authorized target (e.g. a home lab).
type ReconOptions struct {
	// Targets are bare hosts, IPs, or CIDRs — no http(s):// scheme required
	// (unlike DASTOptions.Target). "192.168.1.0/24", "10.0.0.5", "db.lan".
	Targets []string
	// AllowedTargets and AllowActive deliberately reuse
	// config.DASTConfig.AllowedTargets/AllowActive (P13.5.1/P13.5.2) — one
	// shared network-target authorization policy for every tool that reaches
	// out to a network target (ZAP, nmap, nuclei), not a second gate to keep
	// in sync.
	AllowedTargets []string
	AllowActive    bool
}

// maxReconTargets caps how many hosts a single recon_scan call may target
// (P13.5.3) — a hard ceiling, not a silent truncation, so a runaway or
// mistaken host list fails loudly instead of scanning a partial list.
const maxReconTargets = 256

// ReconScanner is one network-target scanning tool (nmap, nuclei) — the
// recon_scan analogue of Scanner (which analyzes a source directory) and
// ImageScanner (which analyzes a container image by reference).
type ReconScanner interface {
	Name() string
	Resolve(ctx context.Context, opts Options) (method Method, runtime sandbox.ContainerRuntime, image string, reason string)
	// Scan runs against targets (already validated by RunRecon's target-
	// authorization gate — see isHostAllowed). active mirrors DASTOptions'
	// baseline/active split: false runs the safe/passive default, true
	// unlocks more aggressive checks, gated by the same
	// security.dast.allow_active flag as ZAP.
	Scan(ctx context.Context, targets []string, active bool, method Method, runtime sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error)
}

// DefaultReconScanners returns the built-in network recon scanners: nmap
// (port/service/version discovery — the attack-surface-mapping step) and
// nuclei (template-driven vulnerability/misconfiguration matching against
// whatever nmap found alive).
func DefaultReconScanners() []ReconScanner {
	return []ReconScanner{nmapScanner{}, nucleiScanner{}}
}

// RunRecon is the P13.5 entry point: validates every target individually
// against the shared network-target authorization gate (P13.5.3 — a single
// bad host fails the whole call, never a partial silent skip), then runs
// every resolvable ReconScanner and aggregates findings the same way
// RunWithOptions aggregates the file-based Scanner interface. Every policy
// check here runs unconditionally, independent of the caller's permission
// mode — same posture as RunDAST.
func RunRecon(ctx context.Context, opts ReconOptions, scanOpts Options) (Report, error) {
	rep := newReport()

	if len(opts.Targets) == 0 {
		return rep, errors.New("recon_scan requires at least one target")
	}
	if len(opts.Targets) > maxReconTargets {
		return rep, fmt.Errorf("recon_scan supports at most %d targets per call, got %d", maxReconTargets, len(opts.Targets))
	}

	var disallowed []string
	for _, target := range opts.Targets {
		// Rejected before anything else looks at it: a target is appended to an
		// argv (host path) or handed to a container as a positional parameter
		// (P55.7), and in both places a leading "-" reads as a flag to the tool
		// rather than as a host. Neither nmap nor nuclei has a hostname that
		// legitimately starts with one.
		if strings.HasPrefix(strings.TrimSpace(target), "-") {
			disallowed = append(disallowed, fmt.Sprintf("%q (a target may not start with \"-\" — it would be read as a scanner flag)", target))
			continue
		}
		host, err := targetHost(target)
		if err != nil {
			disallowed = append(disallowed, fmt.Sprintf("%q (%v)", target, err))
			continue
		}
		if allowed, reason := isHostAllowed(host, opts.AllowedTargets); !allowed {
			disallowed = append(disallowed, fmt.Sprintf("%q (%s)", target, reason))
		}
	}
	if len(disallowed) > 0 {
		return rep, fmt.Errorf("target authorization failed for: %s", strings.Join(disallowed, "; "))
	}

	// MethodContainer is no longer a skip here (P55.7). It used to be, and the
	// reason was sound at the time: the only container runner Aegis had denied
	// network outright, so "run nmap in a container" meant "run nmap where it
	// cannot reach anything". The netscanner image inverts exactly that one
	// property — network on, no workspace mounted — which is what a tool
	// pointed at a remote target needs and what a tool pointed at local source
	// must never have.
	for _, sc := range DefaultReconScanners() {
		method, rt, image, reason := sc.Resolve(ctx, scanOpts)
		if method == MethodNone {
			rep.Skipped[sc.Name()] = reason
			continue
		}
		findings, err := sc.Scan(ctx, opts.Targets, opts.AllowActive, method, rt, image, scanOpts)
		rep.record(sc.Name(), method, findings, err)
	}

	rep.Findings = DedupFindings(rep.Findings)
	assignASVS(rep.Findings)
	rep.sortFindings()
	return rep, nil
}

// targetHost extracts the bare host/IP isHostAllowed should gate on from one
// recon target: a CIDR's network address, a host:port pair's host, or the
// target itself when it's already a bare host/IP.
func targetHost(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("empty target")
	}
	if strings.Contains(target, "/") {
		ip, _, err := net.ParseCIDR(target)
		if err != nil {
			return "", fmt.Errorf("invalid CIDR: %w", err)
		}
		return ip.String(), nil
	}
	if host, _, err := net.SplitHostPort(target); err == nil {
		return host, nil
	}
	return target, nil
}

// ---- nmap: port/service/version discovery ----

type nmapScanner struct{}

func (nmapScanner) Name() string { return "nmap" }

func (nmapScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return ResolveNetwork(ctx, "nmap", opts)
}

// nmapContainerReportPath is where nmap writes its XML inside the netscanner
// container. A path, not /dev/stdout: nmap's XML writer seeks, and with no
// workspace mount there is no shared file to read back either way, so the report
// is written inside and cat'd out (see netscannerCollect).
const nmapContainerReportPath = "/tmp/aegis-nmap.xml"

// Scan runs nmap against targets and parses its XML output into Findings.
// No shell string-building for the host path: exec.CommandContext takes an
// explicit argv slice built only from already-validated targets, never a
// concatenated command string. The MethodWSL path (P14.x) runs the same argv
// inside a WSL distro instead — recommended on Windows, where the native
// build commonly fails without Npcap set up correctly / admin rights for
// OS-detection scans: the XML output path is translated to its WSL mount
// form via sandbox.WSLPath so nmap-inside-WSL writes to the exact same NTFS
// file this function reads back below, with no cross-boundary file copy
// needed.
func (nmapScanner) Scan(ctx context.Context, targets []string, active bool, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		// No host temp file at all on this path: with nothing mounted, the
		// report only exists inside the container, so it comes back over stdout.
		data, err := runNetscannerReport(ctx, rt, image, "nmap", nmapContainerReportPath,
			nmapArgs(targets, active, nmapContainerReportPath))
		if err != nil {
			return nil, err
		}
		return parseNmapXML(data)
	}

	tmpFile, err := os.CreateTemp("", "aegis-nmap-*.xml")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	var out []byte
	if method == MethodWSL {
		wslOutPath, werr := sandbox.WSLPath(ctx, tmpPath, opts.WSLDistro)
		if werr != nil {
			return nil, werr
		}
		out, _ = sandbox.RunWSLScript(ctx, opts.WSLDistro, "nmap", nmapArgs(targets, active, wslOutPath)...)
	} else {
		cmd := exec.CommandContext(ctx, "nmap", nmapArgs(targets, active, tmpPath)...)
		out, _ = cmd.CombinedOutput()
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("nmap did not produce xml output (output: %s): %w", strings.TrimSpace(string(out)), err)
	}
	return parseNmapXML(data)
}

// nmapArgs builds nmap's argv slice for one Scan call, split out from Scan
// so it's directly unit-testable without invoking a real nmap binary.
// Baseline: version-detection scan of the top 100 ports, no OS fingerprinting
// or NSE scripts. Active (security.dast.allow_active): adds OS detection,
// the full port range, and nmap's default-safe NSE script category — gated
// by the same flag ZAP's active mode uses.
func nmapArgs(targets []string, active bool, xmlOutPath string) []string {
	args := []string{"-sV", "--top-ports", "100", "-T4", "-Pn", "-oX", xmlOutPath}
	if active {
		args = append(args, "-sC", "-O", "-p-")
	}
	return append(args, targets...)
}

type nmapRun struct {
	Hosts []nmapHost `xml:"host"`
}

type nmapHost struct {
	Addresses []nmapAddress `xml:"address"`
	Ports     nmapPorts     `xml:"ports"`
}

type nmapAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

type nmapPorts struct {
	Port []nmapPort `xml:"port"`
}

type nmapPort struct {
	Protocol string      `xml:"protocol,attr"`
	PortID   string      `xml:"portid,attr"`
	State    nmapState   `xml:"state"`
	Service  nmapService `xml:"service"`
}

type nmapState struct {
	State string `xml:"state,attr"`
}

type nmapService struct {
	Name    string `xml:"name,attr"`
	Product string `xml:"product,attr"`
	Version string `xml:"version,attr"`
}

func parseNmapXML(data []byte) ([]Finding, error) {
	var run nmapRun
	if err := xml.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("parse nmap xml: %w", err)
	}
	var findings []Finding
	for _, h := range run.Hosts {
		host := nmapHostAddress(h)
		for _, p := range h.Ports.Port {
			if p.State.State != "open" {
				continue
			}
			findings = append(findings, buildNmapFinding(host, p))
		}
	}
	return findings, nil
}

func nmapHostAddress(h nmapHost) string {
	for _, a := range h.Addresses {
		if a.AddrType == "ipv4" || a.AddrType == "ipv6" {
			return a.Addr
		}
	}
	if len(h.Addresses) > 0 {
		return h.Addresses[0].Addr
	}
	return "unknown"
}

// riskyService documents one port/protocol combination worth flagging above
// plain informational "port is open" — the concrete "identify weaknesses"
// value nmap's recon adds beyond a bare port list.
type riskyService struct {
	severity    Severity
	remediation string
}

// riskyPorts is keyed by "<port>/<protocol>". Deliberately a short, common
// list (not exhaustive) — services frequently found unintentionally exposed
// on a home lab / internal network and worth calling out above INFO.
var riskyPorts = map[string]riskyService{
	"23/tcp":   {SevHigh, "Telnet transmits credentials in plaintext and is largely obsolete — disable it or replace with SSH."},
	"21/tcp":   {SevMedium, "FTP transmits credentials in plaintext by default — disable if unused, or require FTPS/SFTP."},
	"512/tcp":  {SevHigh, "rexec is unauthenticated/plaintext — disable unless explicitly required."},
	"513/tcp":  {SevHigh, "rlogin is unauthenticated/plaintext — disable unless explicitly required."},
	"514/tcp":  {SevHigh, "rsh is unauthenticated/plaintext — disable unless explicitly required."},
	"6379/tcp": {SevHigh, "Redis has no authentication by default — confirm requirepass/ACLs are set, or bind to loopback/private only."},
	"2375/tcp": {SevCritical, "Unencrypted Docker API grants full host control to anyone who can reach it — bind to loopback, require TLS on 2376, or disable remote access."},
	"445/tcp":  {SevMedium, "SMB exposure is a common lateral-movement vector — restrict to trusted networks and confirm SMBv1 is disabled."},
	"139/tcp":  {SevMedium, "NetBIOS/SMB exposure is a common lateral-movement vector — restrict to trusted networks."},
	"3389/tcp": {SevMedium, "RDP exposed beyond a trusted network is a common ransomware entry point — restrict via VPN/firewall and enforce NLA plus strong auth."},
	"5900/tcp": {SevMedium, "VNC is often unauthenticated or weakly authenticated — restrict access and confirm a strong password/tunnel is in place."},
	"9200/tcp": {SevHigh, "Elasticsearch has no authentication by default — confirm security features are enabled, or bind to loopback/private only."},
}

func buildNmapFinding(host string, p nmapPort) Finding {
	service := p.Service.Name
	if service == "" {
		service = "unknown"
	}
	sev := SevInfo
	remediation := "Informational — confirm this service is intentionally exposed on this network."
	if risky, ok := riskyPorts[p.PortID+"/"+p.Protocol]; ok {
		sev = risky.severity
		remediation = risky.remediation
	}
	desc := "Port is open and accepting connections."
	if productVersion := strings.TrimSpace(p.Service.Product + " " + p.Service.Version); productVersion != "" {
		desc = fmt.Sprintf("%s Detected: %s.", desc, productVersion)
	}
	return Finding{
		Tool:        "nmap",
		RuleID:      fmt.Sprintf("open-port-%s-%s", p.PortID, p.Protocol),
		Severity:    sev,
		Title:       fmt.Sprintf("Open port: %s (%s/%s)", service, p.PortID, p.Protocol),
		Location:    fmt.Sprintf("%s:%s/%s", host, p.PortID, p.Protocol),
		Description: desc,
		Remediation: remediation,
	}
}

// ---- nuclei: template-driven vulnerability/misconfiguration scanning ----

type nucleiScanner struct{}

func (nucleiScanner) Name() string { return "nuclei" }

func (nucleiScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return ResolveNetwork(ctx, "nuclei", opts)
}

// nucleiContainerReportPath is where nuclei writes its SARIF inside the
// netscanner container, for the same reason nmap's report does.
const nucleiContainerReportPath = "/tmp/aegis-nuclei.sarif"

// Scan mirrors nmapScanner.Scan's MethodWSL handling: both the pinned
// templates directory and the SARIF output path are translated to their WSL
// mount form via sandbox.WSLPath so nuclei-inside-WSL reads/writes the exact
// same files this function already has open on the host side.
func (nucleiScanner) Scan(ctx context.Context, targets []string, active bool, method Method, rt sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error) {
	if method == MethodContainer {
		// The pin is satisfied by the image rather than by config here: the
		// netscanner bakes a pinned nuclei-templates release
		// (NUCLEI_TEMPLATES_VERSION), covered by the same image-ID verification
		// as the binaries beside it. So security.tools.nuclei.templates_version
		// is not required on this path — and, more to the point, the host
		// path's git clone of an unpinned working copy isn't either.
		data, err := runNetscannerReport(ctx, rt, image, "nuclei", nucleiContainerReportPath,
			nucleiArgs(targets, active, netscannerTemplatesDir, nucleiContainerReportPath))
		if err != nil {
			return nil, err
		}
		return ParseSARIF(data, "nuclei")
	}

	templatesDir, err := resolveNucleiTemplates(ctx, opts)
	if err != nil {
		return nil, err
	}

	tmpFile, err := os.CreateTemp("", "aegis-nuclei-*.sarif")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	var out []byte
	if method == MethodWSL {
		wslTemplatesDir, werr := sandbox.WSLPath(ctx, templatesDir, opts.WSLDistro)
		if werr != nil {
			return nil, werr
		}
		wslOutPath, werr := sandbox.WSLPath(ctx, tmpPath, opts.WSLDistro)
		if werr != nil {
			return nil, werr
		}
		out, _ = sandbox.RunWSLScript(ctx, opts.WSLDistro, "nuclei", nucleiArgs(targets, active, wslTemplatesDir, wslOutPath)...)
	} else {
		cmd := exec.CommandContext(ctx, "nuclei", nucleiArgs(targets, active, templatesDir, tmpPath)...)
		out, _ = cmd.CombinedOutput()
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("nuclei did not produce a sarif report (output: %s): %w", strings.TrimSpace(string(out)), err)
	}
	return ParseSARIF(data, "nuclei")
}

// nucleiArgs builds nuclei's argv slice for one Scan call, split out from
// Scan so it's directly unit-testable without invoking a real nuclei binary.
func nucleiArgs(targets []string, active bool, templatesDir, sarifOutPath string) []string {
	args := []string{"-t", templatesDir, "-duc", "-silent"}
	if !active {
		// Safe-by-default (P13.5.4): exclude dos/fuzz/intrusive-tagged
		// templates unless the same security.dast.allow_active flag ZAP
		// uses is set.
		args = append(args, "-etags", "dos,fuzz,intrusive")
	}
	for _, t := range targets {
		args = append(args, "-target", t)
	}
	return append(args, "-sarif-export", sarifOutPath)
}

// nucleiTemplatesVersionPattern restricts security.tools.nuclei.
// templates_version to a safe git tag/ref shape (P31.1): the value is used
// unsanitized both as a filepath.Join path segment (the per-version cache
// dir) and as the `git clone --branch <version>` argument, so without this
// a value containing "../" would escape nucleiTemplatesBaseDir() and a
// leading "-" could be interpreted as a git flag.
var nucleiTemplatesVersionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// resolveNucleiTemplates enforces P13.5.6's template pinning: nuclei never
// runs against an implicit "latest" template pull. security.tools.nuclei.
// templates_version must name a nuclei-templates release tag; the tag is
// shallow-cloned once into a per-version cache directory and reused after
// that (a version already present on disk is never re-cloned/updated).
func resolveNucleiTemplates(ctx context.Context, opts Options) (string, error) {
	version := strings.TrimSpace(opts.Tools["nuclei"].TemplatesVersion)
	if version == "" {
		return "", errors.New("nuclei requires a pinned template set — set security.tools.nuclei.templates_version to a nuclei-templates release tag (e.g. \"v9.9.0\"); templates are executable network-probe logic and are never pulled at an unpinned \"latest\"")
	}
	if strings.HasPrefix(version, "-") || !nucleiTemplatesVersionPattern.MatchString(version) {
		return "", fmt.Errorf("security.tools.nuclei.templates_version %q is not a valid release tag — only letters, digits, '.', '_', '-' are allowed, and it must not start with '-'", version)
	}
	// A pure-dot value ("." / ".." / "...") satisfies the character class above
	// — '.' is a legitimate tag character — but names a relative path segment
	// rather than a tag, and filepath.Join resolves it against the cache dir
	// instead of nesting under it (".." lands in its parent, which is then
	// handed to nuclei as its template set). The pattern already excludes both
	// separators, so this is the only traversal shape that can reach the Join.
	if strings.Trim(version, ".") == "" {
		return "", fmt.Errorf("security.tools.nuclei.templates_version %q is not a valid release tag — it is a relative path segment, not a version", version)
	}
	dir := filepath.Join(nucleiTemplatesBaseDir(), version)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	if err := cloneNucleiTemplates(ctx, version, dir); err != nil {
		return "", err
	}
	return dir, nil
}

// nucleiTemplatesCacheDir is a seam for tests.
var nucleiTemplatesCacheDir = func() (string, error) { return os.UserCacheDir() }

func nucleiTemplatesBaseDir() string {
	base, err := nucleiTemplatesCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "aegis", "nuclei-templates")
}

func cloneNucleiTemplates(ctx context.Context, version, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", version,
		"https://github.com/projectdiscovery/nuclei-templates.git", dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone nuclei-templates@%s: %w (output: %s)", version, err, strings.TrimSpace(string(out)))
	}
	return nil
}
