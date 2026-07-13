package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/workspacetrust"
)

func runTrust(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newTrustCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// chdirTempCLI changes to a fresh temp dir for the test's duration. Named
// distinctly from other packages' chdirTemp helpers since this one lives in
// package cli.
func chdirTempCLI(t *testing.T) string {
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

func TestTrustStatusShowsFrozenChangesWithoutPrompting(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTempCLI(t)

	if err := os.MkdirAll(dir+"/.aegis", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/.aegis/config.yaml", []byte("permission:\n  mode: auto\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runTrust(t, "", "--status")
	if err != nil {
		t.Fatalf("trust --status: %v", err)
	}
	if !strings.Contains(out, "permission") {
		t.Errorf("expected the permission diff in output, got:\n%s", out)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.WorkspaceTrust.Trusted {
		t.Error("--status must not itself trust the directory")
	}
}

func TestTrustYesGrantsTrust(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTempCLI(t)

	if err := os.MkdirAll(dir+"/.aegis", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/.aegis/config.yaml", []byte("permission:\n  mode: auto\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runTrust(t, "", "--yes"); err != nil {
		t.Fatalf("trust --yes: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !cfg.WorkspaceTrust.Trusted {
		t.Error("directory should be trusted after `aegis trust --yes`")
	}
	if cfg.Permission.Mode != "auto" {
		t.Errorf("permission.mode = %q, want auto now that the workspace is trusted", cfg.Permission.Mode)
	}
}

func TestTrustRevoke(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTempCLI(t)

	if err := workspacetrust.Open(config.WorkspaceTrustStorePath()).Trust(dir); err != nil {
		t.Fatalf("seed trust: %v", err)
	}
	if _, err := runTrust(t, "", "--revoke"); err != nil {
		t.Fatalf("trust --revoke: %v", err)
	}
	if workspacetrust.Open(config.WorkspaceTrustStorePath()).IsTrusted(dir) {
		t.Error("directory should no longer be trusted after --revoke")
	}
}

func TestTrustDeclinedConfirmationDoesNotTrust(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTempCLI(t)

	if err := os.MkdirAll(dir+"/.aegis", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/.aegis/config.yaml", []byte("permission:\n  mode: auto\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runTrust(t, "n\n"); err != nil {
		t.Fatalf("trust (declined): %v", err)
	}
	if workspacetrust.Open(config.WorkspaceTrustStorePath()).IsTrusted(dir) {
		t.Error("directory should not be trusted after declining the confirmation")
	}
}
