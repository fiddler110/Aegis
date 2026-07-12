package cli

import (
	"os"
	"path/filepath"
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
}
