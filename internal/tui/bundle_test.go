package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

func writeTestBundleFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeTestBundleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestBundleFile(t, filepath.Join(dir, "bundle.yaml"), "name: test-pack\nversion: 1.0.0\n")
	writeTestBundleFile(t, filepath.Join(dir, "commands", "hello.md"), "Say hello")
	return dir
}

// TestCmdBundleBareArgsIsUsageError checks the no-subcommand fast path
// returns a usage error without touching the filesystem.
func TestCmdBundleBareArgsIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle"})
	if !res.IsError {
		t.Fatal("expected an error result for bare `/bundle`")
	}
}

// TestCmdBundleUnknownSubcommand checks an unrecognized subcommand is
// rejected before touching the filesystem.
func TestCmdBundleUnknownSubcommand(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle", Args: []string{"bogus"}})
	if !res.IsError {
		t.Fatal("expected an error result for an unknown /bundle subcommand")
	}
}

// TestCmdBundleInfoMissingPathIsUsageError checks `/bundle info` with no path
// is rejected before touching the filesystem.
func TestCmdBundleInfoMissingPathIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle", Args: []string{"info"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/bundle info` with no path")
	}
}

// TestCmdBundleInfoShowsContentHash mirrors TestBundleInfoPrintsContentHash
// (internal/cli/bundle_test.go) for the TUI's /bundle info.
func TestCmdBundleInfoShowsContentHash(t *testing.T) {
	dir := makeTestBundleDir(t)
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle", Args: []string{"info", dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "content hash: sha256:") {
		t.Errorf("expected content hash in output, got:\n%s", res.Output)
	}
}

// TestCmdBundleInstallPreviewDoesNotWrite checks that omitting the trailing
// "confirm" token only previews the install — no artifacts are written to
// disk, mirroring /security install's confirm-gating behavior.
func TestCmdBundleInstallPreviewDoesNotWrite(t *testing.T) {
	dir := makeTestBundleDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle", Args: []string{"install", dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "confirm") {
		t.Errorf("expected a preview instructing to add 'confirm', got:\n%s", res.Output)
	}
	if _, err := os.Stat(filepath.Join(".aegis", "commands", "hello.md")); err == nil {
		t.Fatal("expected no artifact written without 'confirm'")
	}
}

// TestCmdBundleInstallConfirmWrites checks that the trailing "confirm" token
// actually installs the bundle's artifacts into the project scope.
func TestCmdBundleInstallConfirmWrites(t *testing.T) {
	dir := makeTestBundleDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle", Args: []string{"install", dir, "confirm"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if _, err := os.Stat(filepath.Join(".aegis", "commands", "hello.md")); err != nil {
		t.Errorf("expected artifact installed: %v", err)
	}
}

// TestCmdBundleInstallHashMismatchAborts is the P7.6 regression for the TUI
// surface: a pinned sha256 that doesn't match the bundle's actual content
// must abort before anything is written, even with "confirm".
func TestCmdBundleInstallHashMismatchAborts(t *testing.T) {
	dir := makeTestBundleDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle", Args: []string{
		"install", dir, "sha256:0000000000000000000000000000000000000000000000000000000000000000", "confirm",
	}})
	if !res.IsError {
		t.Fatalf("expected hash mismatch to error, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "hash mismatch") {
		t.Errorf("expected hash mismatch error, got: %s", res.Output)
	}
	if _, err := os.Stat(filepath.Join(".aegis", "commands", "hello.md")); err == nil {
		t.Fatal("expected no artifact written on hash mismatch")
	}
}

// TestCmdBundleInstallGlobalScope checks the "global" keyword installs into
// the user data dir instead of the project's .aegis/.
func TestCmdBundleInstallGlobalScope(t *testing.T) {
	dir := makeTestBundleDir(t)
	redirectConfigDir(t)
	chdirTempTUI(t)

	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bundle", Args: []string{"install", dir, "global", "confirm"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if _, err := os.Stat(filepath.Join(".aegis", "commands", "hello.md")); err == nil {
		t.Fatal("expected project scope NOT to receive the artifact when 'global' is given")
	}
}
