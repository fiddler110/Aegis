package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/fiddler110/aegis/internal/config"
)

func loadTemplate(t *testing.T, tmpl string) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		t.Fatalf("template is not valid YAML: %v", err)
	}
	var cfg config.Config
	if err := k.Unmarshal("", &cfg); err != nil {
		t.Fatalf("template does not unmarshal into Config: %v", err)
	}
	return cfg
}

func TestTemplatesParseAndUnmarshal(t *testing.T) {
	loadTemplate(t, projectConfigTemplate)
	cfg := loadTemplate(t, globalConfigTemplate)
	// P25.3: the Ollama-flavored global template ships the guard disabled — an
	// llm rubric self-check on the session's own local (often thinking) model
	// roughly doubles turn latency — but keeps mode: llm configured so /guard
	// on (or enabled: true once small_model is set) works without more edits.
	if cfg.OutputGuard.Enabled || cfg.OutputGuard.Mode != "llm" {
		t.Errorf("output_guard not parsed as disabled-but-configured from template: %+v", cfg.OutputGuard)
	}
	if _, ok := cfg.Personas["security-architect"]; !ok {
		t.Errorf("personas map missing security-architect: %+v", cfg.Personas)
	}
	// The Ollama-flavored global template must default to the native /api/chat
	// adapter, not the legacy OpenAI-compat path (default: openai + a /v1
	// base_url) that serves every request at Ollama's 4096 default and that the
	// daemon warns against. Guards the P35.13 template fix against regression.
	if cfg.Provider.Default != "ollama" {
		t.Errorf("template provider.default = %q, want \"ollama\" (native adapter)", cfg.Provider.Default)
	}
	if strings.HasSuffix(strings.TrimRight(cfg.Provider.BaseURL, "/"), "/v1") {
		t.Errorf("template provider.base_url = %q carries a /v1 OpenAI-compat suffix", cfg.Provider.BaseURL)
	}
}
