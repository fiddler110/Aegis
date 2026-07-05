package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdSecurityDefaultsToStatus is the P14.2 regression for the bare-arg
// case: /security with no subcommand shows scanner status, same as
// `aegis security status`.
func TestCmdSecurityDefaultsToStatus(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "TOOL") || !strings.Contains(res.Output, "METHOD") {
		t.Errorf("expected a status table, got: %s", res.Output)
	}
}

// TestCmdSecurityStatusExplicit checks the explicit "status" subcommand
// produces the same table as the bare-arg case.
func TestCmdSecurityStatusExplicit(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"status"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "TOOL") {
		t.Errorf("expected a status table, got: %s", res.Output)
	}
}

// TestCmdSecurityBaselineEmpty verifies the no-baseline-file case reports a
// clear "no entries" message rather than an error.
func TestCmdSecurityBaselineEmpty(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"baseline"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "no baseline entries") {
		t.Errorf("expected a no-entries message, got: %s", res.Output)
	}
}

// TestCmdSecurityBaselineWithEntries proves a real baseline file's
// suppression entries render with their computed status, mirroring
// `aegis security baseline`.
func TestCmdSecurityBaselineWithEntries(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wd, ".aegis"), 0o755); err != nil {
		t.Fatal(err)
	}
	baseline := "suppressions:\n  - rule_id: CWE-798\n    reason: accepted risk, test fixture\n    expires: 2099-01-01\n"
	if err := os.WriteFile(filepath.Join(wd, ".aegis", "security-baseline.yaml"), []byte(baseline), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"baseline"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "CWE-798") {
		t.Errorf("expected the suppression entry's rule_id, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "active") {
		t.Errorf("expected an 'active' status for a non-expired entry, got: %s", res.Output)
	}
}

// TestCmdSecurityConfigSubcommandDelegates proves "/security config [global]"
// opens the same dialog as the standalone /security-config command, rather
// than duplicating its logic.
func TestCmdSecurityConfigSubcommandDelegates(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"config"}})
	if res.SecurityConfigGlobal == nil || *res.SecurityConfigGlobal {
		t.Fatalf("expected project scope (false) with no args, got %+v", res.SecurityConfigGlobal)
	}

	res = d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"config", "global"}})
	if res.SecurityConfigGlobal == nil || !*res.SecurityConfigGlobal {
		t.Fatalf("expected global scope (true), got %+v", res.SecurityConfigGlobal)
	}
}

// TestCmdSecurityInstallUnknownTool mirrors the CLI's
// TestSecurityInstallUnknownTool.
func TestCmdSecurityInstallUnknownTool(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"install", "not-a-real-scanner"}})
	if !res.IsError {
		t.Fatal("expected an error for an unknown scanner name")
	}
	if !strings.Contains(res.Output, "Unknown scanner") {
		t.Errorf("output = %q, want mention of unknown scanner", res.Output)
	}
}

// TestCmdSecurityInstallRequiresExplicitConfirm is the approval-gate
// regression (P11.10's posture, adapted to the slash-command shape): without
// the trailing "confirm" word, the command must only preview the exact host
// command it would run, never execute it.
func TestCmdSecurityInstallRequiresExplicitConfirm(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"install", "gitleaks"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "This will run the following command") {
		t.Errorf("expected the exact command to be previewed, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "confirm") {
		t.Errorf("expected instructions to re-run with 'confirm', got: %s", res.Output)
	}
}

// TestCmdSecurityUnknownSubcommand checks the umbrella's fallback error for
// an unrecognized subcommand.
func TestCmdSecurityUnknownSubcommand(t *testing.T) {
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "security", Args: []string{"bogus"}})
	if !res.IsError {
		t.Fatal("expected an error for an unknown /security subcommand")
	}
	if !strings.Contains(res.Output, "Unknown /security subcommand") {
		t.Errorf("output = %q, want mention of unknown subcommand", res.Output)
	}
}
