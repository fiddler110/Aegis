package security

import (
	"strings"
	"testing"
)

// osvFixture is a realistic 3-package osv-scanner --format json report:
// one Go package with a called (reachable) vuln, one Go package with an
// uncalled (unreachable) vuln, and one npm package with no call-analysis
// entry at all (unsupported ecosystem — must read as unknown, not guessed).
const osvFixture = `{
  "results": [
    {
      "source": {"path": "go.mod"},
      "packages": [
        {
          "package": {"name": "golang.org/x/net", "version": "0.1.0", "ecosystem": "Go"},
          "vulnerabilities": [
            {"id": "GO-2023-1111", "summary": "HTTP/2 rapid reset", "affected": [
              {"ranges": [{"events": [{"introduced": "0"}, {"fixed": "0.2.0"}]}]}
            ]}
          ],
          "groups": [
            {"ids": ["GO-2023-1111"], "max_severity": "7.5",
             "experimental_analysis": {"GO-2023-1111": {"called": true}}}
          ]
        },
        {
          "package": {"name": "golang.org/x/text", "version": "0.3.0", "ecosystem": "Go"},
          "vulnerabilities": [
            {"id": "GO-2022-2222", "summary": "unreachable panic in obscure parser", "affected": [
              {"ranges": [{"events": [{"introduced": "0"}, {"fixed": "0.3.8"}]}]}
            ]}
          ],
          "groups": [
            {"ids": ["GO-2022-2222"], "max_severity": "9.1",
             "experimental_analysis": {"GO-2022-2222": {"called": false}}}
          ]
        },
        {
          "package": {"name": "lodash", "version": "4.17.15", "ecosystem": "npm"},
          "vulnerabilities": [
            {"id": "GHSA-xxxx-yyyy-zzzz", "summary": "prototype pollution", "affected": [
              {"ranges": [{"events": [{"introduced": "0"}, {"fixed": "4.17.21"}]}]}
            ]}
          ],
          "groups": [
            {"ids": ["GHSA-xxxx-yyyy-zzzz"], "max_severity": "7.4"}
          ]
        }
      ]
    }
  ]
}`

func TestParseOSVScannerReachability(t *testing.T) {
	findings, err := parseOSVScanner([]byte(osvFixture), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 3 {
		t.Fatalf("got %d findings, want 3", len(findings))
	}

	byRule := map[string]Finding{}
	for _, f := range findings {
		byRule[f.RuleID] = f
	}

	reachable := byRule["GO-2023-1111"]
	if reachable.Reachability != ReachabilityReachable {
		t.Errorf("expected called=true to map to Reachable, got %q", reachable.Reachability)
	}
	if reachable.Severity != SevHigh {
		t.Errorf("severity for score 7.5 = %s, want HIGH", reachable.Severity)
	}
	if reachable.Remediation != "upgrade to 0.2.0" {
		t.Errorf("remediation = %q", reachable.Remediation)
	}

	unreachable := byRule["GO-2022-2222"]
	if unreachable.Reachability != ReachabilityUnreachable {
		t.Errorf("expected called=false to map to Unreachable, got %q", unreachable.Reachability)
	}
	if unreachable.Severity != SevCritical {
		t.Errorf("severity for score 9.1 = %s, want CRITICAL", unreachable.Severity)
	}

	unknown := byRule["GHSA-xxxx-yyyy-zzzz"]
	if unknown.Reachability != ReachabilityUnknown {
		t.Errorf("expected no experimental_analysis to map to Unknown, got %q", unknown.Reachability)
	}
}

func TestParseOSVScannerEmpty(t *testing.T) {
	findings, err := parseOSVScanner([]byte("  \n"), "")
	if err != nil || findings != nil {
		t.Errorf("empty osv-scanner output should yield no findings, got %v %v", findings, err)
	}
}

// osvRealShapeFixture is the shape osv-scanner actually emits, which the
// older fixtures above do not: the scan target resolved to an absolute path,
// and a group whose ids carry no CVE at all — the CVE that every other
// scanner keys on is filed under the group's aliases, next to distro IDs that
// are noise. Recorded from osv-scanner 2.4.0 (P34.8).
const osvRealShapeFixture = `{
  "results": [
    {
      "source": {"path": "/scan/root/go.mod"},
      "packages": [
        {
          "package": {"name": "github.com/gogo/protobuf", "version": "1.3.1", "ecosystem": "Go"},
          "vulnerabilities": [
            {"id": "GO-2021-0053", "summary": "panic in ParseUvarint",
             "aliases": ["BIT-protobuf-2021-3121", "CVE-2021-3121", "GHSA-c3h9-896r-86jm"],
             "affected": [{"ranges": [{"events": [{"introduced": "0"}, {"fixed": "1.3.2"}]}]}]}
          ],
          "groups": [
            {"ids": ["GO-2021-0053", "GHSA-c3h9-896r-86jm"],
             "aliases": ["BIT-protobuf-2021-3121", "CVE-2021-3121", "GHSA-c3h9-896r-86jm", "GO-2021-0053"],
             "max_severity": "8.6"}
          ]
        }
      ]
    }
  ]
}`

// TestOSVFindingDedupsAgainstSARIFCopy is the P34.8 regression proof, and it
// asserts the invariant that actually matters rather than either half of the
// mechanism: one vulnerability, reported by osv-scanner and by a SARIF
// scanner over the same file, must collapse to a single finding.
//
// It failed both ways before the fix — osv-scanner rendered the location as
// an absolute path where trivy rendered it relative, and rendered a rule ID
// with no CVE in it — so on a tree where the two tools genuinely overlapped,
// dedup merged nothing at all and every shared CVE was reported twice under
// two different names.
func TestOSVFindingDedupsAgainstSARIFCopy(t *testing.T) {
	osvFindings, err := parseOSVScanner([]byte(osvRealShapeFixture), "/scan/root")
	if err != nil {
		t.Fatal(err)
	}
	if len(osvFindings) != 1 {
		t.Fatalf("expected 1 osv-scanner finding, got %d", len(osvFindings))
	}

	// The same vulnerability as trivy reports it: bare CVE, path relative to
	// the scanned tree, ":line" suffix.
	trivyCopy := Finding{
		Tool:     "trivy",
		RuleID:   "CVE-2021-3121",
		Location: "go.mod:1",
		Severity: SevHigh,
	}

	merged := DedupFindings(append([]Finding{trivyCopy}, osvFindings...))
	if len(merged) != 1 {
		t.Fatalf("one CVE seen by two scanners must dedup to 1 finding, got %d:\n  trivy: %s\n  osv:   %s (rule %q)",
			len(merged), dedupKey(trivyCopy, 0), dedupKey(osvFindings[0], 1), osvFindings[0].RuleID)
	}
	if got := merged[0].SeenBy; len(got) != 1 {
		t.Errorf("merged finding should record the other scanner on SeenBy, got %v", got)
	}
}

func TestOSVRuleIDSurfacesCVEAlias(t *testing.T) {
	got := osvRuleID(osvGroup{
		IDs:     []string{"GO-2021-0053", "GHSA-c3h9-896r-86jm"},
		Aliases: []string{"BIT-protobuf-2021-3121", "CVE-2021-3121", "GHSA-c3h9-896r-86jm", "GO-2021-0053"},
	})
	// The CVE is appended (dedup keys on it); the group's own IDs keep their
	// order and lead; non-CVE aliases and duplicates stay out.
	want := "GO-2021-0053, GHSA-c3h9-896r-86jm, CVE-2021-3121"
	if got != want {
		t.Errorf("osvRuleID = %q, want %q", got, want)
	}
	if normalizeRuleID(got) != "CVE-2021-3121" {
		t.Errorf("rule ID %q must normalize to the CVE for dedup, got %q", got, normalizeRuleID(got))
	}
}

func TestOSVRelativeSource(t *testing.T) {
	cases := []struct{ name, root, path, want string }{
		{"container mount point", "/src", "/src/go.mod", "go.mod"},
		{"host absolute root", "/home/u/repo", "/home/u/repo/sub/go.mod", "sub/go.mod"},
		{"windows root, mixed separators", `D:\dev\repo`, "D:/dev/repo/go.mod", "go.mod"},
		{"trailing slash on root", "/src/", "/src/go.mod", "go.mod"},
		{"no root known", "", "/src/go.mod", "/src/go.mod"},
		// A path outside the scan root is left alone rather than mangled: it
		// really is a different file, and must not dedup against one inside.
		{"unrelated path", "/src", "/other/go.mod", "/other/go.mod"},
		// Guards against a prefix match that isn't a directory boundary.
		{"sibling with shared prefix", "/src", "/srcfoo/go.mod", "/srcfoo/go.mod"},
		{"path equal to root", "/src", "/src", "/src"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := osvRelativeSource(c.root, c.path); got != c.want {
				t.Errorf("osvRelativeSource(%q, %q) = %q, want %q", c.root, c.path, got, c.want)
			}
		})
	}
}

