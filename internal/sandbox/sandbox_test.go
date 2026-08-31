package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- path validator tests ---

// tempRoot is t.TempDir() with symlinks already resolved. ValidatePath returns
// the symlink-*resolved* path by design — that is the path a caller must open
// to avoid a TOCTOU swap of a link under it — so a test comparing its result
// against a raw t.TempDir() is comparing against the wrong thing on any host
// where the temp dir is reached through a link. macOS is exactly that host:
// /var is a symlink to /private/var, so t.TempDir() hands back /var/folders/…
// while ValidatePath correctly returns /private/var/folders/…. Resolving the
// expectation here (rather than teaching ValidatePath to return the unresolved
// path, which would defeat its purpose) keeps these tests hermetic across
// platforms — the same test-side hermeticity problem P48.1 fixed for config.
func tempRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return root
}

func TestValidatePathBasic(t *testing.T) {
	root := tempRoot(t)
	// Create a file inside root.
	inner := filepath.Join(root, "src", "main.go")
	os.MkdirAll(filepath.Dir(inner), 0o755)
	os.WriteFile(inner, []byte("package main"), 0o644)

	got, err := ValidatePath(root, "src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != inner {
		t.Errorf("got %q, want %q", got, inner)
	}
}

func TestValidatePathAbsolute(t *testing.T) {
	root := tempRoot(t)
	inner := filepath.Join(root, "file.txt")
	os.WriteFile(inner, []byte("hi"), 0o644)

	got, err := ValidatePath(root, inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != inner {
		t.Errorf("got %q, want %q", got, inner)
	}
}

func TestValidatePathEscapeDotDot(t *testing.T) {
	root := t.TempDir()
	_, err := ValidatePath(root, "../../../etc/passwd")
	if err == nil {
		t.Error("expected error for .. escape")
	}
}

func TestValidatePathEmpty(t *testing.T) {
	root := t.TempDir()
	_, err := ValidatePath(root, "")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestValidatePathWindowsRootedNoVolumeEscape(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-relative rooted paths only apply on Windows")
	}
	root := t.TempDir()
	// A driveless-rooted path ("/etc/shadow") is not filepath.IsAbs on
	// Windows, but the OS resolves it against the current drive, not root —
	// so it must be rejected as an escape (P32.1), not silently folded into
	// a path under root the way a plain relative path would be.
	if _, err := ValidatePath(root, "/etc/shadow"); err == nil {
		t.Error("expected error for driveless-rooted path escape")
	}
	if _, err := ValidatePath(root, `\Windows\System32`); err == nil {
		t.Error("expected error for driveless-rooted backslash path escape")
	}
}

func TestValidatePathWindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive root matching only applies on Windows")
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "src"), 0o755)

	// A differently-cased root must still resolve a path inside it rather than
	// being mistaken for an escape.
	lowerRoot := strings.ToLower(root)
	if _, err := ValidatePath(lowerRoot, "src/new.go"); err != nil {
		t.Errorf("case-differing root rejected a valid path: %v", err)
	}
}

func TestValidatePathNewFile(t *testing.T) {
	root := tempRoot(t)
	os.MkdirAll(filepath.Join(root, "src"), 0o755)

	// File doesn't exist yet, but parent does.
	got, err := ValidatePath(root, "src/new.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "src", "new.go")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValidatePathSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside root pointing outside. mustSymlink skips on a
	// platform that refuses rather than hard-skipping all of Windows, so a box
	// with Developer Mode enabled still exercises the escape check.
	link := filepath.Join(root, "escape")
	mustSymlink(t, outside, link)

	_, err := ValidatePath(root, "escape/secret.txt")
	if err == nil {
		t.Error("expected error for symlink escape")
	}
	if !strings.Contains(err.Error(), "symlink escape") {
		t.Errorf("expected symlink escape error, got: %v", err)
	}
}

func TestValidatePathSymlinkInside(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "file.txt"), []byte("ok"), 0o644)

	link := filepath.Join(root, "link")
	mustSymlink(t, target, link)

	// Symlink that stays inside root should succeed.
	_, err := ValidatePath(root, "link/file.txt")
	if err != nil {
		t.Errorf("unexpected error for intra-root symlink: %v", err)
	}
}

