package builtin

import (
	"context"
	"encoding/json"

	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tool"
)

// dastScanTool runs OWASP ZAP against a running target (P11.7). Capability
// is CapExecute — the highest-friction default in this codebase — not
// CapNetwork: an active scan sends real attack payloads and spins up a
// container, closer in risk to shell execution than to a single web_fetch
// call. That gets it the normal plan-mode block / build-mode approval
// prompt, but the target-authorization check inside security.RunDAST is the
// real, mode-independent gate (see its doc comment) — approval alone would
// not be enough for a tool that can otherwise be pointed at an arbitrary host.
type dastScanTool struct {
	root           string           // workspace root, used only to anchor the persisted .aegis/security/dast.json report
	opts           security.Options // resolves the zap scanner (host/container/image), same as security_scan
	allowedTargets []string
	allowActive    bool
}

func (t *dastScanTool) Name() string                { return "dast_scan" }
func (t *dastScanTool) Capability() tool.Capability { return tool.CapExecute }
func (t *dastScanTool) Description() string {
	return "Dynamic Application Security Testing (DAST) via OWASP ZAP: crawls (and, in active/api mode, actively attacks) a *running* application to find real, exploitable vulnerabilities — XSS, injection, auth issues, missing security headers. Runs containerized (security.tools.zap.image, digest-pinned; no host-binary path). The target must be loopback/private (default-allowed) or explicitly declared in security.dast.allowed_targets — anything else is rejected before ZAP ever runs, regardless of permission mode. \"baseline\" mode (default) is passive-only (spider + passive scan, no attack traffic). \"active\" and \"api\" modes send real attack payloads and are disabled unless security.dast.allow_active: true is set, in addition to this call needing normal execute-tool approval. \"api\" mode requires api_definition (an OpenAPI spec URL)."
}

// ShortDescription is the deferred-tools advertisement (P62.6). It says
// "running application" because that is the distinction the model has to make
// against security_scan, which reads source; the authorization gate and mode
// rules stay in Description, where they are enforced regardless of what the
// model read.
func (t *dastScanTool) ShortDescription() string {
	return "Attack a running application over HTTP with OWASP ZAP (passive baseline by default) to find exploitable vulnerabilities."
}
func (t *dastScanTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"target":{"type":"string","description":"http(s) URL of the running application to scan"},"mode":{"type":"string","enum":["baseline","active","api"],"description":"baseline (default): passive spider + passive scan only. active: + real attack payloads. api: OpenAPI-defined endpoints + active scan."},"api_definition":{"type":"string","description":"OpenAPI spec URL or path; required when mode is \"api\""}},"required":["target"]}`)
}
func (t *dastScanTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Target        string `json:"target"`
		Mode          string `json:"mode"`
		APIDefinition string `json:"api_definition"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}

	report, err := security.RunDAST(ctx, security.DASTOptions{
		Target:         args.Target,
		Mode:           security.DASTMode(args.Mode),
		APIDefinition:  args.APIDefinition,
		AllowedTargets: t.allowedTargets,
		AllowActive:    t.allowActive,
	}, t.opts)
	if err != nil {
		return tool.Result{}, err
	}
	security.WriteReportArtifact(effectiveRoot(ctx, t.root), "dast", report)
	return tool.Result{Content: report.Format()}, nil
}
