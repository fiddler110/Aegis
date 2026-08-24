// Package sandbox provides pluggable command-execution backends. The local
// backend runs commands directly on the host (the default). The container
// backend auto-detects a container runtime (Docker, Podman, or Apple
// Containers on macOS) and runs commands in an isolated container with the
// workspace bind-mounted.
package sandbox

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// Backend isolates command execution.
type Backend interface {
	// Name identifies the backend ("local", "container:docker", etc.).
	Name() string
	// Exec runs command with combined stdout/stderr returned as a string.
	Exec(ctx context.Context, command string, opts ExecOpts) (string, error)
	// ExecStreaming runs command, streaming combined output to emit as it
	// arrives. It blocks until the command finishes or ctx is cancelled.
	ExecStreaming(ctx context.Context, command string, opts ExecOpts, emit func(string)) error
	// Close releases any resources.
	Close() error
}

// ExecOpts configures a single command execution.
type ExecOpts struct {
	Dir     string        // working directory
	Timeout time.Duration // per-command timeout (0 = no timeout beyond ctx)
}

// ShellCommand returns the platform shell binary and argument list for
// running a command string through: PowerShell (preferring "pwsh" via
// WindowsShellBinary) on Windows, where a POSIX "sh" is not guaranteed to be
// on PATH, and "/bin/sh -c" elsewhere. Exported so every package that shells
// out a passthrough command (internal/tui's `!` command, internal/security's
// guided installer, internal/hooks' hook commands) shares one implementation
// instead of re-deriving it (P77.3).
func ShellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return WindowsShellBinary(), []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// WindowsShellBinary prefers "pwsh" (PowerShell 7+) over "powershell"
// (Windows PowerShell 5.1) when it's on PATH. Machines that installed
// PowerShell 7 via winget/Microsoft Store commonly end up with its
// Core-edition-only module directory ordered ahead of the system32
// Windows PowerShell module directory in PSModulePath; a spawned
// "powershell.exe" process inherits that ordering and autoloads the
// Core-only Microsoft.PowerShell.Utility module instead of the Desktop
// one, silently losing Desktop-only cmdlets like Get-FileHash (which
// scoop's own install scripts depend on). "pwsh" doesn't hit this since
// it only ever loads its own Core-compatible modules. Exported so other
// packages that shell out on Windows (e.g. internal/security's guided
// installer) use the same fallback instead of hardcoding "powershell".
func WindowsShellBinary() string {
	if _, err := exec.LookPath("pwsh"); err == nil {
		return "pwsh"
	}
	return "powershell"
}

// execWithTimeout wraps ctx with a timeout if opts.Timeout > 0.
func execWithTimeout(ctx context.Context, opts ExecOpts) (context.Context, context.CancelFunc) {
	if opts.Timeout > 0 {
		return context.WithTimeout(ctx, opts.Timeout)
	}
	return ctx, func() {}
}