// --- local backend tests ---

func TestLocalExec(t *testing.T) {
	b := NewLocalBackend()
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		command = "Write-Output hello"
	} else {
		command = "echo hello"
	}

	out, err := b.Exec(ctx, command, ExecOpts{Dir: t.TempDir(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected hello in output, got %q", out)
	}
}

func TestLocalExecTimeout(t *testing.T) {
	b := NewLocalBackend()
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	} else {
		command = "sleep 30"
	}

	_, err := b.Exec(ctx, command, ExecOpts{Dir: t.TempDir(), Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Error("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout message, got: %v", err)
	}
}

func TestLocalExecStreaming(t *testing.T) {
	b := NewLocalBackend()
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		command = "Write-Output streaming"
	} else {
		command = "echo streaming"
	}

	var chunks []string
	err := b.ExecStreaming(ctx, command, ExecOpts{Dir: t.TempDir(), Timeout: 5 * time.Second}, func(s string) {
		chunks = append(chunks, s)
	})
	if err != nil {
		t.Fatalf("streaming error: %v", err)
	}
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "streaming") {
		t.Errorf("expected streaming in output, got %q", joined)
	}
}

func TestLocalExecFailure(t *testing.T) {
	b := NewLocalBackend()
	ctx := context.Background()

	_, err := b.Exec(ctx, "exit 1", ExecOpts{Dir: t.TempDir(), Timeout: 5 * time.Second})
	if err == nil {
		t.Error("expected error for failing command")
	}
}

func TestLocalName(t *testing.T) {
	if NewLocalBackend().Name() != "local" {
		t.Error("expected name 'local'")
	}
}

