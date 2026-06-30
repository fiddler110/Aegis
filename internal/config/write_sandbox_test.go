package config

import (
	"os"
	"strings"
	"testing"
)

func TestPatchGlobalSandboxCreatesBlock(t *testing.T) {
	redirectConfigDir(t)

	if err := PatchGlobalSandbox(SandboxPatch{Backend: "auto", Image: "ubuntu:22.04"}); err != nil {
		t.Fatalf("PatchGlobalSandbox: %v", err)
	}
	data, err := os.ReadFile(GlobalConfigPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "sandbox:") || !strings.Contains(out, "backend: auto") {
		t.Errorf("sandbox block not written:\n%s", out)
	}
}

func TestPatchGlobalSandboxPreservesProvider(t *testing.T) {
	redirectConfigDir(t)

	// Seed with a provider block first.
	if err := PatchGlobalProvider(ProviderPatch{Adapter: "anthropic", Model: "claude-opus-4-8", MaxTokens: 1000}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if err := PatchGlobalSandbox(SandboxPatch{Backend: "container", Runtime: "docker"}); err != nil {
		t.Fatalf("PatchGlobalSandbox: %v", err)
	}

	data, _ := os.ReadFile(GlobalConfigPath())
	out := string(data)
	if !strings.Contains(out, "provider:") || !strings.Contains(out, "claude-opus-4-8") {
		t.Errorf("provider block was clobbered:\n%s", out)
	}
	if !strings.Contains(out, "runtime: docker") {
		t.Errorf("sandbox runtime not written:\n%s", out)
	}

	// The written config must re-load with both sections intact.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Sandbox.Backend != "container" || cfg.Sandbox.Runtime != "docker" {
		t.Errorf("reloaded sandbox = %+v", cfg.Sandbox)
	}
	if cfg.Provider.Model != "claude-opus-4-8" {
		t.Errorf("reloaded provider model = %q", cfg.Provider.Model)
	}
}

func TestPatchGlobalSandboxWritesPriority(t *testing.T) {
	redirectConfigDir(t)
	if err := PatchGlobalSandbox(SandboxPatch{Backend: "auto", Priority: []string{"wslc", "docker"}}); err != nil {
		t.Fatalf("PatchGlobalSandbox: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cfg.Sandbox.Priority) != 2 || cfg.Sandbox.Priority[0] != "wslc" {
		t.Errorf("reloaded priority = %v, want [wslc docker]", cfg.Sandbox.Priority)
	}
}
