package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// redirectConfigDir points config.GlobalConfigPath() at a fresh temp dir so
// these tests never read or write the developer's real global config file
// (same convention as internal/config/config_test.go, internal/cli's own
// redirectConfigDir, and internal/tui/securityconfig_test.go).
func redirectConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
}

// chdirTemp changes the process's working directory to a fresh temp dir for
// the duration of the test and restores it on cleanup. Needed for any test
// that exercises "project" scope: config.ProjectConfigPath() resolves
// ".aegis/config.yaml" relative to os.Getwd(), not relative to a Server's
// workspace field (see resolveScope's doc comment in config.go) — without
// this, a project-scope write in a test would land in this package's real
// source directory.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
}

// newConfigTestServer builds a daemon for the config-mutation endpoint
// tests. If workspace is empty, a fresh temp dir is used (mirroring
// newScanTestServer in scan_test.go); pass a specific dir when a test needs
// s.workspace to equal the process cwd (the default-scope-detection tests).
func newConfigTestServer(t *testing.T, workspace string) *client.Client {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	if workspace == "" {
		workspace = t.TempDir()
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.workspace = workspace

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token")
}

// newConfigTestServerWithSandbox is newConfigTestServer plus a pre-set
// active sandbox backend + fallback state (P25.2), so tests can assert that
// GET /config/sandbox reports what SelectSandbox actually picked at startup
// rather than echoing the configured values back unchecked.
func newConfigTestServerWithSandbox(t *testing.T, workspace string, sb sandbox.Backend, fallback bool, reason string) *client.Client {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	if workspace == "" {
		workspace = t.TempDir()
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.workspace = workspace
	srv.sandbox = sb
	srv.sandboxFallback = fallback
	srv.sandboxFallbackReason = reason

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token")
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// ─── /config/sandbox ────────────────────────────────────────────────────────

func TestConfigSandboxGetReturnsDefaults(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.GetConfigSandbox(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetConfigSandbox: %v", err)
	}
	if resp.Backend != "container" {
		t.Errorf("Backend = %q, want default %q", resp.Backend, "container")
	}
	if resp.Scope != "global" {
		t.Errorf("Scope = %q, want %q", resp.Scope, "global")
	}
}

func TestConfigSandboxPatchGlobalAppliesAndPersists(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{
		Scope:   "global",
		Backend: strPtr("auto"),
		Image:   strPtr("ubuntu:24.04"),
	})
	if err != nil {
		t.Fatalf("PatchConfigSandbox: %v", err)
	}
	if resp.Backend != "auto" || resp.Image != "ubuntu:24.04" {
		t.Errorf("response = %+v, want backend=auto image=ubuntu:24.04", resp)
	}

	// Persisted to disk.
	data, err := os.ReadFile(config.GlobalConfigPath())
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(data), "backend: auto") {
		t.Errorf("global config missing backend: auto:\n%s", data)
	}

	// A follow-up GET reflects the write without a daemon restart.
	got, err := cl.GetConfigSandbox(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetConfigSandbox after patch: %v", err)
	}
	if got.Backend != "auto" || got.Image != "ubuntu:24.04" {
		t.Errorf("GET after PATCH = %+v, want backend=auto image=ubuntu:24.04", got)
	}
}

// TestConfigSandboxGetReportsActiveBackendAndFallback is the P25.2
// regression test: the configured backend/runtime (what an operator wrote)
// and the active backend (what SelectSandbox actually picked, and whether
// it fell back to local) must both be visible, and can legitimately differ.
func TestConfigSandboxGetReportsActiveBackendAndFallback(t *testing.T) {
	redirectConfigDir(t)
	const wantReason = "configured sandbox backend \"container\" unavailable (no runtime) — running unsandboxed on the host"
	cl := newConfigTestServerWithSandbox(t, "", sandbox.NewLocalBackendWithEnv(nil), true, wantReason)

	// The configured value on disk claims "container", but the daemon
	// actually fell back to local at startup (simulated above) — a client
	// trusting only Backend/Runtime would wrongly believe exec is sandboxed.
	if _, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{
		Scope:   "global",
		Backend: strPtr("container"),
		Runtime: strPtr("docker"),
	}); err != nil {
		t.Fatalf("PatchConfigSandbox: %v", err)
	}

	resp, err := cl.GetConfigSandbox(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetConfigSandbox: %v", err)
	}
	if resp.Backend != "container" || resp.Runtime != "docker" {
		t.Errorf("configured Backend/Runtime = %q/%q, want container/docker", resp.Backend, resp.Runtime)
	}
	if resp.ActiveBackend != "local" {
		t.Errorf("ActiveBackend = %q, want %q (the real running backend)", resp.ActiveBackend, "local")
	}
	if !resp.Fallback {
		t.Error("Fallback = false, want true — configured container backend is not what's actually running")
	}
	if resp.FallbackReason != wantReason {
		t.Errorf("FallbackReason = %q, want %q", resp.FallbackReason, wantReason)
	}
}

