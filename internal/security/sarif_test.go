package security

import "testing"

func TestParseSARIFSeverityPrecedence(t *testing.T) {
	// security-severity score should win over the bare SARIF level when no
	// tag is present.
	data := []byte(`{"runs":[{
		"tool":{"driver":{"name":"grype","rules":[
			{"id":"CVE-1","properties":{"security-severity":"9.8"}}
		]}},
		"results":[
			{"ruleId":"CVE-1","level":"warning","message":{"text":"critical thing"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"go.mod"}}}]}
		]
	}]}`)
	findings, err := ParseSARIF(data, "grype")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Severity != SevCritical {
		t.Fatalf("expected security-severity score to override level to CRITICAL, got %+v", findings)
	}
}

func TestParseSARIFRuleIndexFallback(t *testing.T) {
	// Some tools omit ruleId and only reference rules by ruleIndex.
	data := []byte(`{"runs":[{
		"tool":{"driver":{"name":"hadolint","rules":[
			{"id":"DL3000","shortDescription":{"text":"use absolute WORKDIR"}}
		]}},
		"results":[
			{"ruleIndex":0,"level":"error","message":{"text":""},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"Dockerfile"},"region":{"startLine":3}}}]}
		]
	}]}`)
	findings, err := ParseSARIF(data, "hadolint")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if findings[0].RuleID != "DL3000" {
		t.Errorf("ruleId should fall back to rules[ruleIndex].id, got %q", findings[0].RuleID)
	}
	if findings[0].Title != "use absolute WORKDIR" {
		t.Errorf("title should fall back to rule shortDescription, got %q", findings[0].Title)
	}
	if findings[0].Location != "Dockerfile:3" {
		t.Errorf("location = %q", findings[0].Location)
	}
}

func TestParseSARIFDefaultToolName(t *testing.T) {
	data := []byte(`{"runs":[{
		"tool":{"driver":{"name":""}},
		"results":[{"ruleId":"r1","level":"note","message":{"text":"info finding"}}]
	}]}`)
	findings, err := ParseSARIF(data, "fallback-tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Tool != "fallback-tool" {
		t.Fatalf("expected defaultTool fallback, got %+v", findings)
	}
	if findings[0].Severity != SevLow {
		t.Errorf("note level should map to LOW, got %s", findings[0].Severity)
	}
}

func TestParseSARIFEmptyRuns(t *testing.T) {
	findings, err := ParseSARIF([]byte(`{"runs":[]}`), "tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %+v", findings)
	}
}
