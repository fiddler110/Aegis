package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OSBackend confines command execution using OS-level sandboxing without a
// container runtime (P4.7): macOS seatbelt (`sandbox-exec`) or Linux bubblewrap
// (`bwrap`). Writes are restricted to the workspace (plus temp dirs) and network
// egress can be denied. This closes the gap where the plain `local` backend
// offered no isolation and containers required Docker.
//
// This is a write/network sandbox only, not a read sandbox: seatbelt's profile
// is "(allow default)" with just file-write* denied outside the workspace, and
// bwrap's is "--ro-bind / /", read-only-mounting the entire host filesystem
// inside the sandbox. A compromised command can still read any host file
// (SSH keys, cloud credentials) and exfiltrate it unless network is also
// denied — materially weaker than the container backend, which the host
// filesystem is never mounted into at all. See docs/security.md.
type OSBackend struct {
	workspace  string // absolute workspace root; writes are confined here
	denyNet    bool   // deny network egress inside the sandbox
	mechanism  string // "seatbelt" or "bwrap"
	wrapperBin string // resolved path to sandbox-exec / bwrap
	// stripEnv lists env var names excluded from the spawned command's
	// environment (P7.2). Always includes DefaultStripEnv. Seatbelt/bwrap
	// confine the filesystem and network but do not touch the process
	// environment, so this still runs on the host's inherited env otherwise.
	stripEnv []string
}

// NewOSBackend builds an OS sandbox for the workspace. Network is denied unless
// allowNetwork is true. strip lists additional env var names to exclude from
// commands beyond DefaultStripEnv (P7.2). Returns an error when no OS sandbox
// mechanism is available on this host (callers should fall back to local).
func NewOSBackend(workspace string, allowNetwork bool, strip []string) (*OSBackend, error) {
	mech, bin, ok := detectOSSandbox()
	if !ok {
		return nil, fmt.Errorf("no OS sandbox available on %s (need sandbox-exec on macOS or bwrap on Linux)", runtime.GOOS)
	}
	return &OSBackend{
		workspace:  workspace,
		denyNet:    !allowNetwork,
		mechanism:  mech,
		wrapperBin: bin,
		stripEnv:   mergeStripEnv(strip),
	}, nil
}

func (o *OSBackend) Name() string { return "os:" + o.mechanism }

func (o *OSBackend) Exec(ctx context.Context, command string, opts ExecOpts) (string, error) {
	runCtx, cancel := execWithTimeout(ctx, opts)
	defer cancel()

	name, args := o.wrap(command, opts)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = o.dir(opts)
	cmd.Env = filteredEnv(os.Environ(), o.stripEnv)
	cmd.WaitDelay = ioCloseGrace

	out, err := cmd.CombinedOutput()
	text := string(out)
	if runCtx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("command timed out after %s", opts.Timeout)
	}
	if err != nil {
		return text, fmt.Errorf("exit error: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return "(no output)", nil
	}
	return text, nil
}

func (o *OSBackend) ExecStreaming(ctx context.Context, command string, opts ExecOpts, emit func(string)) error {
	runCtx, cancel := execWithTimeout(ctx, opts)
	defer cancel()

	name, args := o.wrap(command, opts)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = o.dir(opts)
	cmd.Env = filteredEnv(os.Environ(), o.stripEnv)
	cmd.WaitDelay = ioCloseGrace
	w := emitWriter{emit: emit}
	cmd.Stdout = w
	cmd.Stderr = w

	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command timed out after %s", opts.Timeout)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("exit error: %w", err)
	}
	return nil
}

func (o *OSBackend) Close() error { return nil }

func (o *OSBackend) dir(opts ExecOpts) string {
	if opts.Dir != "" {
		return opts.Dir
	}
	return o.workspace
}

// wrap builds the argv that runs the shell command inside the OS sandbox.
func (o *OSBackend) wrap(command string, opts ExecOpts) (string, []string) {
	shell, shellArgs := shellCommand(command)
	switch o.mechanism {
	case "seatbelt":
		profile := seatbeltProfile(o.workspace, o.denyNet)
		args := []string{"-p", profile, shell}
		return o.wrapperBin, append(args, shellArgs...)
	case "bwrap":
		args := bwrapArgs(o.workspace, o.dir(opts), o.denyNet)
		args = append(args, shell)
		return o.wrapperBin, append(args, shellArgs...)
	default:
		return shell, shellArgs
	}
}

// detectOSSandbox reports which OS sandbox mechanism is available.
func detectOSSandbox() (mechanism, bin string, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		if p, err := exec.LookPath("sandbox-exec"); err == nil {
			return "seatbelt", p, true
		}
	case "linux":
		if p, err := exec.LookPath("bwrap"); err == nil {
			return "bwrap", p, true
		}
	}
	return "", "", false
}

// OSSandboxInfo reports OS-sandbox availability for `aegis sandbox detect`.
func OSSandboxInfo() (mechanism string, available bool, detail string) {
	mech, _, ok := detectOSSandbox()
	if ok {
		return mech, true, "no container runtime required"
	}
	switch runtime.GOOS {
	case "darwin":
		return "seatbelt", false, "sandbox-exec not found (unexpected on macOS)"
	case "linux":
		return "bwrap", false, "install bubblewrap (e.g. apt install bubblewrap)"
	default:
		return "", false, "OS sandbox unsupported on " + runtime.GOOS
	}
}

// seatbeltProfile builds a macOS sandbox profile that allows everything except
// file writes outside the workspace (plus temp/dev), and optionally denies
// network. Later rules override earlier ones in seatbelt, so the broad allow
// comes first and the deny/allow refinements follow.
func seatbeltProfile(workspace string, denyNet bool) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	b.WriteString("(allow file-write*\n")
	fmt.Fprintf(&b, "  (subpath %q)\n", workspace)
	b.WriteString("  (subpath \"/private/tmp\")\n")
	b.WriteString("  (subpath \"/private/var/folders\")\n")
	b.WriteString("  (subpath \"/dev\")\n")
	b.WriteString("  (literal \"/dev/null\"))\n")
	if denyNet {
		b.WriteString("(deny network*)\n")
	}
	return b.String()
}

// bwrapArgs builds bubblewrap arguments: the whole filesystem is read-only, the
// workspace is bind-mounted read-write, /tmp is a fresh tmpfs, and network is
// unshared when denied.
func bwrapArgs(workspace, dir string, denyNet bool) []string {
	args := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", workspace, workspace,
		"--die-with-parent",
	}
	if dir != "" {
		args = append(args, "--chdir", dir)
	}
	if denyNet {
		args = append(args, "--unshare-net")
	}
	return args
}
