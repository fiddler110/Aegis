package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/config"
)

// redirectConfigDir isolates config.Load()/PatchGlobal*/PatchProject* from
// the real machine's config, mirroring internal/config's own test helper
// (unexported there, so duplicated here rather than exported just for tests).
func redirectConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
}

func chdirTempTUI(t *testing.T) {
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

func TestCmdSecurityConfigDefaultsToProjectScope(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security-config"})
	if res.SecurityConfigGlobal == nil {
		t.Fatal("expected SecurityConfigGlobal to be set")
	}
	if *res.SecurityConfigGlobal {
		t.Error("expected project scope (false) with no args")
	}
}

func TestCmdSecurityConfigGlobalArg(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security-config", Args: []string{"global"}})
	if res.SecurityConfigGlobal == nil || !*res.SecurityConfigGlobal {
		t.Fatal("expected global scope (true) with 'global' arg")
	}
}

// TestSecurityConfigApplyEditUpdatesWorkingCopy is the core edit-flow
// regression: editing a tool's fields and completing the form must land in
// m.tools under that tool's name, not silently no-op or overwrite the wrong
// entry.
func TestSecurityConfigApplyEditUpdatesWorkingCopy(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	m := newSecurityConfigModel(80, 24, theme{}, false)
	m.startEdit("trivy")
	if m.editEnabled != true || m.editMethod != "auto" || m.editInstall != "prompt" {
		t.Fatalf("unexpected defaults for a never-configured tool: enabled=%v method=%q install=%q", m.editEnabled, m.editMethod, m.editInstall)
	}

	m.editEnabled = false
	m.editMethod = "container"
	m.editInstall = "always"
	m.editImage = "aquasec/trivy@sha256:deadbeef"
	m.applyEdit()

	tc, ok := m.tools["trivy"]
	if !ok {
		t.Fatal("expected trivy to be present in the working copy after applyEdit")
	}
	if tc.ToolEnabled() {
		t.Error("expected trivy to be disabled")
	}
	if tc.Method != "container" || tc.Install != "always" || tc.Image != "aquasec/trivy@sha256:deadbeef" {
		t.Errorf("tool config = %+v, values not applied correctly", tc)
	}
}

// TestSecurityConfigSaveCmdPreservesEgressSettings is the regression noted in
// the model's own doc comment: saving scanner config must not silently drop
// an existing egress_then_write/network_allowlist setting, since
// patchSecurity replaces the whole security: block.
func TestSecurityConfigSaveCmdPreservesEgressSettings(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	// Seed existing contextual-security config the dialog should carry through.
	if err := config.PatchProjectSecurity(config.SecurityPatch{
		EgressThenWrite:  true,
		NetworkAllowList: []string{"api.github.com"},
		DAST:             config.DASTConfig{AllowedTargets: []string{"staging.example.com"}, AllowActive: true},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m := newSecurityConfigModel(80, 24, theme{}, false)
	m.startEdit("gitleaks")
	m.editEnabled = false
	m.applyEdit()

	msg := m.saveCmd()()
	saved, ok := msg.(securityConfigSavedMsg)
	if !ok {
		t.Fatalf("saveCmd() returned %T, want securityConfigSavedMsg", msg)
	}
	if saved.err != nil {
		t.Fatalf("save failed: %v", saved.err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !cfg.Security.EgressThenWrite {
		t.Error("egress_then_write was dropped by saving scanner config")
	}
	if len(cfg.Security.NetworkAllowList) != 1 || cfg.Security.NetworkAllowList[0] != "api.github.com" {
		t.Errorf("network_allowlist = %v, want [api.github.com]", cfg.Security.NetworkAllowList)
	}
	if !cfg.Security.DAST.AllowActive || len(cfg.Security.DAST.AllowedTargets) != 1 || cfg.Security.DAST.AllowedTargets[0] != "staging.example.com" {
		t.Errorf("dast policy was dropped by saving scanner config: %+v", cfg.Security.DAST)
	}
	if cfg.Security.Tools["gitleaks"].ToolEnabled() {
		t.Error("expected gitleaks to be disabled after save")
	}
}
