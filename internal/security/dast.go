package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
	yaml "go.yaml.in/yaml/v3"
)

// DASTMode selects which ZAP Automation Framework jobs a dast_scan run
// executes (P11.7).
type DASTMode string

const (
	DASTModeBaseline DASTMode = "baseline" // passive: spider + passive scan only, no attack payloads
	DASTModeActive   DASTMode = "active"   // + active scan: sends real attack payloads
	DASTModeAPI      DASTMode = "api"      // OpenAPI-defined endpoints + active scan
)

// DASTOptions is one dast_scan call's parameters and the operator policy
// every call is checked against.
type DASTOptions struct {
	Target string   // http(s) URL of the running application to scan
	Mode   DASTMode // "" defaults to DASTModeBaseline
	// APIDefinition is an OpenAPI spec URL or path; required for DASTModeAPI.
	APIDefinition string
	// AllowedTargets and AllowActive mirror config.DASTConfig — passed
	// explicitly rather than importing internal/config, the same
	// decoupling internal/sandbox and this package already follow elsewhere.
	AllowedTargets []string
	AllowActive    bool
}

// RunDAST is the P11.7 entry point: validates the target and scan mode
// against policy, resolves the ZAP scanner the same way every other tool
// does (security.Resolve), runs it, and parses its SARIF report into a
// Report. Every policy check here runs unconditionally, independent of the
// caller's permission mode — an active DAST scan against a host the
// operator hasn't authorized is an attack, not a policy nuance that
// approval alone should gate.
func RunDAST(ctx context.Context, dastOpts DASTOptions, scanOpts Options) (Report, error) {
	rep := newReport()

	mode := dastOpts.Mode
	if mode == "" {
		mode = DASTModeBaseline
	}

	if allowed, reason := isDASTTargetAllowed(dastOpts.Target, dastOpts.AllowedTargets); !allowed {
		return rep, errors.New(reason)
	}
	if mode != DASTModeBaseline && !dastOpts.AllowActive {
		return rep, fmt.Errorf("%s scanning sends real attack payloads and is disabled by default — set security.dast.allow_active: true to enable it", mode)
	}

	method, rt, image, reason := Resolve(ctx, "zap", scanOpts)
	if method == MethodNone {
		rep.Skipped["zap"] = reason
		return rep, nil
	}
	if method != MethodContainer {
		// zap has no descriptor.Binary (container-only, see the "zap"
		// descriptor's doc comment) so Resolve should never return
		// MethodHost for it — guard anyway rather than silently
		// misbehaving if that assumption ever changes.
		return rep, fmt.Errorf("zap resolved to unexpected method %q (it is container-only)", method)
	}

	planBytes, err := buildZAPPlan(mode, dastOpts.Target, dastOpts.APIDefinition)
	if err != nil {
		return rep, err
	}

	workDir, err := os.MkdirTemp("", "aegis-dast-*")
	if err != nil {
		return rep, err
	}
	defer os.RemoveAll(workDir)
	// The official zaproxy image runs as its own non-root "zap" user rather
	// than root; overriding that with --user would weaken the image's own
	// hardening, so instead the mounted work directory is made
	// world-writable — the workaround ZAP's own docs recommend for the
	// resulting host-UID/container-UID mount-permission mismatch.
	if err := os.Chmod(workDir, 0o777); err != nil {
		return rep, err
	}
	reportsDir := filepath.Join(workDir, "reports")
	if err := os.MkdirAll(reportsDir, 0o777); err != nil {
		return rep, err
	}
	if err := os.Chmod(reportsDir, 0o777); err != nil {
		return rep, err
	}
	if err := os.WriteFile(filepath.Join(workDir, "zap.yaml"), planBytes, 0o644); err != nil {
		return rep, err
	}

	runOut, _ := runZAPContainer(ctx, rt, image, workDir)

	reportPath := filepath.Join(reportsDir, zapReportFile)
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return rep, fmt.Errorf("zap did not produce a report (container output: %s): %w", strings.TrimSpace(string(runOut)), err)
	}
	findings, err := ParseSARIF(data, "zap")
	if err != nil {
		return rep, err
	}
	rep.record("zap", method, findings, nil)
	rep.sortFindings()
	return rep, nil
}

// runZAPContainer runs ZAP's Automation Framework against the plan already
// written to workDir/zap.yaml. ZAP commonly exits non-zero purely because it
// found vulnerabilities (matching every other scanner's non-zero-on-findings
// convention in this package) — the report file on disk, not the exit code,
// is the real success signal, checked by the caller.
func runZAPContainer(ctx context.Context, rt sandbox.ContainerRuntime, image, workDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, string(rt), zapContainerRunArgs(rt, image, workDir)...)
	out, _ := cmd.CombinedOutput()
	return out, nil
}

// zapContainerRunArgs mirrors containerRunArgs' hardening (no Linux
// capabilities, no privilege escalation) but deliberately omits
// --network none: DAST's entire purpose is reaching the scan target over
// the network — the one documented exception to every other scanner
// container's network isolation (see runContainerImage's doc comment).
func zapContainerRunArgs(rt sandbox.ContainerRuntime, image, workDir string) []string {
	args := []string{"run", "--rm"}
	if rt != sandbox.RuntimeAppleContainers {
		args = append(args, "--cap-drop=ALL", "--security-opt=no-new-privileges")
	}
	args = append(args, "-v", sandbox.HostMountPath(rt, workDir)+":/zap/wrk:rw", image,
		"zap.sh", "-cmd", "-autorun", "/zap/wrk/zap.yaml")
	return args
}

