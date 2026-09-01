package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// redirectTrustStore isolates config.WorkspaceTrustStorePath to a fresh temp
// directory for the duration of the test (same technique
// internal/config's redirectConfigDir uses), so a real trust grant on the
// machine running the test can neither leak in nor be polluted by it.
func redirectTrustStore(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
}

// TestBuildGateDefaultsStrictPlanModeByWorkspaceTrust is P81.20/FIND-20's
// posture change, driven through the real (*Server).buildGate/cronPermCheck
// path rather than exercised only at the config.PlanModeShellReadsEnabled
// unit level. An operator who has expressed no
// permission.plan_mode_shell_reads preference gets the classifier's CapRead
// downgrade in plan mode only for a workspace with a current trust grant;
// an unreviewed workspace gets the strict posture, where the plan-mode
// guarantee does not depend on classifyShellCommand's correctness.
func TestBuildGateDefaultsStrictPlanModeByWorkspaceTrust(t *testing.T) {
	redirectTrustStore(t)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Permission.Mode: plan, PlanModeShellReads left nil — no operator
	// preference, so the trust-based default is what decides this.
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, reg)

	readCmd := "cat " + filepath.Join(root, "notes.txt")
	job := cron.Job{Command: readCmd, Workdir: root}

	if allowed, reason := srv.cronPermCheck(context.Background(), job); allowed {
		t.Fatalf("untrusted workspace: cronPermCheck(%q) = allowed, want denied (strict-by-default); reason=%q", readCmd, reason)
	}

	if err := config.TrustWorkspace(root); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}
	if !config.WorkspaceTrusted(root) {
		t.Fatal("workspace should be trusted immediately after the grant")
	}

	if allowed, reason := srv.cronPermCheck(context.Background(), job); !allowed {
		t.Fatalf("trusted workspace: cronPermCheck(%q) = denied (reason=%q), want allowed — the pre-P81.20 convenience default should still apply once the workspace is trusted", readCmd, reason)
	}
}
