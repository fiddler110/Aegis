package persona

import "testing"

func TestGet(t *testing.T) {
	if p, ok := Get("security"); !ok || p.Name != "security" {
		t.Errorf("security persona not found: %+v ok=%v", p, ok)
	}
	if p, ok := Get(""); !ok || p.Name != "general" {
		t.Errorf("empty should default to general, got %+v ok=%v", p, ok)
	}
	if p, ok := Get("nonexistent"); ok || p.Name != "general" {
		t.Errorf("unknown should fall back to general and report not-ok, got %+v ok=%v", p, ok)
	}
}

func TestAllRegisteredPersonasResolvable(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			t.Errorf("persona %q listed in Names() but not found in registry", name)
		}
		if p.Name != name {
			t.Errorf("persona %q has mismatched Name field: %q", name, p.Name)
		}
		if p.Description == "" {
			t.Errorf("persona %q has empty Description", name)
		}
		if p.System == "" {
			t.Errorf("persona %q has empty System prompt", name)
		}
	}
}

func TestNamesMatchesRegistry(t *testing.T) {
	names := Names()
	if len(names) != len(registry) {
		t.Errorf("Names() has %d entries but registry has %d", len(names), len(registry))
	}
	seen := make(map[string]bool)
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate name in Names(): %q", n)
		}
		seen[n] = true
	}
}

func TestSecurityPromptCoversModes(t *testing.T) {
	p, _ := Get("security")
	for _, want := range []string{"STRIDE", "THREAT MODELING", "ISSUE IDENTIFICATION", "security_scan", "Mermaid"} {
		if !contains(p.System, want) {
			t.Errorf("security prompt missing %q", want)
		}
	}
}

func TestPersonaPromptKeywords(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
	}{
		{"platform-architect", []string{"PLATFORM ARCHITECT", "ARCHITECTURE DESIGN", "CAPACITY"}},
		{"security-architect", []string{"SECURITY ARCHITECT", "STRIDE", "THREAT MODELING"}},
		{"security-engineer", []string{"SECURITY ENGINEER", "VULNERABILITY MANAGEMENT", "INCIDENT RESPONSE"}},
		{"appsec-engineer", []string{"APPLICATION SECURITY", "OWASP", "SECURE CODE REVIEW"}},
		{"developer", []string{"SOFTWARE DEVELOPER", "DEBUGGING", "TESTING"}},
		{"security-researcher", []string{"SECURITY RESEARCHER", "MITRE ATT&CK", "VULNERABILITY RESEARCH"}},
		{"risk-assessor", []string{"RISK ASSESSOR", "RISK IDENTIFICATION", "RISK TREATMENT"}},
		{"business-analyst", []string{"BUSINESS ANALYST", "REQUIREMENTS", "STAKEHOLDER"}},
		{"data-analyst", []string{"DATA ANALYST", "STATISTICAL", "VISUALIZATION"}},
		{"network-security-architect", []string{"NETWORK SECURITY ARCHITECT", "micro-segmentation", "zero-trust"}},
		{"report-writer", []string{"REPORT WRITER", "executive summary", "TECHNICAL WRITING"}},
		{"sre", []string{"SITE RELIABILITY ENGINEER", "SLO", "OBSERVABILITY", "INCIDENT"}},
		{"infrastructure-architect", []string{"INFRASTRUCTURE ARCHITECT", "INFRASTRUCTURE AS CODE", "Terraform"}},
		{"cloud-architect", []string{"CLOUD ARCHITECT", "well-architected", "CLOUD MIGRATION"}},
		{"cloud-security-engineer", []string{"CLOUD SECURITY ENGINEER", "IAM", "CIS Benchmarks", "GuardDuty"}},
	}
	for _, tt := range tests {
		p, ok := Get(tt.name)
		if !ok {
			t.Errorf("persona %q not found", tt.name)
			continue
		}
		for _, kw := range tt.keywords {
			if !contains(p.System, kw) {
				t.Errorf("persona %q prompt missing keyword %q", tt.name, kw)
			}
		}
	}
}

func TestToolUseBlock_content(t *testing.T) {
	block := ToolUseBlock()
	for _, want := range []string{
		"IMMEDIATELY",
		"narration of intent",
		"A tool result is input",
		"truncated",
	} {
		if !contains(block, want) {
			t.Errorf("ToolUseBlock() missing expected phrase: %q", want)
		}
	}
}

func TestGeneralSystem_noGenericToolRules(t *testing.T) {
	p, _ := Get("general")
	if contains(p.System, "call the appropriate tool IMMEDIATELY") {
		t.Error("generalSystem still contains generic call-immediately rule (should be removed — covered by shared ToolUseBlock)")
	}
}

func TestSecuritySystem_genericRulesRemoved(t *testing.T) {
	p, _ := Get("security")
	if contains(p.System, "Never narrate intent") {
		t.Error("securitySystem still contains generic narration rule (should be removed — covered by shared ToolUseBlock)")
	}
	if !contains(p.System, "LOCAL project or workspace") {
		t.Error("securitySystem must keep LOCAL/EXTERNAL tool selection guidance")
	}
}

func TestSecurityArchitectSystem_genericRulesRemoved(t *testing.T) {
	p, _ := Get("security-architect")
	if contains(p.System, "Call tools immediately. Do not write") {
		t.Error("securityArchitectSystem still contains generic call-immediately rule (should be removed — covered by shared ToolUseBlock)")
	}
	if !contains(p.System, "LOCAL project or workspace") {
		t.Error("securityArchitectSystem must keep LOCAL/EXTERNAL tool selection guidance")
	}
}

func TestPersonas_allHaveCompletingOutput(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			t.Errorf("persona.Get(%q) returned false", name)
			continue
		}
		if !contains(p.System, "## Completing your output") {
			t.Errorf("persona %q missing ## Completing your output section", name)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
