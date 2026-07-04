package builtin

import (
	"context"
	"encoding/json"

	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tool"
)

type securityScanTool struct {
	root string
	opts security.Options
}

func (t *securityScanTool) Name() string                { return "security_scan" }
func (t *securityScanTool) Capability() tool.Capability { return tool.CapExecute }
func (t *securityScanTool) Description() string {
	return "Run available security scanners (semgrep, trivy, gitleaks, kubescape, hadolint) over the workspace and return normalized findings (severity, location, rule, remediation). Runs the host binary if installed, else falls back to a configured container image (security.tools.<name>.image); reports exactly why a scanner is skipped otherwise instead of a silent skip. Pass \"image\" instead of \"path\" to scan a built container image (trivy image, grype, dockle) by reference — image scanning is host-binary only, since scanner containers run network-isolated and can't pull the target image."
}
func (t *securityScanTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative subdirectory to scan (optional, defaults to the whole workspace)"},"image":{"type":"string","description":"container image reference to scan instead of the workspace (e.g. \"alpine:3.20\"); mutually exclusive with path"}}}`)
}
func (t *securityScanTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path  string `json:"path"`
		Image string `json:"image"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.Image != "" {
		report := security.ScanImage(ctx, args.Image, security.DefaultImageScanners(), t.opts)
		return tool.Result{Content: report.Format()}, nil
	}
	dir := t.root
	if args.Path != "" {
		resolved, err := resolvePath(t.root, args.Path)
		if err != nil {
			return tool.Result{}, err
		}
		dir = resolved
	}
	report := security.RunWithOptions(ctx, dir, security.DefaultScanners(), t.opts)
	return tool.Result{Content: report.Format()}, nil
}
