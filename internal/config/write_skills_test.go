package config

import (
	"os"
	"strings"
	"testing"
)

func TestPatchProjectSkillsEnabledWritesAndReloads(t *testing.T) {
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

	if err := PatchProjectSkillsEnabled([]string{"security-audit", "html-report"}); err != nil {
		t.Fatalf("PatchProjectSkillsEnabled: %v", err)
	}

	data, err := os.ReadFile(ProjectConfigPath())
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if !strings.Contains(string(data), "security-audit") || !strings.Contains(string(data), "html-report") {
		t.Errorf("builtin_enabled not written:\n%s", data)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cfg.Skills.BuiltinEnabled) != 2 {
		t.Errorf("reloaded Skills.BuiltinEnabled = %v, want 2 entries", cfg.Skills.BuiltinEnabled)
	}
}

func TestPatchProjectSkillsEnabledPreservesOtherSections(t *testing.T) {
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
	if err := PatchProjectSkillsEnabled([]string{"security-audit"}); err != nil {
		t.Fatalf("PatchProjectSkillsEnabled: %v", err)
	}

	data, _ := os.ReadFile(ProjectConfigPath())
	out := string(data)
	if !strings.Contains(out, "sandbox:") || !strings.Contains(out, "backend: auto") {
		t.Errorf("sandbox block was clobbered:\n%s", out)
	}
	if !strings.Contains(out, "security-audit") {
		t.Errorf("builtin_enabled not written:\n%s", out)
	}
}

func TestPatchProjectSkillsEnabledEmptyList(t *testing.T) {
	redirectConfigDir(t)
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if err := PatchProjectSkillsEnabled([]string{"security-audit"}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := PatchProjectSkillsEnabled(nil); err != nil {
		t.Fatalf("disable: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cfg.Skills.BuiltinEnabled) != 0 {
		t.Errorf("expected empty BuiltinEnabled after clearing, got %v", cfg.Skills.BuiltinEnabled)
	}
}

// TestProjectSkillsEnabledDoesNotMergeWithGlobal documents the real koanf
// layering behavior for list-valued keys: the project file's list replaces
// the global file's list entirely rather than merging element-wise. Callers
// (CLI/TUI) must therefore read the already-merged effective list from
// config.Load() and write back the full desired set, not a delta against
// only their own layer.
func TestProjectSkillsEnabledDoesNotMergeWithGlobal(t *testing.T) {
	redirectConfigDir(t)
	if err := PatchGlobalSkillsEnabled([]string{"html-report"}); err != nil {
		t.Fatalf("seed global: %v", err)
	}

	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if err := PatchProjectSkillsEnabled([]string{"security-audit"}); err != nil {
		t.Fatalf("PatchProjectSkillsEnabled: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cfg.Skills.BuiltinEnabled) != 1 || cfg.Skills.BuiltinEnabled[0] != "security-audit" {
		t.Errorf("expected project list to fully replace global list, got %v", cfg.Skills.BuiltinEnabled)
	}
}
