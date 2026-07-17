package providerfactory

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

func TestIsLegacyOllamaCompat(t *testing.T) {
	tests := []struct {
		name    string
		def     string
		baseURL string
		want    bool
	}{
		{"the pre-P33.9 config this exists for", "openai", "http://localhost:11434/v1", true},
		{"11434 without the /v1 suffix", "openai", "http://localhost:11434", true},
		{"11434 on another host", "openai", "http://gpu-box.lan:11434/v1", true},
		{"any non-openai /v1 base", "openai", "http://127.0.0.1:1234/v1", true},
		{"case-insensitive provider name", "OpenAI", "http://localhost:11434/v1", true},
		{"trailing slash", "openai", "http://localhost:11434/v1/", true},

		{"the real OpenAI endpoint", "openai", "https://api.openai.com/v1", false},
		{"the real OpenAI endpoint, odd case", "openai", "https://API.OpenAI.com/v1", false},
		{"empty base_url is the plain OpenAI default", "openai", "", false},
		{"already on the native adapter", "ollama", "http://localhost:11434", false},
		{"native adapter with a lingering /v1", "ollama", "http://localhost:11434/v1", false},
		{"anthropic is not affected", "anthropic", "https://gateway.corp/v1", false},
		{"a non-/v1 openai base_url is some other gateway", "openai", "https://gateway.corp/api", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := config.ProviderConfig{Default: tc.def, BaseURL: tc.baseURL}
			if got := IsLegacyOllamaCompat(p); got != tc.want {
				t.Errorf("IsLegacyOllamaCompat(%q, %q) = %v, want %v", tc.def, tc.baseURL, got, tc.want)
			}
			// Detail/Fix are gated on the same predicate and must agree with it.
			if gotDetail := LegacyOllamaCompatDetail(p) != ""; gotDetail != tc.want {
				t.Errorf("LegacyOllamaCompatDetail non-empty = %v, want %v", gotDetail, tc.want)
			}
			if gotFix := LegacyOllamaCompatFix(p) != ""; gotFix != tc.want {
				t.Errorf("LegacyOllamaCompatFix non-empty = %v, want %v", gotFix, tc.want)
			}
		})
	}
}

// The fix has to be copy-pasteable and honest about the think regression —
// describing the change instead of spelling it out is the failure mode P34.5
// names explicitly.
func TestLegacyOllamaCompatFixNamesEveryConfigKey(t *testing.T) {
	fix := LegacyOllamaCompatFix(config.ProviderConfig{Default: "openai", BaseURL: "http://localhost:11434/v1"})
	for _, want := range []string{
		"provider.default: ollama",
		"provider.base_url: http://localhost:11434", // the /v1 the native adapter doesn't want, stripped
		"provider.context_window: 32768",
		"provider.think: true",
	} {
		if !strings.Contains(fix, want) {
			t.Errorf("fix %q does not mention %q", fix, want)
		}
	}
	if strings.Contains(fix, "11434/v1") {
		t.Errorf("fix suggests the compat /v1 base_url, not the native one: %q", fix)
	}
}

// A base_url that only might be Ollama (a /v1 base on a non-default port) must
// not be asserted to be one.
func TestLegacyOllamaCompatDetailHedgesWhenNotCertain(t *testing.T) {
	certain := LegacyOllamaCompatDetail(config.ProviderConfig{Default: "openai", BaseURL: "http://localhost:11434/v1"})
	if strings.Contains(certain, "if that is") {
		t.Errorf("detail hedges on an unambiguous :11434 base_url: %q", certain)
	}
	maybe := LegacyOllamaCompatDetail(config.ProviderConfig{Default: "openai", BaseURL: "http://localhost:1234/v1"})
	if !strings.Contains(maybe, "if that is") {
		t.Errorf("detail asserts Ollama for a base_url that may be another compat server: %q", maybe)
	}
}
