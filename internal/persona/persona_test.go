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

func TestSecurityPromptCoversModes(t *testing.T) {
	p, _ := Get("security")
	for _, want := range []string{"STRIDE", "THREAT MODELING", "ISSUE IDENTIFICATION", "security_scan", "Mermaid"} {
		if !contains(p.System, want) {
			t.Errorf("security prompt missing %q", want)
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
