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

// TestPatchSecurityRoundTripsCarriedFields is a regression test for fields
// being dropped by a write that didn't mean to touch them. patchSecurity
// rewrites the whole security: block, so any field absent from SecurityPatch
// is deleted from the operator's config as a side effect — which is what
// happened to security.wsl_distro and security.debate: a `/security-config`
// save or `aegis harden` run silently removed them.
func TestPatchSecurityRoundTripsCarriedFields(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	want := SecurityPatch{
		DefaultMethod: "auto",
		WSLDistro:     "kali-linux",
		Debate:        DebateIntegrationConfig{ThreatModel: true, Triage: true},
		Multiscanner: MultiscannerConfig{
			Enabled:     true,
			Image:       "localhost/aegis-multiscanner:v1",
			ImageID:     "sha256:abc123",
			Concurrency: 4,
			Tools:       []string{"trivy", "gitleaks"},
		},
	}
	if err := PatchGlobalSecurity(want); err != nil {
		t.Fatalf("PatchGlobalSecurity: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Security.WSLDistro != "kali-linux" {
		t.Errorf("wsl_distro = %q, want kali-linux (dropped by the write)", cfg.Security.WSLDistro)
	}
	if !cfg.Security.Debate.ThreatModel || !cfg.Security.Debate.Triage {
		t.Errorf("debate = %+v, want both true (dropped by the write)", cfg.Security.Debate)
	}
	ms := cfg.Security.Multiscanner
	if !ms.Enabled || ms.Image != want.Multiscanner.Image || ms.ImageID != want.Multiscanner.ImageID {
		t.Errorf("multiscanner = %+v, want %+v", ms, want.Multiscanner)
	}
	if ms.Concurrency != 4 {
		t.Errorf("multiscanner.concurrency = %d, want 4", ms.Concurrency)
	}
	if strings.Join(ms.Tools, ",") != "trivy,gitleaks" {
		t.Errorf("multiscanner.tools = %v, want [trivy gitleaks]", ms.Tools)
	}
}

// TestPatchSecurityOmitsUnbuiltMultiscanner keeps a never-built multiscanner
// out of the file entirely, rather than writing a block that claims an enabled
// image with nothing behind it.
func TestPatchSecurityOmitsUnbuiltMultiscanner(t *testing.T) {
	block := buildSecurityBlock(SecurityPatch{DefaultMethod: "auto"})
	if strings.Contains(block, "multiscanner:") {
		t.Errorf("empty multiscanner should not be written, got:\n%s", block)
	}
	if strings.Contains(block, "wsl_distro:") || strings.Contains(block, "debate:") {
		t.Errorf("empty wsl_distro/debate should not be written, got:\n%s", block)
	}
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

// TestPatchGlobalSecurityPreservesDASTPolicy is the P11.7 sibling of
// TestPatchGlobalSecurityPreservesEgressPolicy: DAST is part of the same
// wholesale-replaced security: block, so patching only DefaultMethod/Tools
// must not silently drop an operator's target allowlist/allow_active.
func TestPatchGlobalSecurityPreservesDASTPolicy(t *testing.T) {
	redirectConfigDir(t)

	err := PatchGlobalSecurity(SecurityPatch{
		DefaultMethod: "auto",
		DAST: DASTConfig{
			AllowedTargets: []string{"staging.example.com", ".internal.example.com"},
			AllowActive:    true,
		},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !cfg.Security.DAST.AllowActive {
		t.Error("dast.allow_active was dropped")
	}
	want := []string{"staging.example.com", ".internal.example.com"}
	if len(cfg.Security.DAST.AllowedTargets) != len(want) {
		t.Fatalf("dast.allowed_targets = %v, want %v", cfg.Security.DAST.AllowedTargets, want)
	}
	for i, w := range want {
		if cfg.Security.DAST.AllowedTargets[i] != w {
			t.Errorf("allowed_targets[%d] = %q, want %q", i, cfg.Security.DAST.AllowedTargets[i], w)
		}
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

// TestPatchGlobalSecurityPreservesNucleiTemplatesVersion is the P13.5.6
// sibling of the DAST/egress preservation tests above: a tool's
// templates_version must round-trip through the same wholesale-replaced
// security.tools.<name> block as its enabled/method/image fields.
func TestPatchGlobalSecurityPreservesNucleiTemplatesVersion(t *testing.T) {
	redirectConfigDir(t)

	err := PatchGlobalSecurity(SecurityPatch{
		DefaultMethod: "auto",
		Tools: map[string]SecurityToolConfig{
			"nuclei": {TemplatesVersion: "v9.9.0"},
		},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cfg.Security.Tools["nuclei"].TemplatesVersion; got != "v9.9.0" {
		t.Errorf("templates_version = %q, want v9.9.0", got)
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
