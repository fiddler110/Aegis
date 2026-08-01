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

// TestTrustDirRecordsTrustForANamedDirectory covers `aegis trust --dir`, the
// P52.13 surface that authorizes a workspace.additional_roots entry. The
// no-flag path short-circuits on "nothing security-relevant to freeze", which
// would make an additional root — a research repo with no .aegis/config.yaml
// at all — impossible to record a decision for.
func TestTrustDirRecordsTrustForANamedDirectory(t *testing.T) {
	redirectConfigDir(t)
	chdirTempCLI(t)
	target := t.TempDir()

	store := workspacetrust.Open(config.WorkspaceTrustStorePath())
	if store.IsTrusted(target) {
		t.Fatal("fresh directory should not be trusted")
	}

	out, err := runTrust(t, "", "--dir", target, "--yes")
	if err != nil {
		t.Fatalf("trust --dir: %v (%s)", err, out)
	}
	if !workspacetrust.Open(config.WorkspaceTrustStorePath()).IsTrusted(target) {
		t.Fatalf("directory not trusted after `trust --dir`: %s", out)
	}

	// ...and --revoke takes it back.
	if _, err := runTrust(t, "", "--dir", target, "--revoke"); err != nil {
		t.Fatalf("trust --dir --revoke: %v", err)
	}
	if workspacetrust.Open(config.WorkspaceTrustStorePath()).IsTrusted(target) {
		t.Error("directory still trusted after --revoke")
	}
}

// TestTrustDirDeclinedDoesNotTrust keeps the confirmation prompt meaningful on
// the --dir path too.
func TestTrustDirDeclinedDoesNotTrust(t *testing.T) {
	redirectConfigDir(t)
	chdirTempCLI(t)
	target := t.TempDir()

	if _, err := runTrust(t, "n\n", "--dir", target); err != nil {
		t.Fatalf("trust --dir: %v", err)
	}
	if workspacetrust.Open(config.WorkspaceTrustStorePath()).IsTrusted(target) {
		t.Error("declining the prompt still trusted the directory")
	}
}

// TestTrustDirRejectsNonDirectory keeps a typo'd root from being recorded as a
// trusted path that will only ever be dropped at load time.
func TestTrustDirRejectsNonDirectory(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTempCLI(t)

	if _, err := runTrust(t, "", "--dir", dir+"/does-not-exist", "--yes"); err == nil {
		t.Error("trusting a nonexistent directory succeeded")
	}
}
