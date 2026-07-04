package config

import (
	"os"
	"strings"
	"testing"
)

func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })
}

func TestPatchGlobalSecurityCreatesBlock(t *testing.T) {
	redirectConfigDir(t)

	err := PatchGlobalSecurity(SecurityPatch{
		DefaultMethod: "container",
		Tools: map[string]SecurityToolConfig{
			"trivy": {Method: "auto", Image: "aquasec/trivy@sha256:deadbeef"},
		},
	})
	if err != nil {
		t.Fatalf("PatchGlobalSecurity: %v", err)
	}
	data, err := os.ReadFile(GlobalConfigPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "security:") || !strings.Contains(out, "default_method: container") {
		t.Errorf("security block not written:\n%s", out)
	}
	if !strings.Contains(out, "trivy:") || !strings.Contains(out, "aquasec/trivy@sha256:deadbeef") {
		t.Errorf("tool config not written:\n%s", out)
	}
}

// TestPatchGlobalSecurityPreservesEgressPolicy is the regression for patching
// only DefaultMethod/Tools while another config surface (or a hand-edited
// file) had already set egress_then_write/network_allowlist: since
// patchSecurity replaces the whole security: block, forgetting to carry
// those through would silently drop an operator's contextual-security
// settings as a side effect of editing scanner config.
func TestPatchGlobalSecurityPreservesEgressPolicy(t *testing.T) {
	redirectConfigDir(t)

	err := PatchGlobalSecurity(SecurityPatch{
		EgressThenWrite:  true,
		NetworkAllowList: []string{"api.github.com"},
		DefaultMethod:    "auto",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !cfg.Security.EgressThenWrite {
		t.Error("egress_then_write was dropped")
	}
	if len(cfg.Security.NetworkAllowList) != 1 || cfg.Security.NetworkAllowList[0] != "api.github.com" {
		t.Errorf("network_allowlist = %v, want [api.github.com]", cfg.Security.NetworkAllowList)
	}
}

func TestPatchProjectSecurityPreservesOtherSections(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	if err := PatchProjectDefaultPersona("security-architect"); err != nil {
		t.Fatalf("seed persona: %v", err)
	}
	if err := PatchProjectSecurity(SecurityPatch{DefaultMethod: "host"}); err != nil {
		t.Fatalf("PatchProjectSecurity: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultPersona != "security-architect" {
		t.Errorf("default_persona was clobbered: %q", cfg.DefaultPersona)
	}
	if cfg.Security.DefaultMethod != "host" {
		t.Errorf("default_method = %q, want host", cfg.Security.DefaultMethod)
	}
}

func TestBuildSecurityBlockEnabledFalseIsExplicit(t *testing.T) {
	disabled := false
	block := buildSecurityBlock(SecurityPatch{
		Tools: map[string]SecurityToolConfig{"semgrep": {Enabled: &disabled}},
	})
	if !strings.Contains(block, "enabled: false") {
		t.Errorf("expected an explicit enabled: false, got:\n%s", block)
	}
}
