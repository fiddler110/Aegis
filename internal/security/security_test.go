package security

import (
	"context"
	"strings"
	"testing"
)

func TestParseSemgrep(t *testing.T) {
	data := []byte(`{"results":[
		{"check_id":"go.lang.security.audit.sqli","path":"db/query.go","start":{"line":42},
		 "extra":{"message":"possible SQL injection","severity":"ERROR"}},
		{"check_id":"generic.secrets.key","path":"cfg.go","start":{"line":7},
		 "extra":{"message":"hardcoded key","severity":"WARNING"}}
	]}`)
	findings, err := parseSemgrep(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	if findings[0].Severity != SevHigh {
		t.Errorf("ERROR should map to HIGH, got %s", findings[0].Severity)
	}
	if findings[0].Location != "db/query.go:42" {
		t.Errorf("location = %q", findings[0].Location)
	}
	if findings[1].Severity != SevMedium {
		t.Errorf("WARNING should map to MEDIUM, got %s", findings[1].Severity)
	}
}

func TestParseTrivy(t *testing.T) {
	data := []byte(`{"Results":[
		{"Target":"go.sum","Vulnerabilities":[
			{"VulnerabilityID":"CVE-2024-1234","PkgName":"foo","InstalledVersion":"1.0.0",
			 "FixedVersion":"1.0.1","Severity":"CRITICAL","Title":"RCE in foo","Description":"bad"}
		]},
		{"Target":"Dockerfile","Misconfigurations":[
			{"ID":"DS002","Severity":"HIGH","Title":"root user","Message":"runs as root","Resolution":"add USER"}
		]}
	]}`)
	findings, err := parseTrivy(data)
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

func (f fakeScanner) Name() string    { return f.name }
func (f fakeScanner) Available() bool { return f.available }
func (f fakeScanner) Scan(context.Context, string) ([]Finding, error) {
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
	out := rep.Format()
	if !strings.Contains(out, "Findings: 2") || !strings.Contains(out, "CRITICAL") {
		t.Errorf("format output unexpected: %q", out)
	}
}
