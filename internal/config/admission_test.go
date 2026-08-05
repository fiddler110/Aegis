package config

import "testing"

// TestAdmissionLimitAuto is the P59.9 policy in one table: an unset key is
// "auto", which bounds a local backend (one GPU, not a fleet) and leaves a
// cloud one alone.
func TestAdmissionLimitAuto(t *testing.T) {
	cases := []struct {
		name    string
		prov    string
		baseURL string
		want    int
	}{
		{"native ollama, default endpoint", "ollama", "", MaxConcurrentRequestsDefaultLocal},
		{"native ollama, explicit endpoint", "ollama", "http://localhost:11434", MaxConcurrentRequestsDefaultLocal},
		// An OpenAI-compatible server on loopback (LM Studio, llama.cpp,
		// a local proxy) is the same single-GPU resource as Ollama.
		{"openai-compat on loopback", "openai", "http://127.0.0.1:1234/v1", MaxConcurrentRequestsDefaultLocal},
		{"anthropic", "anthropic", "", 0},
		{"openai cloud", "openai", "", 0},
		{"openai via a remote gateway", "openai", "https://gateway.example.com/v1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p ProviderConfig
			if got := p.AdmissionLimit(tc.prov, tc.baseURL); got != tc.want {
				t.Errorf("AdmissionLimit(%q, %q) = %d, want %d", tc.prov, tc.baseURL, got, tc.want)
			}
		})
	}
}

// TestAdmissionLimitExplicit: a positive value applies everywhere, and a
// negative one is the escape hatch for an operator whose local host genuinely
// has room — distinct from unset, which is why 0 could not mean "unbounded".
func TestAdmissionLimitExplicit(t *testing.T) {
	p := ProviderConfig{MaxConcurrentRequests: 4}
	if got := p.AdmissionLimit("ollama", ""); got != 4 {
		t.Errorf("local explicit = %d, want 4", got)
	}
	if got := p.AdmissionLimit("anthropic", ""); got != 4 {
		t.Errorf("cloud explicit = %d, want 4", got)
	}

	off := ProviderConfig{MaxConcurrentRequests: -1}
	if got := off.AdmissionLimit("ollama", ""); got != 0 {
		t.Errorf("local explicitly-unbounded = %d, want 0", got)
	}
}
