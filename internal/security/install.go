package security

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sort"
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

// PlatformAvailability summarizes, for one scanner, which OSes have a guided
// host-install command and whether the current OS is among them (P13.1).
type PlatformAvailability struct {
	HasAnyHostInstall  bool     // true if any OS has a guided install command at all
	CurrentOSSupported bool     // true if runtime.GOOS specifically has one
	OtherOSes          []string // sorted OS names with a guided install, excluding the current OS
}

// InstallAvailability reports name's cross-platform host-install coverage.
func InstallAvailability(name string) PlatformAvailability {
	d, ok := DescriptorFor(name)
	if !ok {
		return PlatformAvailability{}
	}
	_, current := d.Install[runtime.GOOS]
	others := make([]string, 0, len(d.Install))
	for osName := range d.Install {
		if osName != runtime.GOOS {
			others = append(others, osName)
		}
	}
	sort.Strings(others)
	return PlatformAvailability{HasAnyHostInstall: len(d.Install) > 0, CurrentOSSupported: current, OtherOSes: others}
}

// AvailabilityNote returns a short, actionable suffix for a MethodNone reason
// that already says a host binary is missing ("not installed") when name has
// a guided host install for some OS but not the current one — steering the
// operator toward the actual next step (another OS, or a container image)
// instead of a bare "not installed" verdict that reads identically whether
// installing here is even possible (P13.1). Returns "" when reason isn't
// about a missing host binary (e.g. disabled/opt-in/container-method
// reasons), or when the current OS already has — or no OS has — a guided
// install, since there's nothing more useful to add in those cases.
func AvailabilityNote(name, reason string) string {
	if !strings.Contains(reason, "not installed") {
		return ""
	}
	av := InstallAvailability(name)
	if !av.HasAnyHostInstall || av.CurrentOSSupported {
		return ""
	}
	return fmt.Sprintf(
		"no native host install for %s (available on: %s) — configure security.tools.%s.image for a container fallback",
		runtime.GOOS, strings.Join(av.OtherOSes, ", "), name,
	)
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
