package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- path validator tests ---

func TestValidatePathBasic(t *testing.T) {
	root := t.TempDir()
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
	root := t.TempDir()
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

func TestValidatePathNewFile(t *testing.T) {
	root := t.TempDir()
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
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside root pointing outside.
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := ValidatePath(root, "escape/secret.txt")
	if err == nil {
		t.Error("expected error for symlink escape")
	}
	if !strings.Contains(err.Error(), "symlink escape") {
		t.Errorf("expected symlink escape error, got: %v", err)
	}
}

func TestValidatePathSymlinkInside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require elevated privileges on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "real")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "file.txt"), []byte("ok"), 0o644)

	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

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
