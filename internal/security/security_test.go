package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

func TestParseSemgrepSARIF(t *testing.T) {
	data := []byte(`{"runs":[{
		"tool":{"driver":{"name":"semgrep","rules":[
			{"id":"go.lang.security.audit.sqli","shortDescription":{"text":"possible SQL injection"}},
			{"id":"generic.secrets.key","shortDescription":{"text":"hardcoded key"}}
		]}},
		"results":[
			{"ruleId":"go.lang.security.audit.sqli","level":"error","message":{"text":"possible SQL injection"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"db/query.go"},"region":{"startLine":42}}}]},
			{"ruleId":"generic.secrets.key","level":"warning","message":{"text":"hardcoded key"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"cfg.go"},"region":{"startLine":7}}}]}
		]
	}]}`)
	findings, err := ParseSARIF(data, "semgrep")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	if findings[0].Severity != SevHigh {
		t.Errorf("error level should map to HIGH, got %s", findings[0].Severity)
	}
	if findings[0].Location != "db/query.go:42" {
		t.Errorf("location = %q", findings[0].Location)
	}
	if findings[1].Severity != SevMedium {
		t.Errorf("warning level should map to MEDIUM, got %s", findings[1].Severity)
	}
	if findings[0].Tool != "semgrep" {
		t.Errorf("tool = %q, want semgrep", findings[0].Tool)
	}
}

func TestParseTrivySARIF(t *testing.T) {
	data := []byte(`{"runs":[{
		"tool":{"driver":{"name":"trivy","rules":[
			{"id":"CVE-2024-1234","shortDescription":{"text":"RCE in foo"},"help":{"text":"upgrade foo to 1.0.1"},
			 "properties":{"tags":["CRITICAL"]}},
			{"id":"DS002","shortDescription":{"text":"root user"},"help":{"text":"add USER"},
			 "properties":{"tags":["HIGH"]}}
		]}},
		"results":[
			{"ruleId":"CVE-2024-1234","level":"error","message":{"text":"bad"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"go.sum"}}}]},
			{"ruleId":"DS002","level":"error","message":{"text":"runs as root"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"Dockerfile"}}}]}
		]
	}]}`)
	findings, err := ParseSARIF(data, "trivy")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	var vuln, misc *Finding
	for i := range findings {
		switch findings[i].RuleID {
		case "CVE-2024-1234":
			vuln = &findings[i]
		case "DS002":
			misc = &findings[i]
		}
	}
	if vuln == nil || vuln.Severity != SevCritical || vuln.Remediation != "upgrade foo to 1.0.1" {
		t.Errorf("vuln finding wrong: %+v", vuln)
	}
	if misc == nil || misc.Remediation != "add USER" {
		t.Errorf("misconfig finding wrong: %+v", misc)
	}
}

func TestParseGitleaks(t *testing.T) {
	data := []byte(`[{"RuleID":"aws-key","Description":"AWS Access Key","File":"app\\secrets.go","StartLine":12}]`)
	findings, err := parseGitleaks(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	f := findings[0]
	if f.Severity != SevHigh {
		t.Errorf("severity = %s, want HIGH", f.Severity)
	}
	if f.Location != "app/secrets.go:12" {
		t.Errorf("location = %q (should use forward slashes)", f.Location)
	}
}

func TestParseGitleaksEmpty(t *testing.T) {
	findings, err := parseGitleaks([]byte("  \n"))
	if err != nil || findings != nil {
		t.Errorf("empty gitleaks output should yield no findings, got %v %v", findings, err)
	}
}

// fakeScanner lets us test RunAll without external binaries.
type fakeScanner struct {
	name      string
	available bool
	findings  []Finding
}

func (f fakeScanner) Name() string { return f.name }
func (f fakeScanner) Resolve(context.Context, Options) (Method, sandbox.ContainerRuntime, string, string) {
	if f.available {
		return MethodHost, "", "", ""
	}
	return MethodNone, "", "", "not installed"
}
func (f fakeScanner) Scan(context.Context, string, Method, sandbox.ContainerRuntime, string, Options) ([]Finding, error) {
	return f.findings, nil
}

func TestRunAllAggregatesAndSorts(t *testing.T) {
	scanners := []Scanner{
		fakeScanner{name: "low", available: true, findings: []Finding{{Tool: "a", Severity: SevLow, Title: "l"}}},
		fakeScanner{name: "crit", available: true, findings: []Finding{{Tool: "b", Severity: SevCritical, Title: "c"}}},
		fakeScanner{name: "missing", available: false},
	}
	rep := RunAll(context.Background(), ".", scanners)
	if len(rep.Findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(rep.Findings))
	}
	if rep.Findings[0].Severity != SevCritical {
		t.Errorf("findings not sorted by severity: %+v", rep.Findings)
	}
	if rep.Skipped["missing"] != "not installed" {
		t.Errorf("missing scanner not reported skipped: %v", rep.Skipped)
	}
	if rep.RanVia["low"] != "host" || rep.RanVia["crit"] != "host" {
		t.Errorf("expected host as the ran-via method for available scanners: %v", rep.RanVia)
	}
	out := rep.Format()
	if !strings.Contains(out, "Findings: 2") || !strings.Contains(out, "CRITICAL") {
		t.Errorf("format output unexpected: %q", out)
	}
}

// fakeImageScanner lets us test ScanImage without external binaries.
type fakeImageScanner struct {
	name     string
	method   Method
	findings []Finding
}

func (f fakeImageScanner) Name() string { return f.name }
func (f fakeImageScanner) Resolve(context.Context, Options) (Method, sandbox.ContainerRuntime, string, string) {
	if f.method == MethodNone {
		return MethodNone, "", "", "not installed"
	}
	return f.method, "", "", ""
}
func (f fakeImageScanner) ScanImage(context.Context, string, Method) ([]Finding, error) {
	return f.findings, nil
}

