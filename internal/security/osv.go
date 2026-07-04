package security

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// osv-scanner's --call-analysis reachability verdict (P11.12) has no SARIF
// equivalent — confirmed against the upstream google/osv-scanner source
// (pkg/models): the `experimental_analysis` map lives only on the native
// JSON report's per-package `groups`, so osv-scanner gets its own parser
// here instead of routing through ParseSARIF like semgrep/trivy do.
//
// Struct shapes below are a minimal read of that JSON schema — only the
// fields Aegis surfaces as a Finding.

type osvScannerOutput struct {
	Results []osvSourceResult `json:"results"`
}

type osvSourceResult struct {
	Source   osvSource     `json:"source"`
	Packages []osvPkgVulns `json:"packages"`
}

type osvSource struct {
	Path string `json:"path"`
}

type osvPkgVulns struct {
	Package         osvPackageInfo     `json:"package"`
	Vulnerabilities []osvVulnerability `json:"vulnerabilities"`
	Groups          []osvGroup         `json:"groups"`
}

type osvPackageInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

type osvVulnerability struct {
	ID       string        `json:"id"`
	Summary  string        `json:"summary"`
	Affected []osvAffected `json:"affected"`
}

type osvAffected struct {
	Ranges []osvRange `json:"ranges"`
}

type osvRange struct {
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Fixed string `json:"fixed,omitempty"`
}

// osvGroup mirrors google/osv-scanner's GroupInfo: IDs alias the same
// underlying vulnerability across databases, and ExperimentalAnalysis
// carries the --call-analysis verdict per ID when the ecosystem supports it.
type osvGroup struct {
	IDs                  []string                   `json:"ids"`
	MaxSeverity          string                     `json:"max_severity"`
	ExperimentalAnalysis map[string]osvAnalysisInfo `json:"experimental_analysis"`
}

type osvAnalysisInfo struct {
	Called      bool `json:"called"`
	Unimportant bool `json:"unimportant"`
}

// parseOSVScanner maps osv-scanner's native JSON report onto Finding, one
// per (package, alias-group) pair.
func parseOSVScanner(data []byte) ([]Finding, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, nil
	}
	var doc osvScannerOutput
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse osv-scanner output: %w", err)
	}
	var out []Finding
	for _, res := range doc.Results {
		for _, pkg := range res.Packages {
			vulnByID := make(map[string]osvVulnerability, len(pkg.Vulnerabilities))
			for _, v := range pkg.Vulnerabilities {
				vulnByID[v.ID] = v
			}
			for _, g := range pkg.Groups {
				if len(g.IDs) == 0 {
					continue
				}
				ruleID := strings.Join(g.IDs, ", ")
				title := ruleID
				remediation := ""
				if v, ok := vulnByID[g.IDs[0]]; ok {
					title = firstNonEmpty(v.Summary, ruleID)
					remediation = fixedVersionRemediation(v.Affected)
				}
				out = append(out, Finding{
					Tool:         "osv-scanner",
					RuleID:       ruleID,
					Severity:     osvSeverity(g.MaxSeverity),
					Title:        title,
					Location:     fmt.Sprintf("%s@%s (%s)", pkg.Package.Name, pkg.Package.Version, firstNonEmpty(res.Source.Path, pkg.Package.Ecosystem)),
					Remediation:  remediation,
					Reachability: groupReachability(g.IDs, g.ExperimentalAnalysis),
				})
			}
		}
	}
	return out, nil
}

// groupReachability resolves a group's Reachability from its per-ID
// analysis: unknown if the ecosystem wasn't analyzed at all (no entries),
// reachable if any aliased ID was actually called, else unreachable.
func groupReachability(ids []string, analysis map[string]osvAnalysisInfo) Reachability {
	if len(analysis) == 0 {
		return ReachabilityUnknown
	}
	sawAnalysis := false
	for _, id := range ids {
		if a, ok := analysis[id]; ok {
			sawAnalysis = true
			if a.Called {
				return ReachabilityReachable
			}
		}
	}
	if !sawAnalysis {
		return ReachabilityUnknown
	}
	return ReachabilityUnreachable
}

// osvSeverity parses osv-scanner's computed numeric max_severity (a CVSS
// base score, e.g. "9.8"). A missing/unparseable score is not treated as
// low risk — a real, database-confirmed vulnerability with no readable
// score defaults to Medium rather than silently reading as Info.
func osvSeverity(maxSeverity string) Severity {
	score, err := strconv.ParseFloat(strings.TrimSpace(maxSeverity), 64)
	if err != nil {
		return SevMedium
	}
	switch {
	case score >= 9:
		return SevCritical
	case score >= 7:
		return SevHigh
	case score >= 4:
		return SevMedium
	case score > 0:
		return SevLow
	default:
		return SevMedium
	}
}

// fixedVersionRemediation collects distinct "fixed" versions across a
// vulnerability's affected ranges into a short upgrade suggestion.
func fixedVersionRemediation(affected []osvAffected) string {
	seen := map[string]bool{}
	var versions []string
	for _, a := range affected {
		for _, rg := range a.Ranges {
			for _, ev := range rg.Events {
				if ev.Fixed == "" || seen[ev.Fixed] {
					continue
				}
				seen[ev.Fixed] = true
				versions = append(versions, ev.Fixed)
			}
		}
	}
	if len(versions) == 0 {
		return ""
	}
	if len(versions) == 1 {
		return "upgrade to " + versions[0]
	}
	return "upgrade to one of: " + strings.Join(versions, ", ")
}
