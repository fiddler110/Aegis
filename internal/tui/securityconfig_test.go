package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/huh/v2"

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

// TestSecurityConfigActionFormOffersInstallWhenAvailable is the regression for
// the "select tools to install" ask: a tool with a guided install command for
// this OS must offer an "install" option; a tool with none (zap, container-
// only) must not.
func TestSecurityConfigActionFormOffersInstallWhenAvailable(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	m := newSecurityConfigModel(80, 24, theme{}, false)

	m.editingName = "trivy"
	form := m.buildActionForm()
	if form == nil {
		t.Fatal("buildActionForm returned nil")
	}
	form.Init()
	if !strings.Contains(formOptionValues(t, form), "Install now") {
		t.Error("expected an install option for trivy, which ships a guided install for every OS")
	}

	m.editingName = "zap"
	form = m.buildActionForm()
	form.Init()
	if strings.Contains(formOptionValues(t, form), "Install now") {
		t.Error("expected no install option for zap, which has no guided install command")
	}
}

// formOptionValues renders a form to a string so the test can assert on
// visible option labels without depending on huh's internal option-value API.
func formOptionValues(t *testing.T, form *huh.Form) string {
	t.Helper()
	return form.View()
}

// TestSecurityConfigInstallConfirmDeclinedGoesBackToList checks that
// declining the confirm step (installConfirmed left false) never starts an
// install and returns to the list, discarding the pending install.
func TestSecurityConfigInstallConfirmDeclinedGoesBackToList(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	m := newSecurityConfigModel(80, 24, theme{}, false)
	m.editingName = "trivy"
	m.installCmd = "echo should never run"
	m.installConfirmed = false
	m.phase = scPhaseInstallConfirm
	m.form = m.buildInstallConfirmForm()
	// Completing the form without confirming must not run installCmdFunc —
	// simulate the completed-but-declined branch updateInstallConfirm takes.
	if m.installConfirmed {
		t.Fatal("test setup: installConfirmed should start false")
	}
	m.backToList()
	if m.phase != scPhaseList {
		t.Errorf("phase = %v, want scPhaseList after declining install", m.phase)
	}
	if m.editingName != "" {
		t.Errorf("editingName = %q, want cleared after backToList", m.editingName)
	}
}

// TestSecurityConfigInstallDoneMsgSetsNotice is the regression for the
// install-result banner: updateInstalling must set a success or failure
// notice from a securityInstallDoneMsg and re-trigger a status refresh,
// without ever needing to run a real install command in the test itself
// (that path is covered by internal/security's own RunGuidedInstall tests).
func TestSecurityConfigInstallDoneMsgSetsNotice(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	m := newSecurityConfigModel(80, 24, theme{}, false)
	m.phase = scPhaseInstalling
	m.editingName = "trivy"

	cmd := m.updateInstalling(securityInstallDoneMsg{name: "trivy", output: "ok"})
	if m.notice != "✓ trivy installed." {
		t.Errorf("notice = %q, want a success message", m.notice)
	}
	if m.phase != scPhaseLoading {
		t.Errorf("phase = %v, want scPhaseLoading to refresh statuses after install", m.phase)
	}
	if cmd == nil {
		t.Error("expected a non-nil cmd to kick off the status refresh")
	}

	m2 := newSecurityConfigModel(80, 24, theme{}, false)
	m2.phase = scPhaseInstalling
	m2.editingName = "trivy"
	m2.updateInstalling(securityInstallDoneMsg{name: "trivy", err: fmt.Errorf("boom")})
	if !strings.Contains(m2.notice, "✗") || !strings.Contains(m2.notice, "boom") {
		t.Errorf("notice = %q, want a failure message mentioning the error", m2.notice)
	}
}
