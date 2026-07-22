package security

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// osv-scanner's exit codes, as measured against 2.4.0 — the version pinned in
// the multiscanner Containerfile — on both the host and container paths
// (P34.12):
//
//	0/1  package sources found (1 = vulnerabilities among them), JSON on stdout
//	127  a real failure: unresolvable path, unknown flag
//	128  "No package sources found", zero bytes on stdout
//
// 128 is the one that needs interpreting. It is overwhelmingly an accurate
// refusal — "this tree declares no dependencies" — which runJSON (which
// tolerates a non-zero exit only when output was produced) otherwise turns
// into an `osv-scanner: error: exit status 128` row on any C/C++, shell or
// docs repo. That's P34.6's shape: a correct answer rendered as a broken tool.
//
// But 128 covers a second case the "no dependencies here" reading gets wrong:
// osv-scanner also reports it when candidate manifests *were* found and every
// one of them failed to parse, logging "Error during extraction:" per file
// first. Mapping that to zero findings would silently drop SCA coverage on a
// repo whose lockfile is corrupt — the precise failure mode a manifest-name
// allowlist was rejected for risking. So the marker is what separates them.
const (
	osvNoPackageSourcesExit = 128
	osvExtractionErrMarker  = "Error during extraction:"
)

// interpretOSVError maps an osv-scanner run that produced no stdout onto
// either a benign "nothing to scan" (nil — report zero findings) or an error
// worth surfacing. Any exit code other than 128 is passed through untouched:
// 127 means osv-scanner genuinely failed, and a container runtime's own
// failures land on 125/126/127 rather than 128.
func interpretOSVError(err error) error {
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != osvNoPackageSourcesExit {
		return err
	}
	if detail := osvExtractionFailures(string(ee.Stderr)); detail != "" {
		return fmt.Errorf("found no parsable dependency manifest — %s", detail)
	}
	return nil
}

// osvExtractionFailures pulls osv-scanner's per-file extraction errors out of
// stderr, so a corrupt lockfile is reported as the parse failure it is rather
// than as a bare exit code.
func osvExtractionFailures(stderr string) string {
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, osvExtractionErrMarker) {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, osvExtractionErrMarker)))
		}
	}
	return strings.Join(out, "; ")
}

// osv-scanner's --call-analysis reachability verdict (P11.12) has no SARIF
// equivalent — confirmed against the upstream google/osv-scanner source
// (pkg/models): the `experimental_analysis` map lives only on the native
// JSON report's per-package `groups`, so osv-scanner gets its own parser
// here instead of routing through ParseSARIF like opengrep/trivy do.
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
//
// Aliases is the superset of IDs: osv-scanner routinely reports a group whose
// IDs are Go-vulndb/GHSA identifiers only and files the CVE under Aliases
// instead (P34.8), so it's Aliases — not IDs — that reliably carries the one
// identifier other scanners key on.
type osvGroup struct {
	IDs                  []string                   `json:"ids"`
	Aliases              []string                   `json:"aliases"`
	MaxSeverity          string                     `json:"max_severity"`
	ExperimentalAnalysis map[string]osvAnalysisInfo `json:"experimental_analysis"`
}

type osvAnalysisInfo struct {
	Called      bool `json:"called"`
	Unimportant bool `json:"unimportant"`
}

// osvRuleID renders a group's identifiers with any CVE alias guaranteed to be
// among them.
//
// Dedup keys SCA findings on an embedded CVE (normalizeRuleID), which only
// works if the CVE is actually in the rendered rule ID. osv-scanner's group
// IDs frequently aren't: a vulnerability trivy reports as "CVE-2021-3121"
// arrives here as ids ["GO-2021-0053", "GHSA-c3h9-896r-86jm"] with the CVE
// only under aliases, so the two copies of one finding never merged (P34.8).
// Only CVE aliases are appended — normalizeRuleID looks for nothing else, and
// osv-scanner's alias sets also carry distro-specific IDs (BIT-*, etc.) that
// would be noise in a rule ID a user reads.
func osvRuleID(g osvGroup) string {
	ids := append([]string{}, g.IDs...)
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		seen[strings.ToUpper(id)] = true
	}
	for _, alias := range g.Aliases {
		if !cveRe.MatchString(alias) {
			continue
		}
		if key := strings.ToUpper(alias); !seen[key] {
			seen[key] = true
			ids = append(ids, alias)
		}
	}
	return strings.Join(ids, ", ")
}

// osvRelativeSource re-renders osv-scanner's source path relative to the scan
// root, the way every SARIF-based scanner already reports locations.
//
// osv-scanner resolves its target to an absolute host path (host method) or
// to the container mount point (/src, container method), while trivy/grype
// emit a path relative to the scanned tree. normalizeLocation can't reconcile
// the two — it has no idea what the root was — so "CVE-x at go.mod" and
// "CVE-x at /abs/path/go.mod" keyed differently and no osv-scanner finding
// ever merged with anything (P34.8). Root is trimmed here, at the one place
// that knows it.
func osvRelativeSource(root, path string) string {
	if root == "" || path == "" {
		return path
	}
	r := strings.TrimSuffix(toForwardSlash(root), "/")
	p := toForwardSlash(path)
	if r == "" || len(p) <= len(r) {
		return p
	}
	// EqualFold: a Windows host root ("D:\dev\repo") and osv-scanner's
	// rendering of it ("D:/dev/repo") can differ in case.
	if strings.EqualFold(p[:len(r)], r) && p[len(r)] == '/' {
		return p[len(r)+1:]
	}
	return p
}

// parseOSVScanner maps osv-scanner's native JSON report onto Finding, one
// per (package, alias-group) pair. Root is the directory osv-scanner was
// pointed at, trimmed from each finding's location (see osvRelativeSource);
// pass "" to leave osv-scanner's own paths untouched.
func parseOSVScanner(data []byte, root string) ([]Finding, error) {
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
				ruleID := osvRuleID(g)
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
					Location:     fmt.Sprintf("%s@%s (%s)", pkg.Package.Name, pkg.Package.Version, firstNonEmpty(osvRelativeSource(root, res.Source.Path), pkg.Package.Ecosystem)),
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
