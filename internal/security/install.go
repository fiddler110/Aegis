package security

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
)

// InstallCommand returns the guided host-install shell command for name on
// the current OS, and whether one exists. Callers (the `aegis security
// install` CLI and the `/security-config` TUI wizard) show this to the
// operator before running it — installing software is a privileged,
// host-modifying action that must never happen silently.
func InstallCommand(name string) (string, bool) {
	d, ok := DescriptorFor(name)
	if !ok {
		return "", false
	}
	cmd, ok := d.Install[runtime.GOOS]
	if !ok || strings.TrimSpace(cmd) == "" {
		return "", false
	}
	return cmd, true
}

// shellInvocation returns the platform shell binary and args to run command
// through, mirroring the shell tool's own invocation convention.
func shellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// RunGuidedInstall runs name's guided host-install command for the current
// OS, streaming combined stdout+stderr to out as it runs. The caller is
// responsible for getting explicit operator confirmation first (see
// InstallCommand) — this function only executes.
func RunGuidedInstall(ctx context.Context, name string, out io.Writer) error {
	command, ok := InstallCommand(name)
	if !ok {
		return fmt.Errorf("no guided install available for %s on %s — see the tool's own docs, or configure a container image (security.tools.%s.image)", name, runtime.GOOS, name)
	}
	shell, args := shellInvocation(command)
	c := exec.CommandContext(ctx, shell, args...)
	c.Stdout = out
	c.Stderr = out
	if err := c.Run(); err != nil {
		return fmt.Errorf("install command failed: %w", err)
	}
	return nil
}
