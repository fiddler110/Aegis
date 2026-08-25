package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

func runHarden(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newHardenCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestHardenAppliesUnsetCaps(t *testing.T) {
	redirectConfigDir(t)

	out, err := runHarden(t, "", "--yes")
	if err != nil {
		t.Fatalf("harden --yes: %v", err)
	}
	// The default sandbox.backend is now "container", which harden already
	// treats as hardened (a specific runtime was pinned) — only security
	// changes on a fresh config.
	if !strings.Contains(out, `already "container"`) || !strings.Contains(out, "false -> true") {
		t.Errorf("expected sandbox already hardened and security changed, got:\n%s", out)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Sandbox.Backend != "container" {
		t.Errorf("sandbox.backend = %q, want container (unchanged, already hardened)", cfg.Sandbox.Backend)
	}
	if !cfg.Security.EgressThenWrite {
		t.Error("security.egress_then_write not set")
	}
	if cfg.Cost.SessionCapUSD != config.HardenSessionCapUSD {
		t.Errorf("cost.session_cap_usd = %v, want %v", cfg.Cost.SessionCapUSD, config.HardenSessionCapUSD)
	}
	if cfg.Cost.DailyCapUSD != config.HardenDailyCapUSD {
		t.Errorf("cost.daily_cap_usd = %v, want %v", cfg.Cost.DailyCapUSD, config.HardenDailyCapUSD)
	}
	if cfg.Cost.SessionTokenCap != config.HardenSessionTokenCap {
		t.Errorf("cost.session_token_cap = %v, want %v", cfg.Cost.SessionTokenCap, config.HardenSessionTokenCap)
	}
	if cfg.Cost.DailyTokenCap != config.HardenDailyTokenCap {
		t.Errorf("cost.daily_token_cap = %v, want %v", cfg.Cost.DailyTokenCap, config.HardenDailyTokenCap)
	}
}

func TestHardenIsIdempotent(t *testing.T) {
	redirectConfigDir(t)

	if _, err := runHarden(t, "", "--yes"); err != nil {
		t.Fatalf("first harden: %v", err)
	}
	out, err := runHarden(t, "", "--yes")
	if err != nil {
		t.Fatalf("second harden: %v", err)
	}
	if !strings.Contains(out, "unchanged") {
		t.Errorf("expected second run to report no changes, got:\n%s", out)
	}
	if strings.Contains(out, "->") {
		t.Errorf("second run should not report any transitions, got:\n%s", out)
	}
}

func TestHardenPreservesCustomCaps(t *testing.T) {
	redirectConfigDir(t)

	if err := config.PatchGlobalCost(config.CostPatch{SessionCapUSD: 1.5}); err != nil {
		t.Fatalf("pre-seed cost config: %v", err)
	}

	if _, err := runHarden(t, "", "--yes"); err != nil {
		t.Fatalf("harden: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Cost.SessionCapUSD != 1.5 {
		t.Errorf("cost.session_cap_usd = %v, want preserved 1.5", cfg.Cost.SessionCapUSD)
	}
	// Untouched caps still get filled in.
	if cfg.Cost.DailyCapUSD != config.HardenDailyCapUSD {
		t.Errorf("cost.daily_cap_usd = %v, want %v", cfg.Cost.DailyCapUSD, config.HardenDailyCapUSD)
	}
}

func TestHardenAbortsWithoutConfirmation(t *testing.T) {
	redirectConfigDir(t)

	if _, err := runHarden(t, "n\n"); err != nil {
		t.Fatalf("harden (declined): %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// sandbox.backend defaults to "container" already, so it's not a useful
	// signal here (harden wouldn't change it either way) — check the other
	// axis harden touches on a fresh config instead.
	if cfg.Security.EgressThenWrite {
		t.Error("security.egress_then_write changed despite declined confirmation")
	}
}

func TestHardenRespectsContainerBackend(t *testing.T) {
	redirectConfigDir(t)

	if err := config.PatchGlobalSandbox(config.SandboxPatch{Backend: "container", Runtime: "docker"}); err != nil {
		t.Fatalf("pre-seed sandbox config: %v", err)
	}
	out, err := runHarden(t, "", "--yes")
	if err != nil {
		t.Fatalf("harden: %v", err)
	}
	if !strings.Contains(out, `already "container"`) {
		t.Errorf("expected container backend to be left alone, got:\n%s", out)
	}
	cfg, _ := config.Load()
	if cfg.Sandbox.Backend != "container" || cfg.Sandbox.Runtime != "docker" {
		t.Errorf("sandbox = %+v, want unchanged container/docker", cfg.Sandbox)
	}
}
