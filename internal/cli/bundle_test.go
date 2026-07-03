package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBundleFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeTestBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeBundleFile(t, filepath.Join(dir, "bundle.yaml"), "name: test-pack\nversion: 1.0.0\n")
	writeBundleFile(t, filepath.Join(dir, "commands", "hello.md"), "Say hello")
	return dir
}

func runBundle(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newBundleCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestBundleInfoPrintsContentHash(t *testing.T) {
	dir := makeTestBundle(t)
	out, err := runBundle(t, "info", dir)
	if err != nil {
		t.Fatalf("bundle info: %v\n%s", err, out)
	}
	if !strings.Contains(out, "content hash: sha256:") {
		t.Errorf("expected content hash in output, got:\n%s", out)
	}
}

// TestBundleInstallExpectSHA256Mismatch is the P7.6 regression: a pinned
// --expect-sha256 that doesn't match the bundle's actual content must abort
// the install before anything is written, so a compromised/altered upstream
// git repo doesn't silently install different code than what was reviewed.
func TestBundleInstallExpectSHA256Mismatch(t *testing.T) {
	dir := makeTestBundle(t)
	scope := t.TempDir()

	out, err := runBundle(t, "install", dir, "--scope", "user", "--expect-sha256", "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	_ = scope
	if err == nil {
		t.Fatalf("expected error on hash mismatch, output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("expected hash mismatch error, got: %v", err)
	}
}

// TestBundleInstallExpectSHA256Match verifies a correctly pinned hash (bare
// hex, without the "sha256:" prefix) allows the install to proceed.
func TestBundleInstallExpectSHA256Match(t *testing.T) {
	dir := makeTestBundle(t)

	infoOut, err := runBundle(t, "info", dir)
	if err != nil {
		t.Fatalf("bundle info: %v", err)
	}
	idx := strings.Index(infoOut, "content hash: sha256:")
	if idx < 0 {
		t.Fatalf("could not find content hash in info output:\n%s", infoOut)
	}
	line := infoOut[idx+len("content hash: sha256:"):]
	hash := strings.SplitN(line, "\n", 2)[0]

	projectDir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	out, err := runBundle(t, "install", dir, "--expect-sha256", hash)
	if err != nil {
		t.Fatalf("expected matching hash to succeed: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, ".aegis", "commands", "hello.md")); statErr != nil {
		t.Errorf("expected artifact installed: %v", statErr)
	}
}
