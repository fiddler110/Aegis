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
	return "Run available security scanners (opengrep, trivy, gitleaks, trufflehog, kubescape, hadolint, osv-scanner, grype) over the workspace and return normalized findings (severity, location, rule, remediation, and — for osv-scanner where supported — reachability: whether the vulnerable code is actually called by this project, not just present as a dependency). opengrep is the default SAST engine (semgrep is selectable via security.tools.semgrep.enabled: true); gosec/bandit/brakeman/njsscan are opt-in language-targeted SAST engines for Go/Python/Ruby/Node.js, off by default, but a call with no \"scanners\" filter auto-detects the project's language and auto-enables the matching one for this run unless you've explicitly configured it yourself (e.g. a pure Rust or Java repo never triggers bandit). hadolint, kubescape and brakeman are likewise skipped automatically, with a reason, when the workspace has no Dockerfile, Kubernetes manifest, or Rails application for them to analyze (brakeman needs a Rails app specifically, so a non-Rails Ruby project skips rather than running). Pass \"scanners\" to instead run only specific scanners or categories (e.g. [\"trufflehog\"], [\"secrets\"]) — force-enabled for this run regardless of config or relevance. grype's directory scan prefers CVE-matching against a syft-generated SBOM (persisted to .aegis/sbom.cdx.json), falling back to its own cataloger if syft isn't available. Findings are deduped across overlapping tools (same CVE/rule at the same location collapses to one, tagged with every tool that also flagged it) and tagged with a best-effort OWASP ASVS chapter when confidently derivable from a CWE. A path-scoped .aegis/security-baseline.yaml lets an operator suppress a specific accepted-risk finding with a mandatory expiry date — suppressed findings are reported separately, never silently dropped, and an expired or malformed baseline entry is flagged rather than honored. Runs the host binary if installed, else falls back to a configured container image (security.tools.<name>.image); reports exactly why a scanner is skipped otherwise instead of a silent skip. Pass \"image\" instead of \"path\" to scan a built container image (trivy image, grype, dockle) by reference — image scanning is host-binary only, since scanner containers run network-isolated and can't pull the target image. Every findings report (path or image) is also persisted under .aegis/security/."
}
func (t *securityScanTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative subdirectory to scan (optional, defaults to the whole workspace)"},"image":{"type":"string","description":"container image reference to scan instead of the workspace (e.g. \"alpine:3.20\"); mutually exclusive with path"},"sbom":{"type":"boolean","description":"generate a CycloneDX SBOM via syft instead of scanning for findings; persists to .aegis/sbom.cdx.json under path. Mutually exclusive with image."},"scanners":{"type":"array","items":{"type":"string"},"description":"run only these scanner names and/or category aliases (e.g. \"trufflehog\", \"secrets\", \"sast\") instead of every enabled scanner; force-enabled for this run regardless of config. Omit to run every enabled scanner plus auto-detected language-specific engines. Mutually exclusive with image/sbom."}}}`)
}
func (t *securityScanTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path     string   `json:"path"`
		Image    string   `json:"image"`
		SBOM     bool     `json:"sbom"`
		Scanners []string `json:"scanners"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	root := effectiveRoot(ctx, t.root)
	if args.Image != "" {
		report := security.ScanImage(ctx, args.Image, security.DefaultImageScanners(), t.opts)
		security.WriteReportArtifact(root, "image", report)
		return tool.Result{Content: report.Format()}, nil
	}
	dir := root
	if args.Path != "" {
		resolved, err := resolvePath(root, args.Path)
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
	scanners := security.DefaultScanners()
	opts := t.opts
	if len(args.Scanners) > 0 {
		var err error
		scanners, opts, err = security.SelectScanners(scanners, opts, args.Scanners)
		if err != nil {
			return tool.Result{}, err
		}
	} else {
		opts = security.AutoEnableLanguageScanners(dir, opts)
	}
	report := security.RunWithOptions(ctx, dir, scanners, opts)
	security.WriteReportArtifact(dir, "scan", report)
	return tool.Result{Content: report.Format()}, nil
}