// TestConfigSandboxGetNoFallbackWhenActiveMatchesConfigured covers the
// non-degraded case: when SelectSandbox picked exactly what was configured,
// Fallback must be false and ActiveBackend must reflect the real backend.
func TestConfigSandboxGetNoFallbackWhenActiveMatchesConfigured(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServerWithSandbox(t, "", sandbox.NewLocalBackendWithEnv(nil), false, "")

	resp, err := cl.GetConfigSandbox(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetConfigSandbox: %v", err)
	}
	if resp.ActiveBackend != "local" {
		t.Errorf("ActiveBackend = %q, want %q", resp.ActiveBackend, "local")
	}
	if resp.Fallback {
		t.Error("Fallback = true, want false")
	}
	if resp.FallbackReason != "" {
		t.Errorf("FallbackReason = %q, want empty", resp.FallbackReason)
	}
}

// TestConfigSandboxPatchRejectsUnknownBackend covers P25.2's PATCH-side
// validation: writing an unrecognized backend must fail with 400, not be
// silently persisted to disk.
func TestConfigSandboxPatchRejectsUnknownBackend(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	_, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{
		Scope:   "global",
		Backend: strPtr("kubernetes"),
	})
	if err == nil {
		t.Fatal("expected an error for an unknown sandbox backend")
	}
}

// TestConfigSandboxPatchAliasesRuntimeName covers P25.2's PATCH-side
// aliasing: PATCHing backend="podman" directly (the mistake the docs'
// runtime-name phrasing invites) must be written as backend=container,
// runtime=podman, not literally as "podman".
func TestConfigSandboxPatchAliasesRuntimeName(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{
		Scope:   "global",
		Backend: strPtr("podman"),
	})
	if err != nil {
		t.Fatalf("PatchConfigSandbox: %v", err)
	}
	if resp.Backend != "container" || resp.Runtime != "podman" {
		t.Errorf("response = backend=%q runtime=%q, want container/podman", resp.Backend, resp.Runtime)
	}

	data, err := os.ReadFile(config.GlobalConfigPath())
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if strings.Contains(string(data), "backend: podman") {
		t.Errorf("global config wrote the unaliased backend name:\n%s", data)
	}
	if !strings.Contains(string(data), "backend: container") || !strings.Contains(string(data), "runtime: podman") {
		t.Errorf("global config missing aliased backend: container / runtime: podman:\n%s", data)
	}
}

func TestConfigSandboxPatchIsPartial(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")
	ctx := context.Background()

	if _, err := cl.PatchConfigSandbox(ctx, api.ConfigSandboxPatchRequest{
		Scope:   "global",
		Backend: strPtr("container"),
		Runtime: strPtr("docker"),
	}); err != nil {
		t.Fatalf("first patch: %v", err)
	}
	// Second patch only touches Network; Backend/Runtime must survive.
	resp, err := cl.PatchConfigSandbox(ctx, api.ConfigSandboxPatchRequest{
		Scope:   "global",
		Network: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("second patch: %v", err)
	}
	if resp.Backend != "container" || resp.Runtime != "docker" {
		t.Errorf("partial patch clobbered untouched fields: %+v", resp)
	}
	if !resp.Network {
		t.Error("Network not applied")
	}
}

func TestConfigSandboxPatchProjectScopeWritesProjectFile(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTemp(t)
	cl := newConfigTestServer(t, dir)

	if _, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{
		Scope:   "project",
		Backend: strPtr("container"),
		Runtime: strPtr("docker"),
	}); err != nil {
		t.Fatalf("PatchConfigSandbox: %v", err)
	}

	data, err := os.ReadFile(config.ProjectConfigPath())
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if !strings.Contains(string(data), "runtime: docker") {
		t.Errorf("project config missing runtime: docker:\n%s", data)
	}
	if _, err := os.Stat(config.GlobalConfigPath()); !os.IsNotExist(err) {
		t.Errorf("expected no global config to be written, stat err = %v", err)
	}
}