const (
	zapContextName = "aegis-dast"
	zapReportFile  = "report.sarif"
)

// zapPlan mirrors the ZAP Automation Framework's YAML schema closely enough
// to marshal a valid plan; Parameters are left as maps rather than typed
// per job kind since each job type has its own parameter set and this
// package only ever needs to construct four fixed job shapes (spider,
// passiveScan-wait, activeScan/openapi, report).
type zapPlan struct {
	Env  zapEnv   `yaml:"env"`
	Jobs []zapJob `yaml:"jobs"`
}

type zapEnv struct {
	Contexts   []zapContext   `yaml:"contexts"`
	Parameters map[string]any `yaml:"parameters"`
}

type zapContext struct {
	Name string   `yaml:"name"`
	URLs []string `yaml:"urls"`
}

type zapJob struct {
	Type       string         `yaml:"type"`
	Parameters map[string]any `yaml:"parameters,omitempty"`
}

// buildZAPPlan renders an Automation Framework plan for mode. Every mode
// ends with a `report` job using the `sarif-json` template so results feed
// the same ParseSARIF ingester every other scanner uses — SARIF is the one
// ZAP report format documented to carry through the Automation Framework's
// report job (the packaged zap-baseline.py/zap-full-scan.py scripts' own
// -J/-r flags only produce plain JSON/HTML, not SARIF).
func buildZAPPlan(mode DASTMode, target, apiDefinition string) ([]byte, error) {
	plan := zapPlan{
		Env: zapEnv{
			Contexts: []zapContext{{Name: zapContextName, URLs: []string{target}}},
			Parameters: map[string]any{
				"failOnError":      true,
				"progressToStdout": true,
			},
		},
	}

	reportJob := zapJob{Type: "report", Parameters: map[string]any{
		"template":    "sarif-json",
		"reportDir":   "/zap/wrk/reports",
		"reportFile":  zapReportFile,
		"reportTitle": "Aegis DAST scan",
	}}
	spiderJob := zapJob{Type: "spider", Parameters: map[string]any{"context": zapContextName, "url": target}}
	passiveWaitJob := zapJob{Type: "passiveScan-wait", Parameters: map[string]any{"maxDuration": 5}}
	activeScanJob := zapJob{Type: "activeScan", Parameters: map[string]any{"context": zapContextName}}

	switch mode {
	case DASTModeBaseline:
		plan.Jobs = []zapJob{spiderJob, passiveWaitJob, reportJob}
	case DASTModeActive:
		plan.Jobs = []zapJob{spiderJob, passiveWaitJob, activeScanJob, reportJob}
	case DASTModeAPI:
		if strings.TrimSpace(apiDefinition) == "" {
			return nil, errors.New("api scan mode requires api_definition (an OpenAPI spec URL or path)")
		}
		openapiJob := zapJob{Type: "openapi", Parameters: map[string]any{
			"apiUrl": apiDefinition, "targetUrl": target, "context": zapContextName,
		}}
		plan.Jobs = []zapJob{openapiJob, activeScanJob, reportJob}
	default:
		return nil, fmt.Errorf("unknown dast mode %q (want baseline, active, or api)", mode)
	}

	return yaml.Marshal(plan)
}

// isDASTTargetAllowed is the P11.7 hard authorization gate: loopback and
// RFC-1918 private addresses are always allowed (the common "scan my
// locally running app" case needs no config); any other host must be
// explicitly declared in allowedTargets. Hostnames are matched as literal
// strings — never DNS-resolved — so a declared target's identity can't be
// silently changed by whatever it happens to resolve to at scan time (ZAP
// does its own resolution inside the container, outside this check's
// visibility).
func isDASTTargetAllowed(rawTarget string, allowedTargets []string) (allowed bool, reason string) {
	u, err := url.Parse(strings.TrimSpace(rawTarget))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false, "target must be a valid http:// or https:// URL"
	}
	host := u.Hostname()
	if isLoopbackOrPrivateHost(host) {
		return true, ""
	}
	for _, entry := range allowedTargets {
		if hostMatchesDASTAllowEntry(host, entry) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("target host %q is not loopback/private and is not declared in security.dast.allowed_targets", host)
}

func isLoopbackOrPrivateHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A non-IP, non-"localhost" hostname is never auto-allowed: DNS
		// resolution is deliberately not performed here (see doc comment).
		return false
	}
	for _, r := range dastPrivateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func hostMatchesDASTAllowEntry(host, entry string) bool {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return false
	}
	if _, cidr, err := net.ParseCIDR(entry); err == nil {
		ip := net.ParseIP(host)
		return ip != nil && cidr.Contains(ip)
	}
	if strings.HasPrefix(entry, ".") {
		return strings.HasSuffix(strings.ToLower(host), strings.ToLower(entry))
	}
	return strings.EqualFold(host, entry)
}

// dastPrivateRanges is the loopback/RFC-1918/link-local table used only for
// DAST's default-allow policy — a small, deliberate duplicate of the
// equivalent table in internal/tool/builtin's SSRF dialer (which uses the
// opposite polarity: it *blocks* these ranges for outbound web_fetch calls)
// rather than a cross-package dependency, matching how internal/sandbox is
// already kept decoupled from internal/config elsewhere in this package.
var dastPrivateRanges = mustParseCIDRs(
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16",
	"::1/128", "fc00::/7", "fe80::/10",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("security: invalid built-in CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}
