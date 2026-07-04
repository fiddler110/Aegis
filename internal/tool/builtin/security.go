package builtin

import (
	"context"
	"encoding/json"
	"fmt"

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
	return "Run available security scanners (opengrep, trivy, gitleaks, kubescape, hadolint, osv-scanner, grype) over the workspace and return normalized findings (severity, location, rule, remediation, and — for osv-scanner where supported — reachability: whether the vulnerable code is actually called by this project, not just present as a dependency). opengrep is the default SAST engine (semgrep is selectable via security.tools.semgrep.enabled: true); gosec/bandit/brakeman/njsscan are opt-in language-targeted SAST engines for Go/Python/Ruby/Node.js, off by default. grype's directory scan prefers CVE-matching against a syft-generated SBOM (persisted to .aegis/sbom.cdx.json), falling back to its own cataloger if syft isn't available. Runs the host binary if installed, else falls back to a configured container image (security.tools.<name>.image); reports exactly why a scanner is skipped otherwise instead of a silent skip. Pass \"image\" instead of \"path\" to scan a built container image (trivy image, grype, dockle) by reference — image scanning is host-binary only, since scanner containers run network-isolated and can't pull the target image."
}
func (t *securityScanTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative subdirectory to scan (optional, defaults to the whole workspace)"},"image":{"type":"string","description":"container image reference to scan instead of the workspace (e.g. \"alpine:3.20\"); mutually exclusive with path"},"sbom":{"type":"boolean","description":"generate a CycloneDX SBOM via syft instead of scanning for findings; persists to .aegis/sbom.cdx.json under path. Mutually exclusive with image."}}}`)
}
func (t *securityScanTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path  string `json:"path"`
		Image string `json:"image"`
		SBOM  bool   `json:"sbom"`
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
	if args.SBOM {
		sbom, method, err := security.GenerateSBOM(ctx, dir, t.opts)
		if err != nil {
			return tool.Result{}, err
		}
		security.WriteSBOMArtifact(dir, sbom)
		return tool.Result{Content: fmt.Sprintf("SBOM generated via %s and written to %s (%d bytes)", method, security.SBOMArtifactPath(dir), len(sbom))}, nil
	}
	report := security.RunWithOptions(ctx, dir, security.DefaultScanners(), t.opts)
	return tool.Result{Content: report.Format()}, nil
}
