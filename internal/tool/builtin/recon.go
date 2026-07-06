package builtin

import (
	"context"
	"encoding/json"

	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tool"
)

// reconScanTool runs nmap + nuclei against a network-target host list
// (P13.5) for attack-surface mapping — the network-recon analogue of
// dastScanTool. Capability is CapExecute for the same reason as dast_scan:
// this reaches out to real network hosts, closer in risk to shell execution
// than a single web_fetch call. The real, mode-independent gate is the
// target-authorization check inside security.RunRecon (shared with
// dast_scan, see internal/security/target.go) — approval alone would not be
// enough for a tool that can otherwise be pointed at an arbitrary host.
type reconScanTool struct {
	opts           security.Options // resolves nmap/nuclei (host binary only for v1), same as security_scan/dast_scan
	allowedTargets []string
	allowActive    bool
}

func (t *reconScanTool) Name() string                { return "recon_scan" }
func (t *reconScanTool) Capability() tool.Capability { return tool.CapExecute }
func (t *reconScanTool) Description() string {
	return "Network/host reconnaissance for attack-surface mapping: nmap discovers live hosts, open ports, and service/version banners across a target host list or CIDR range; nuclei then matches ProjectDiscovery's community templates (CVEs, misconfigurations, exposed panels) against what's alive. Findings are normalized and aggregated the same way security_scan aggregates its scanners. The target(s) must be loopback/private (default-allowed) or explicitly declared in security.dast.allowed_targets — the same shared gate dast_scan uses — anything else is rejected before either scanner runs, regardless of permission mode. Baseline mode (default) runs nmap's top-100-port version scan and nuclei with dos/fuzz/intrusive templates excluded; set security.dast.allow_active: true to unlock nmap's OS detection/full-port-range/default-script mode and nuclei's full template set — the same flag dast_scan's active mode uses. nuclei additionally requires security.tools.nuclei.templates_version (a pinned nuclei-templates release tag). Host-binary only for both scanners in this version — no container fallback (a network-isolated scanner container can't reach LAN targets)."
}
func (t *reconScanTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"targets":{"type":"array","items":{"type":"string"},"description":"bare hosts, IPs, or CIDR ranges to scan (e.g. \"192.168.1.0/24\", \"10.0.0.5\", \"db.lan\") — no http(s):// scheme"}},"required":["targets"]}`)
}
func (t *reconScanTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Targets []string `json:"targets"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	report, err := security.RunRecon(ctx, security.ReconOptions{
		Targets:        args.Targets,
		AllowedTargets: t.allowedTargets,
		AllowActive:    t.allowActive,
	}, t.opts)
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{Content: report.Format()}, nil
}
