package security

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestInstallCommandKnownTool(t *testing.T) {
	// semgrep ships an install command for every supported OS (unlike
	// opengrep, whose curl|bash installer has no Windows equivalent).
	cmd, ok := InstallCommand("semgrep")
	if !ok || strings.TrimSpace(cmd) == "" {
		t.Fatalf("InstallCommand(semgrep) = (%q, %v), want a non-empty command", cmd, ok)
	}
}

func TestInstallCommandUnknownTool(t *testing.T) {
	if _, ok := InstallCommand("not-a-real-scanner"); ok {
		t.Error("InstallCommand for an unknown scanner should return ok=false")
	}
}

func TestRunGuidedInstallUnknownToolErrors(t *testing.T) {
	var buf bytes.Buffer
	err := RunGuidedInstall(context.Background(), "not-a-real-scanner", &buf)
	if err == nil {
		t.Fatal("expected an error for an unknown scanner")
	}
	if !strings.Contains(err.Error(), "no guided install available") {
		t.Errorf("err = %q, want it to explain no install is available", err.Error())
	}
}

func TestRunGuidedInstallRunsCommand(t *testing.T) {
	// A fake descriptor whose "install" command is a trivial, portable
	// shell/PowerShell one-liner so this test never touches the network or a
	// real package manager.
	withTestDescriptor(t, ScannerDescriptor{
		Name:   "test-echo-install",
		Binary: "test-echo-install",
		Install: map[string]string{
			"windows": "Write-Output hello",
			"linux":   "echo hello",
			"darwin":  "echo hello",
		},
	})

	var buf bytes.Buffer
	if err := RunGuidedInstall(context.Background(), "test-echo-install", &buf); err != nil {
		t.Fatalf("RunGuidedInstall: %v (output: %s)", err, buf.String())
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("output = %q, want it to contain the command's output", buf.String())
	}
}
