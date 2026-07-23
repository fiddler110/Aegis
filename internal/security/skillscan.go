package security

import (
	"context"
	"fmt"
)

// ScanBundleWarnings runs the same filesystem scanners `aegis security scan`
// drives (DefaultScanners) over dir and returns one warning line per
// HIGH/CRITICAL finding — the shape internal/skills folds into a bundled
// skill's <skill_assets> block so a compromised .aegis/skills/ bundle's
// scripts can't reach the model unflagged (P44.1).
//
// It is deliberately silent about everything else. When the multiscanner image
// hasn't been built and no host scanner binary is installed, every scanner
// resolves to MethodNone, RunWithOptions returns no findings, and this returns
// nil — the no-op degradation that keeps a session which never ran `aegis
// security build-image` paying nothing, mirroring verifyMultiscannerImage's
// fallback. Sub-HIGH findings are dropped too: the point is to flag content
// dangerous enough to warrant a hard warning, not to surface a full report in
// a system-prompt block.
func ScanBundleWarnings(ctx context.Context, dir string, opts Options) []string {
	return bundleWarnings(RunWithOptions(ctx, dir, DefaultScanners(), opts))
}

// bundleWarnings extracts the HIGH/CRITICAL findings from a scan report as
// warning lines. Split out from ScanBundleWarnings so the severity filter and
// formatting are testable without standing up a scanner.
func bundleWarnings(report Report) []string {
	var warnings []string
	for _, f := range report.Findings {
		if f.Severity != SevCritical && f.Severity != SevHigh {
			continue
		}
		loc := f.Location
		if loc == "" {
			loc = "unknown location"
		}
		warnings = append(warnings, fmt.Sprintf("%s flagged %q (%s) at %s", f.Tool, f.Title, f.Severity, loc))
	}
	return warnings
}