func TestConfigSandboxRejectsUnknownScope(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")
	_, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{Scope: "bogus"})
	if err == nil {
		t.Fatal("expected an error for an unknown scope")
	}
}

func TestConfigDefaultScopeIsProjectWhenAegisDirExists(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".aegis"), 0o755); err != nil {
		t.Fatal(err)
	}
	cl := newConfigTestServer(t, dir) // s.workspace == cwd, matching real `aegis serve` usage

	resp, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{Backend: strPtr("auto")})
	if err != nil {
		t.Fatalf("PatchConfigSandbox: %v", err)
	}
	if resp.Scope != "project" {
		t.Errorf("Scope = %q, want %q (workspace has a .aegis/ dir)", resp.Scope, "project")
	}
	if _, err := os.Stat(config.ProjectConfigPath()); err != nil {
		t.Errorf("expected project config to be written: %v", err)
	}
	if _, err := os.Stat(config.GlobalConfigPath()); !os.IsNotExist(err) {
		t.Errorf("expected no global config to be written, stat err = %v", err)
	}
}

func TestConfigDefaultScopeIsGlobalWithoutAegisDir(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTemp(t) // no .aegis/ subdir created
	cl := newConfigTestServer(t, dir)

	resp, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{Backend: strPtr("auto")})
	if err != nil {
		t.Fatalf("PatchConfigSandbox: %v", err)
	}
	if resp.Scope != "global" {
		t.Errorf("Scope = %q, want %q (no .aegis/ dir in workspace)", resp.Scope, "global")
	}
}

// ─── /config/security ───────────────────────────────────────────────────────

func TestConfigSecurityGetReturnsDefaults(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.GetConfigSecurity(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetConfigSecurity: %v", err)
	}
	if resp.EgressThenWrite {
		t.Error("EgressThenWrite should default false")
	}
	if resp.DefaultMethod != "auto" {
		t.Errorf("DefaultMethod = %q, want auto", resp.DefaultMethod)
	}
}

func TestConfigSecurityPatchAppliesAndPersists(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")
	ctx := context.Background()

	resp, err := cl.PatchConfigSecurity(ctx, api.ConfigSecurityPatchRequest{
		Scope:           "global",
		EgressThenWrite: boolPtr(true),
		Tools: map[string]api.SecurityToolConfigWire{
			"gitleaks": {Enabled: false, Method: "host"},
		},
	})
	if err != nil {
		t.Fatalf("PatchConfigSecurity: %v", err)
	}
	if !resp.EgressThenWrite {
		t.Error("EgressThenWrite not applied")
	}
	tc, ok := resp.Tools["gitleaks"]
	if !ok || tc.Enabled || tc.Method != "host" {
		t.Errorf("Tools[gitleaks] = %+v, ok=%v, want disabled/host", tc, ok)
	}

	got, err := cl.GetConfigSecurity(ctx, "global")
	if err != nil {
		t.Fatalf("GetConfigSecurity after patch: %v", err)
	}
	if !got.EgressThenWrite {
		t.Error("EgressThenWrite not persisted")
	}
	if tc := got.Tools["gitleaks"]; tc.Enabled || tc.Method != "host" {
		t.Errorf("persisted Tools[gitleaks] = %+v, want disabled/host", tc)
	}
}