func TestOSVSeverityFallsBackToMediumOnUnparseable(t *testing.T) {
	if got := osvSeverity(""); got != SevMedium {
		t.Errorf("empty score = %s, want MEDIUM (never silently Info)", got)
	}
	if got := osvSeverity("not-a-number"); got != SevMedium {
		t.Errorf("garbage score = %s, want MEDIUM", got)
	}
}

func TestFixedVersionRemediationDedupsAndJoins(t *testing.T) {
	affected := []osvAffected{
		{Ranges: []osvRange{{Events: []osvEvent{{Fixed: "1.0.0"}, {Fixed: "1.0.0"}}}}},
		{Ranges: []osvRange{{Events: []osvEvent{{Fixed: "2.0.0"}}}}},
	}
	got := fixedVersionRemediation(affected)
	if got != "upgrade to one of: 1.0.0, 2.0.0" {
		t.Errorf("got %q", got)
	}
}

func TestFixedVersionRemediationEmpty(t *testing.T) {
	if got := fixedVersionRemediation(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestSortFindingsTiebreaksOnReachability is the P11.12 regression: within
// the same severity, a confirmed-reachable finding must surface above an
// unanalyzed one, which in turn surfaces above a confirmed-unreachable one.
func TestSortFindingsTiebreaksOnReachability(t *testing.T) {
	rep := Report{Findings: []Finding{
		{Tool: "a", Severity: SevHigh, Reachability: ReachabilityUnreachable},
		{Tool: "b", Severity: SevHigh, Reachability: ReachabilityUnknown},
		{Tool: "c", Severity: SevHigh, Reachability: ReachabilityReachable},
	}}
	rep.sortFindings()
	got := []string{rep.Findings[0].Tool, rep.Findings[1].Tool, rep.Findings[2].Tool}
	want := []string{"c", "b", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sort order = %v, want %v", got, want)
		}
	}
}

func TestFormatIncludesReachabilityTag(t *testing.T) {
	rep := Report{Findings: []Finding{
		{Tool: "osv-scanner", Severity: SevHigh, Title: "x", Reachability: ReachabilityReachable},
	}}
	out := rep.Format()
	if !strings.Contains(out, "[reachable: called by this project]") {
		t.Errorf("Format() missing reachable tag: %q", out)
	}
}
