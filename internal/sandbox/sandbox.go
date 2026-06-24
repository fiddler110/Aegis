// Package sandbox provides pluggable command-execution backends. The local
// backend runs commands directly on the host (the default). The container
// backend auto-detects a container runtime (Docker, Podman, or Apple
// Containers on macOS) and runs commands in an isolated container with the
// workspace bind-mounted.
package sandbox

import (
	"context"
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

// shellCommand returns the platform shell binary and argument list for running
// a command string.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// execWithTimeout wraps ctx with a timeout if opts.Timeout > 0.
func execWithTimeout(ctx context.Context, opts ExecOpts) (context.Context, context.CancelFunc) {
	if opts.Timeout > 0 {
		return context.WithTimeout(ctx, opts.Timeout)
	}
	return ctx, func() {}
}