// TestLocalExecStripsProviderSecrets is the P7.2 regression: a command run by
// the default local backend must not see ANTHROPIC_API_KEY/OPENAI_API_KEY
// even though the daemon process itself has them set (e.g. to authenticate
// with the LLM provider). Otherwise a prompt-injected `shell` call could read
// them back out and exfiltrate via web_fetch.
func TestLocalExecStripsProviderSecrets(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-secret-value")
	t.Setenv("OPENAI_API_KEY", "sk-oai-secret-value")
	t.Setenv("HARMLESS_VAR", "still-here")

	b := NewLocalBackend()
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		command = "Get-ChildItem Env: | ForEach-Object { \"$($_.Name)=$($_.Value)\" }"
	} else {
		command = "env"
	}
	out, err := b.Exec(ctx, command, ExecOpts{Dir: t.TempDir(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if strings.Contains(out, "sk-ant-secret-value") || strings.Contains(out, "sk-oai-secret-value") {
		t.Errorf("provider secret leaked into command environment:\n%s", out)
	}
	if !strings.Contains(out, "HARMLESS_VAR") {
		t.Errorf("expected unrelated env var to survive stripping:\n%s", out)
	}
}

// TestLocalExecStripsConfiguredExtraSecrets verifies NewLocalBackendWithEnv
// strips additional names beyond DefaultStripEnv (e.g. an MCP token loaded
// from .aegis/.env), without dropping unrelated vars.
func TestLocalExecStripsConfiguredExtraSecrets(t *testing.T) {
	t.Setenv("MY_MCP_TOKEN", "super-secret-token")
	t.Setenv("HARMLESS_VAR", "still-here")

	b := NewLocalBackendWithEnv([]string{"MY_MCP_TOKEN"})
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		command = "Get-ChildItem Env: | ForEach-Object { \"$($_.Name)=$($_.Value)\" }"
	} else {
		command = "env"
	}
	out, err := b.Exec(ctx, command, ExecOpts{Dir: t.TempDir(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if strings.Contains(out, "super-secret-token") {
		t.Errorf("configured extra secret leaked into command environment:\n%s", out)
	}
	if !strings.Contains(out, "HARMLESS_VAR") {
		t.Errorf("expected unrelated env var to survive stripping:\n%s", out)
	}
}

func TestFilteredEnvCaseInsensitiveOnWindows(t *testing.T) {
	environ := []string{"Path=C:\\x", "ANTHROPIC_API_KEY=secret", "Other=1"}
	out := filteredEnv(environ, []string{"anthropic_api_key"})
	if runtime.GOOS == "windows" {
		for _, kv := range out {
			if strings.HasPrefix(strings.ToUpper(kv), "ANTHROPIC_API_KEY=") {
				t.Errorf("expected case-insensitive strip on windows, got %v", out)
			}
		}
	}
}

func TestMergeStripEnvDedupes(t *testing.T) {
	merged := mergeStripEnv([]string{"ANTHROPIC_API_KEY", "MY_MCP_TOKEN", ""})
	seen := map[string]int{}
	for _, n := range merged {
		seen[n]++
	}
	if seen["ANTHROPIC_API_KEY"] != 1 {
		t.Errorf("expected ANTHROPIC_API_KEY exactly once, got %d", seen["ANTHROPIC_API_KEY"])
	}
	if seen["MY_MCP_TOKEN"] != 1 {
		t.Errorf("expected MY_MCP_TOKEN exactly once, got %d", seen["MY_MCP_TOKEN"])
	}
	if seen["OPENAI_API_KEY"] != 1 {
		t.Errorf("expected default OPENAI_API_KEY to still be present, got %d", seen["OPENAI_API_KEY"])
	}
}

// TestResolveExistingFailsClosedWithNoExistingAncestor is M8. The walk used to
// return the *unresolved* path when it reached the filesystem root without
// finding anything that exists; the caller then compared that against a
// symlink-resolved root, which is a confinement verdict computed across two
// namespaces — the exact mismatch ResolveForCompare's own doc comment warns
// gives a wrong answer "in whichever direction the link points".
//
// Only Windows can actually produce the case: elsewhere every rooted path sits
// under "/", which exists, so the walk always terminates on something real. An
// unused drive letter has no existing ancestor at all.
func TestResolveExistingFailsClosedWithNoExistingAncestor(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("every POSIX rooted path has an existing ancestor (/), so the branch is unreachable there")
	}
	unused := unusedDriveLetter(t)
	orphan := unused + `:\nope\deeper\file.txt`

	if _, _, err := resolveExisting(orphan); !errors.Is(err, errNoExistingAncestor) {
		t.Errorf("resolveExisting(%q) err = %v, want errNoExistingAncestor", orphan, err)
	}
	// And the verdict the validators reach: refused, not silently compared.
	if _, err := ValidatePath(t.TempDir(), orphan); err == nil {
		t.Errorf("ValidatePath(%q) returned no error; an unresolvable path must fail closed", orphan)
	}
}

// unusedDriveLetter returns a drive letter with no volume mounted on it.
func unusedDriveLetter(t *testing.T) string {
	t.Helper()
	for _, c := range "QRSTUVWXYZ" {
		if _, err := os.Stat(string(c) + `:\`); err != nil {
			return string(c)
		}
	}
	t.Skip("no unused drive letter available on this host")
	return ""
}

// TestResolveForCompareStillNormalizesWithoutAnAncestor pins the other side of
// M8: ResolveForCompare is not a gate — it normalizes both sides of a
// comparison its caller then makes — so it keeps returning a cleaned path
// rather than adopting the validators' fail-closed behavior.
func TestResolveForCompareStillNormalizesWithoutAnAncestor(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c.txt")
	if got := ResolveForCompare(deep); got == "" {
		t.Error("a not-yet-existing leaf under an existing root must still normalize")
	}
}
