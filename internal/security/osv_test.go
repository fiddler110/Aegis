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
	findings, err := parseOSVScanner([]byte(osvFixture))
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
	findings, err := parseOSVScanner([]byte("  \n"))
	if err != nil || findings != nil {
		t.Errorf("empty osv-scanner output should yield no findings, got %v %v", findings, err)
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