func TestConfigSecurityPatchIsPartial(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")
	ctx := context.Background()

	if _, err := cl.PatchConfigSecurity(ctx, api.ConfigSecurityPatchRequest{
		Scope:         "global",
		DefaultMethod: strPtr("host"),
	}); err != nil {
		t.Fatalf("first patch: %v", err)
	}
	resp, err := cl.PatchConfigSecurity(ctx, api.ConfigSecurityPatchRequest{
		Scope:           "global",
		EgressThenWrite: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("second patch: %v", err)
	}
	if resp.DefaultMethod != "host" {
		t.Errorf("partial patch clobbered DefaultMethod: %+v", resp)
	}
	if !resp.EgressThenWrite {
		t.Error("EgressThenWrite not applied")
	}
}

// ─── /config/skills ──────────────────────────────────────────────────────────

func TestConfigSkillsGetAndPatchRoundTrip(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")
	ctx := context.Background()

	empty, err := cl.GetConfigSkills(ctx, "global")
	if err != nil {
		t.Fatalf("GetConfigSkills: %v", err)
	}
	if len(empty.BuiltinEnabled) != 0 {
		t.Errorf("BuiltinEnabled = %v, want empty by default", empty.BuiltinEnabled)
	}
	// The catalog of embedded built-ins is always present (P15.7), even when
	// nothing is enabled, and each entry carries a description.
	if len(empty.Available) == 0 {
		t.Fatal("Available is empty, want the embedded built-in skill catalog")
	}
	foundAudit := false
	for _, b := range empty.Available {
		if b.Name == "security-audit" {
			foundAudit = true
			if b.Description == "" {
				t.Error("security-audit built-in has no description")
			}
		}
	}
	if !foundAudit {
		t.Errorf("Available = %v, want it to include security-audit", empty.Available)
	}

	resp, err := cl.PatchConfigSkills(ctx, "global", []string{"security-audit", "threat-modeling"})
	if err != nil {
		t.Fatalf("PatchConfigSkills: %v", err)
	}
	if len(resp.BuiltinEnabled) != 2 {
		t.Errorf("BuiltinEnabled = %v, want 2 entries", resp.BuiltinEnabled)
	}
	if len(resp.Available) != len(empty.Available) {
		t.Errorf("PATCH Available has %d entries, GET had %d — should be the same catalog",
			len(resp.Available), len(empty.Available))
	}

	got, err := cl.GetConfigSkills(ctx, "global")
	if err != nil {
		t.Fatalf("GetConfigSkills after patch: %v", err)
	}
	if len(got.BuiltinEnabled) != 2 || got.BuiltinEnabled[0] != "security-audit" {
		t.Errorf("persisted BuiltinEnabled = %v", got.BuiltinEnabled)
	}
}

// ─── POST /config/harden ─────────────────────────────────────────────────────

func TestConfigHardenPreviewDoesNotWrite(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.ConfigHarden(context.Background(), api.ConfigHardenRequest{Scope: "global", Confirm: false})
	if err != nil {
		t.Fatalf("ConfigHarden preview: %v", err)
	}
	if resp.Applied {
		t.Error("Applied should be false without Confirm")
	}
	// The default sandbox.backend is now "container", which harden already
	// treats as hardened (a specific runtime was pinned), so a fresh config
	// has nothing to change here — only cost/security move.
	if resp.SandboxChanged || resp.SandboxBackend != "container" {
		t.Errorf("expected sandbox.backend to stay container (already hardened), got %+v", resp)
	}
	if _, err := os.Stat(config.GlobalConfigPath()); !os.IsNotExist(err) {
		t.Errorf("preview must not write a config file, stat err = %v", err)
	}
}

func TestConfigHardenAppliesUnsetCaps(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")
	ctx := context.Background()

	resp, err := cl.ConfigHarden(ctx, api.ConfigHardenRequest{Scope: "global", Confirm: true})
	if err != nil {
		t.Fatalf("ConfigHarden: %v", err)
	}
	if !resp.Applied {
		t.Error("Applied should be true with Confirm")
	}
	// See TestConfigHardenPreviewDoesNotWrite: the default is already
	// "container", which harden treats as already hardened.
	if resp.SandboxChanged || resp.SandboxBackend != "container" {
		t.Errorf("expected sandbox.backend to stay container (already hardened), got %+v", resp)
	}
	if !resp.SecurityChanged {
		t.Error("expected security.egress_then_write to change")
	}
	if len(resp.CostChanges) == 0 {
		t.Error("expected cost caps to be filled in on a fresh config")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Sandbox.Backend != "container" {
		t.Errorf("cfg.Sandbox.Backend = %q, want container (unchanged, already hardened)", cfg.Sandbox.Backend)
	}
	if !cfg.Security.EgressThenWrite {
		t.Error("cfg.Security.EgressThenWrite not persisted")
	}
	if cfg.Cost.SessionCapUSD != config.HardenSessionCapUSD {
		t.Errorf("cfg.Cost.SessionCapUSD = %v, want %v", cfg.Cost.SessionCapUSD, config.HardenSessionCapUSD)
	}
}

func TestConfigHardenIsIdempotent(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")
	ctx := context.Background()

	if _, err := cl.ConfigHarden(ctx, api.ConfigHardenRequest{Scope: "global", Confirm: true}); err != nil {
		t.Fatalf("first harden: %v", err)
	}
	resp, err := cl.ConfigHarden(ctx, api.ConfigHardenRequest{Scope: "global", Confirm: true})
	if err != nil {
		t.Fatalf("second harden: %v", err)
	}
	if resp.SandboxChanged || resp.SecurityChanged || len(resp.CostChanges) != 0 {
		t.Errorf("second run should report no changes, got %+v", resp)
	}
}

// ─── /security/status, /security/baseline, /security/install ───────────────

// TestSecurityStatusListsKnownScanners is a smoke test: it doesn't assert any
// particular tool is installed (CI/dev boxes vary), only that every built-in
// scanner is reported with a non-empty status string — mirroring how
// scan_test.go treats "no scanner binaries installed" as an expected,
// non-error outcome.
func TestSecurityStatusListsKnownScanners(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.SecurityStatus(context.Background())
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if len(resp.Tools) == 0 {
		t.Fatal("expected at least one scanner in the status response")
	}
	found := false
	for _, ts := range resp.Tools {
		if ts.Name == "" || ts.Status == "" {
			t.Errorf("tool status missing name/status: %+v", ts)
		}
		if ts.Name == "gitleaks" {
			found = true
		}
	}
	if !found {
		t.Error("expected gitleaks to be among the reported scanners")
	}
}

// TestSecurityBaselineEmptyByDefault checks the no-baseline-file case
// (the common one) returns an empty list rather than an error.
func TestSecurityBaselineEmptyByDefault(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.SecurityBaseline(context.Background())
	if err != nil {
		t.Fatalf("SecurityBaseline: %v", err)
	}
	if len(resp.Suppressions) != 0 {
		t.Errorf("Suppressions = %v, want empty with no baseline file", resp.Suppressions)
	}
	if resp.Path == "" {
		t.Error("expected a baseline path even when the file doesn't exist")
	}
}

// TestSecurityInstallRejectsUnknownTool checks the tool name is validated
// against the scanner registry before anything else.
func TestSecurityInstallRejectsUnknownTool(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	_, err := cl.SecurityInstall(context.Background(), api.SecurityInstallRequest{Tool: "not-a-real-scanner"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized tool name")
	}
}

// TestSecurityInstallWithoutConfirmNeverRuns checks a known tool with
// Confirm false always reports Ran=false — this must hold regardless of
// whether this OS actually has a guided install command for the tool, since
// the whole point of the gate is "never execute without explicit
// confirmation."
func TestSecurityInstallWithoutConfirmNeverRuns(t *testing.T) {
	redirectConfigDir(t)
	cl := newConfigTestServer(t, "")

	resp, err := cl.SecurityInstall(context.Background(), api.SecurityInstallRequest{Tool: "gitleaks", Confirm: false})
	if err != nil {
		t.Fatalf("SecurityInstall: %v", err)
	}
	if resp.Ran {
		t.Error("Ran should be false when Confirm is false")
	}
	if resp.Error == "" {
		t.Error("expected an explanatory Error (either no guided install, or the confirm hint)")
	}
}
