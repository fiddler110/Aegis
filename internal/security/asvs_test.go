package security

import "testing"

func TestExtractCWEFromTextRecognizesCommonForms(t *testing.T) {
	cases := map[string]string{
		"CWE-79: Cross-site Scripting": "79",
		"cwe:89":                       "89",
		"cwe_918":                      "918",
		"no cwe reference here at all": "",
		"security":                     "",
	}
	for in, want := range cases {
		if got := extractCWEFromText(in); got != want {
			t.Errorf("extractCWEFromText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestASVSForKnownCWEMapsToChapter(t *testing.T) {
	f := Finding{Tool: "opengrep", CWE: "89"}
	got := asvsFor(f)
	if got != "V5.3 Output Encoding and Injection Prevention" {
		t.Fatalf("got %q", got)
	}
}

func TestASVSUnmappedCWELeftEmpty(t *testing.T) {
	f := Finding{Tool: "opengrep", CWE: "999999"}
	if got := asvsFor(f); got != "" {
		t.Fatalf("expected no mapping for an unrecognized CWE, got %q", got)
	}
}

func TestASVSToolFallbackForGitleaks(t *testing.T) {
	f := Finding{Tool: "gitleaks", RuleID: "generic-api-key"}
	if got := asvsFor(f); got != "V6.4 Secret Management" {
		t.Fatalf("got %q", got)
	}
}

func TestASVSTrivyMisconfigVsCVESplit(t *testing.T) {
	misconfig := Finding{Tool: "trivy", RuleID: "AVD-AWS-0001"}
	if got := asvsFor(misconfig); got != "V14 Configuration" {
		t.Fatalf("misconfig: got %q", got)
	}
	vuln := Finding{Tool: "trivy", RuleID: "CVE-2024-1234"}
	if got := asvsFor(vuln); got != "" {
		t.Fatalf("SCA vuln finding should stay unmapped, got %q", got)
	}
}

func TestASVSUnmappedForSCATools(t *testing.T) {
	for _, tool := range []string{"osv-scanner", "grype", "dockle"} {
		f := Finding{Tool: tool, RuleID: "CVE-2024-1234"}
		if got := asvsFor(f); got != "" {
			t.Errorf("tool %s: expected unmapped SCA finding, got %q", tool, got)
		}
	}
}

func TestAssignASVSSkipsAlreadySetValues(t *testing.T) {
	findings := []Finding{
		{Tool: "opengrep", CWE: "79", ASVS: "custom-override"},
		{Tool: "gitleaks"},
	}
	assignASVS(findings)
	if findings[0].ASVS != "custom-override" {
		t.Errorf("expected preset ASVS left untouched, got %q", findings[0].ASVS)
	}
	if findings[1].ASVS != "V6.4 Secret Management" {
		t.Errorf("expected gitleaks fallback applied, got %q", findings[1].ASVS)
	}
}
