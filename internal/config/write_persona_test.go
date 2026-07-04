package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchProjectDefaultPersonaWritesAndReloads(t *testing.T) {
	redirectConfigDir(t)
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if err := PatchProjectDefaultPersona("developer"); err != nil {
		t.Fatalf("PatchProjectDefaultPersona: %v", err)
	}

	data, err := os.ReadFile(ProjectConfigPath())
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if !strings.Contains(string(data), `default_persona: "developer"`) {
		t.Errorf("default_persona not written:\n%s", data)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultPersona != "developer" {
		t.Errorf("reloaded DefaultPersona = %q, want developer", cfg.DefaultPersona)
	}
}

func TestPatchProjectDefaultPersonaPreservesOtherSections(t *testing.T) {
	redirectConfigDir(t)
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if err := PatchProjectSandbox(SandboxPatch{Backend: "auto"}); err != nil {
		t.Fatalf("seed sandbox: %v", err)
	}
	if err := PatchProjectDefaultPersona("security"); err != nil {
		t.Fatalf("PatchProjectDefaultPersona: %v", err)
	}

	data, _ := os.ReadFile(ProjectConfigPath())
	out := string(data)
	if !strings.Contains(out, "sandbox:") || !strings.Contains(out, "backend: auto") {
		t.Errorf("sandbox block was clobbered:\n%s", out)
	}
	if !strings.Contains(out, `default_persona: "security"`) {
		t.Errorf("default_persona not written:\n%s", out)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultPersona != "security" || cfg.Sandbox.Backend != "auto" {
		t.Errorf("reloaded cfg default_persona=%q sandbox.backend=%q", cfg.DefaultPersona, cfg.Sandbox.Backend)
	}
}

func TestPatchGlobalDefaultPersona(t *testing.T) {
	redirectConfigDir(t)

	if err := PatchGlobalDefaultPersona("sre"); err != nil {
		t.Fatalf("PatchGlobalDefaultPersona: %v", err)
	}
	data, err := os.ReadFile(GlobalConfigPath())
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(data), `default_persona: "sre"`) {
		t.Errorf("default_persona not written:\n%s", data)
	}
}

// TestProjectDefaultPersonaOverridesGlobal exercises the real layered-config
// precedence: project .aegis/config.yaml must win over the global file.
func TestProjectDefaultPersonaOverridesGlobal(t *testing.T) {
	redirectConfigDir(t)
	if err := PatchGlobalDefaultPersona("sre"); err != nil {
		t.Fatalf("seed global: %v", err)
	}

	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if err := PatchProjectDefaultPersona("developer"); err != nil {
		t.Fatalf("PatchProjectDefaultPersona: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultPersona != "developer" {
		t.Errorf("project default_persona should win, got %q", cfg.DefaultPersona)
	}
}

func TestPatchDefaultPersonaCreatesConfigDir(t *testing.T) {
	redirectConfigDir(t)
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "project")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if err := PatchProjectDefaultPersona("appsec-engineer"); err != nil {
		t.Fatalf("PatchProjectDefaultPersona: %v", err)
	}
	if _, err := os.Stat(ProjectConfigPath()); err != nil {
		t.Errorf(".aegis/config.yaml not created: %v", err)
	}
}
