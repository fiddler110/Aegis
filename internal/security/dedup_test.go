package security

import "testing"

func TestNormalizeRuleIDExtractsCVE(t *testing.T) {
	got := normalizeRuleID("CVE-2023-1234, GHSA-xxxx-yyyy-zzzz")
	if got != "CVE-2023-1234" {
		t.Fatalf("got %q, want CVE-2023-1234", got)
	}
}

func TestNormalizeRuleIDFallsBackToLowercase(t *testing.T) {
	got := normalizeRuleID("  Opengrep.Rule.ID  ")
	if got != "opengrep.rule.id" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeLocationExtractsParentheticalPath(t *testing.T) {
	got := normalizeLocation("lodash@4.17.15 (package-lock.json)")
	if got != "package-lock.json" {
		t.Fatalf("got %q, want package-lock.json", got)
	}
}

func TestNormalizeLocationStripsLineNumber(t *testing.T) {
	got := normalizeLocation("src/app.go:42")
	if got != "src/app.go" {
		t.Fatalf("got %q, want src/app.go", got)
	}
}

func TestNormalizeLocationNormalizesSlashesAndCase(t *testing.T) {
	got := normalizeLocation(`SRC\App.go`)
	if got != "src/app.go" {
		t.Fatalf("got %q", got)
	}
}

func TestDedupFindingsMergesAcrossTools(t *testing.T) {
	findings := []Finding{
		{Tool: "osv-scanner", RuleID: "CVE-2023-1234", Severity: SevHigh, Location: "lodash@4.17.15 (package-lock.json)"},
		{Tool: "trivy", RuleID: "CVE-2023-1234", Severity: SevCritical, Location: "package-lock.json"},
		{Tool: "grype", RuleID: "CVE-2023-1234", Severity: SevHigh, Location: "package-lock.json"},
	}
	out := DedupFindings(findings)
	if len(out) != 1 {
		t.Fatalf("expected 1 deduped finding, got %d", len(out))
	}
	if out[0].Tool != "trivy" || out[0].Severity != SevCritical {
		t.Fatalf("expected trivy/critical (highest severity) kept, got %+v", out[0])
	}
	if len(out[0].SeenBy) != 2 || out[0].SeenBy[0] != "grype" || out[0].SeenBy[1] != "osv-scanner" {
		t.Fatalf("expected SeenBy [grype osv-scanner], got %v", out[0].SeenBy)
	}
}

func TestDedupFindingsPreservesDistinctFindings(t *testing.T) {
	findings := []Finding{
		{Tool: "opengrep", RuleID: "rule-a", Severity: SevMedium, Location: "a.go:1"},
		{Tool: "opengrep", RuleID: "rule-b", Severity: SevMedium, Location: "b.go:1"},
	}
	out := DedupFindings(findings)
	if len(out) != 2 {
		t.Fatalf("expected 2 distinct findings preserved, got %d", len(out))
	}
}

func TestDedupFindingsNeverMergesEmptyKeyFindings(t *testing.T) {
	findings := []Finding{
		{Tool: "toolA", RuleID: "", Severity: SevLow, Location: ""},
		{Tool: "toolB", RuleID: "", Severity: SevLow, Location: ""},
	}
	out := DedupFindings(findings)
	if len(out) != 2 {
		t.Fatalf("findings missing rule/location must never merge, got %d", len(out))
	}
}

func TestDedupFindingsReachabilityTiebreak(t *testing.T) {
	findings := []Finding{
		{Tool: "osv-scanner", RuleID: "CVE-2024-0001", Severity: SevHigh, Location: "pkg@1 (go.sum)", Reachability: ReachabilityUnreachable},
		{Tool: "trivy", RuleID: "CVE-2024-0001", Severity: SevHigh, Location: "go.sum", Reachability: ReachabilityReachable},
	}
	out := DedupFindings(findings)
	if len(out) != 1 || out[0].Reachability != ReachabilityReachable {
		t.Fatalf("expected the reachable copy to win the tiebreak, got %+v", out)
	}
}
