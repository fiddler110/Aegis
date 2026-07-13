package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestGenerateAndWriteTokenWritesRandomHex confirms GenerateAndWriteToken
// writes a 32-byte (64 hex char) random token to disk and returns the same
// value it wrote, and that two calls produce different tokens — the same
// "fresh token on every call" contract internal/server's daemon.token
// bootstrap has, now shared by the mcp-serve/acp stdio interfaces
// (P27.4/FIND-06).
func TestGenerateAndWriteTokenWritesRandomHex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.token")

	token, err := GenerateAndWriteToken(path)
	if err != nil {
		t.Fatalf("GenerateAndWriteToken: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64 (32 random bytes hex-encoded)", len(token))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(data) != token {
		t.Errorf("file contents = %q, want %q (the returned token)", data, token)
	}

	token2, err := GenerateAndWriteToken(path)
	if err != nil {
		t.Fatalf("GenerateAndWriteToken (second call): %v", err)
	}
	if token2 == token {
		t.Error("expected a fresh random token on each call, got the same value twice")
	}
}

// TestGenerateAndWriteTokenModeIsOwnerOnly checks the POSIX mode bits
// GenerateAndWriteToken passes to os.WriteFile. On Windows these bits are
// cosmetic — os.WriteFile's mode argument has no effect on the ACL a new
// file inherits from its parent directory there (see fsguard's package
// doc) — so the real enforcement on that platform is the ACL
// fsguard.RestrictToOwner applies, covered by
// TestGenerateAndWriteTokenRestrictsACL in token_windows_test.go.
func TestGenerateAndWriteTokenModeIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are cosmetic on Windows; see TestGenerateAndWriteTokenRestrictsACL")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "acp.token")

	if _, err := GenerateAndWriteToken(path); err != nil {
		t.Fatalf("GenerateAndWriteToken: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("token file mode = %v, want no group/other bits set", got)
	}
}
