package security

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// wslDistroAvailable is a seam over sandbox.WSLDistroAvailable so tests can
// inject a deterministic result without needing a real WSL install.
var wslDistroAvailable = sandbox.WSLDistroAvailable

// InstallCommand returns the guided host-install shell command for name on
// the current OS, and whether one exists. Callers (the `aegis security
// install` CLI and the `/security-config` TUI wizard) show this to the
// operator before running it — installing software is a privileged,
// host-modifying action that must never happen silently.
//
// On Windows, a WSLCapable tool with no native Windows install entry falls
// back to running its Linux install command inside WSL when a distro is
// registered (P14.x) — opengrep and kubescape ship no Windows build at all,
// but their Linux curl|bash installer works unmodified there. The returned
// command is the literal `wsl -- bash -lc '...'` invocation, so it's
// simultaneously an accurate thing to show the operator and something
// RunGuidedInstall can execute unchanged.
//
// The WSLCapable condition matters (P34.9): Resolve only ever offers
// MethodWSL for a WSLCapable tool, so installing anything else into WSL
// produces a binary no scan can reach. Until njsscan dropped its (broken)
// Windows install entry every tool without one happened to be WSLCapable,
// which made the missing check invisible rather than harmless.
func InstallCommand(name string) (string, bool) {
	d, ok := DescriptorFor(name)
	if !ok {
		return "", false
	}
	if cmd, ok := d.Install[hostGOOS]; ok && strings.TrimSpace(cmd) != "" {
		return cmd, true
	}
	if hostGOOS == "windows" && d.WSLCapable {
		if linuxCmd, ok := d.Install["linux"]; ok && strings.TrimSpace(linuxCmd) != "" && wslDistroAvailable(context.Background(), "") {
			return sandbox.WSLInstallCommand(linuxCmd, ""), true
		}
	}
	return "", false
}

// PlatformAvailability summarizes, for one scanner, which OSes have a guided
// host-install command and whether the current OS is among them (P13.1).
type PlatformAvailability struct {
	HasAnyHostInstall  bool     // true if any OS has a guided install command at all
	CurrentOSSupported bool     // true if the current OS specifically has one
	OtherOSes          []string // sorted OS names with a guided install, excluding the current OS
}

// InstallAvailability reports name's cross-platform host-install coverage.
func InstallAvailability(name string) PlatformAvailability {
	d, ok := DescriptorFor(name)
	if !ok {
		return PlatformAvailability{}
	}
	_, current := d.Install[hostGOOS]
	others := make([]string, 0, len(d.Install))
	for osName := range d.Install {
		if osName != hostGOOS {
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
		hostGOOS, strings.Join(av.OtherOSes, ", "), name,
	)
}

// NoGuidedInstallReason builds the explanatory error shown when
// InstallCommand has nothing to offer for name on this OS — shared by
// RunGuidedInstall and the `aegis security install` CLI so the wording (and
// the WSL guidance it adds when relevant) never drifts between the two.
func NoGuidedInstallReason(name string) string {
	msg := fmt.Sprintf("no guided install available for %s on %s — see the tool's own docs, or configure a container image (security.tools.%s.image)", name, hostGOOS, name)
	d, ok := DescriptorFor(name)
	if !ok {
		return msg
	}
	// A HostBroken platform is the one case where "no guided install" isn't
	// the operator's problem to solve: no host install exists that would
	// work, so pointing at one (or at WSL, below) wastes their time. Say why
	// and name the method that does run the tool here.
	if reason := d.HostBroken[hostGOOS]; reason != "" {
		return fmt.Sprintf("%s — no host install would fix that: run `aegis security build-image` and leave security.tools.%s.method unset (or set it to \"container\") to run it in the Linux scanner image instead", reason, name)
	}
	if hostGOOS != "windows" {
		return msg
	}
	// Gated on WSLCapable for the same reason InstallCommand's WSL fallback
	// is: suggesting WSL for a tool Resolve can't route through WSL sends the
	// operator to install something no scan will ever use.
	if linuxCmd, ok := d.Install["linux"]; ok && strings.TrimSpace(linuxCmd) != "" && d.WSLCapable {
		return msg + ", or install a Linux distro under WSL (`wsl --install`) so its Linux install command can run there"
	}
	return msg
}

// RunGuidedInstall runs name's guided host-install command for the current
// OS, streaming combined stdout+stderr to out as it runs. The caller is
// responsible for getting explicit operator confirmation first (see
// InstallCommand) — this function only executes.
//
// Audited for FIND-31/P24.8 (verification-only threat-model item): command
// is always one of the compile-time literals in descriptors' Install maps
// (method.go) — name only ever selects a map key, DescriptorFor rejects
// unknown names, and no config/runtime value is ever concatenated into a
// command string. shellInvocation hands command to exec.CommandContext as a
// single, unmodified argv element (never re-split by Go), so it's opaque to
// everything except the one real shell process that interprets it — see
// TestInstallCommandsHaveNoInterpolationMarkers and
// TestShellInvocationKeepsCommandAsSingleArgvElement in install_test.go.
func RunGuidedInstall(ctx context.Context, name string, out io.Writer) error {
	command, ok := InstallCommand(name)
	if !ok {
		return fmt.Errorf("%s", NoGuidedInstallReason(name))
	}
	shell, args := sandbox.ShellCommand(command)
	c := exec.CommandContext(ctx, shell, args...)
	c.Stdout = out
	c.Stderr = out
	if err := c.Run(); err != nil {
		return fmt.Errorf("install command failed: %w", err)
	}
	return nil
}
