package builtin

import (
	"context"
	"encoding/json"

	"github.com/scottymacleod/aegis/internal/security"
	"github.com/scottymacleod/aegis/internal/tool"
)

type securityScanTool struct{ root string }

func (t *securityScanTool) Name() string                { return "security_scan" }
func (t *securityScanTool) Capability() tool.Capability { return tool.CapExecute }
func (t *securityScanTool) Description() string {
	return "Run available security scanners (semgrep, trivy, gitleaks) over the workspace and return normalized findings (severity, location, rule, remediation). Scanners that are not installed are reported as skipped."
}
func (t *securityScanTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative subdirectory to scan (optional, defaults to the whole workspace)"}}}`)
}
func (t *securityScanTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	dir := t.root
	if args.Path != "" {
		resolved, err := resolvePath(t.root, args.Path)
		if err != nil {
			return tool.Result{}, err
		}
		dir = resolved
	}
	report := security.RunAll(ctx, dir, security.DefaultScanners())
	return tool.Result{Content: report.Format()}, nil
}