func TestScanImageAggregatesAndSorts(t *testing.T) {
	scanners := []ImageScanner{
		fakeImageScanner{name: "low", method: MethodHost, findings: []Finding{{Tool: "a", Severity: SevLow, Title: "l"}}},
		fakeImageScanner{name: "crit", method: MethodHost, findings: []Finding{{Tool: "b", Severity: SevCritical, Title: "c"}}},
		fakeImageScanner{name: "missing", method: MethodNone},
	}
	rep := ScanImage(context.Background(), "alpine:3.20", scanners, Options{})
	if len(rep.Findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(rep.Findings))
	}
	if rep.Findings[0].Severity != SevCritical {
		t.Errorf("findings not sorted by severity: %+v", rep.Findings)
	}
	if rep.Skipped["missing"] != "not installed" {
		t.Errorf("missing scanner not reported skipped: %v", rep.Skipped)
	}
}

// TestScanImageSkipsContainerFallback is the P11.5 scoping regression: a
// scanner that Resolve would run via a container must be skipped with the
// documented reason, never silently run through a --network none container
// that can't reach a registry.
func TestScanImageSkipsContainerFallback(t *testing.T) {
	scanners := []ImageScanner{
		fakeImageScanner{name: "container-only", method: MethodContainer, findings: []Finding{{Tool: "x", Severity: SevHigh}}},
	}
	rep := ScanImage(context.Background(), "alpine:3.20", scanners, Options{})
	if len(rep.Findings) != 0 {
		t.Fatalf("expected no findings from a container-resolved scanner, got %+v", rep.Findings)
	}
	if rep.Skipped["container-only"] != imageContainerFallbackUnsupported {
		t.Errorf("reason = %q, want the container-fallback-unsupported message", rep.Skipped["container-only"])
	}
}

// TestRunWithOptionsDedupsAcrossScanners is the P11.8 regression: two
// scanners independently flagging the same CVE at the same location must
// collapse to one finding in the aggregated report, not two.
func TestRunWithOptionsDedupsAcrossScanners(t *testing.T) {
	scanners := []Scanner{
		fakeScanner{name: "osv-scanner", available: true, findings: []Finding{
			{Tool: "osv-scanner", RuleID: "CVE-2024-5555", Severity: SevHigh, Location: "pkg@1.0.0 (go.sum)"},
		}},
		fakeScanner{name: "trivy", available: true, findings: []Finding{
			{Tool: "trivy", RuleID: "CVE-2024-5555", Severity: SevCritical, Location: "go.sum"},
		}},
	}
	rep := RunWithOptions(context.Background(), t.TempDir(), scanners, Options{})
	if len(rep.Findings) != 1 {
		t.Fatalf("expected the duplicate CVE finding deduped to 1, got %d: %+v", len(rep.Findings), rep.Findings)
	}
	if rep.Findings[0].Severity != SevCritical {
		t.Errorf("expected the higher-severity copy kept, got %+v", rep.Findings[0])
	}
	if len(rep.Findings[0].SeenBy) != 1 || rep.Findings[0].SeenBy[0] != "osv-scanner" {
		t.Errorf("expected SeenBy to record osv-scanner, got %v", rep.Findings[0].SeenBy)
	}
}

// TestRunWithOptionsAppliesBaselineSuppression is the P11.8 regression for
// the suppression allowlist: a finding matched by an active baseline entry
// must move to Suppressed, not disappear or stay in Findings.
func TestRunWithOptionsAppliesBaselineSuppression(t *testing.T) {
	dir := t.TempDir()
	writeBaseline(t, dir, `
suppressions:
  - rule_id: "CVE-2024-7777"
    reason: "accepted risk, tracked in JIRA-123"
    expires: "2099-01-01"
`)
	scanners := []Scanner{
		fakeScanner{name: "trivy", available: true, findings: []Finding{
			{Tool: "trivy", RuleID: "CVE-2024-7777", Severity: SevHigh, Location: "go.sum"},
			{Tool: "trivy", RuleID: "CVE-2024-8888", Severity: SevMedium, Location: "go.sum"},
		}},
	}
	rep := RunWithOptions(context.Background(), dir, scanners, Options{})
	if len(rep.Findings) != 1 || rep.Findings[0].RuleID != "CVE-2024-8888" {
		t.Fatalf("expected only the unsuppressed finding in Findings, got %+v", rep.Findings)
	}
	if len(rep.Suppressed) != 1 || rep.Suppressed[0].RuleID != "CVE-2024-7777" {
		t.Fatalf("expected the accepted-risk finding in Suppressed, got %+v", rep.Suppressed)
	}
	out := rep.Format()
	if !strings.Contains(out, "Suppressed by baseline: 1") {
		t.Errorf("Format should surface the suppression count, got %q", out)
	}
}

func TestRunWithOptionsBaselineParseErrorFailsSafe(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aegis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BaselinePath(dir), []byte("not: [valid: yaml:"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanners := []Scanner{
		fakeScanner{name: "trivy", available: true, findings: []Finding{
			{Tool: "trivy", RuleID: "CVE-2024-9999", Severity: SevHigh, Location: "go.sum"},
		}},
	}
	rep := RunWithOptions(context.Background(), dir, scanners, Options{})
	if len(rep.Findings) != 1 {
		t.Fatalf("a broken baseline must never suppress findings, got %+v", rep.Findings)
	}
	if rep.BaselineError == "" {
		t.Error("expected BaselineError to be set for malformed YAML")
	}
}
