package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// OSBackend confines command execution using OS-level sandboxing without a
// container runtime (P4.7): macOS seatbelt (`sandbox-exec`) or Linux bubblewrap
// (`bwrap`). Writes are restricted to the workspace (plus temp dirs), reads are
// restricted to the workspace plus a bounded toolchain allowlist (P27.18/
// FIND-19), and network egress can be denied. This closes the gap where the
// plain `local` backend offered no isolation and containers required Docker.
//
// Read confinement is an allowlist, not "deny everything else": seatbelt
// explicitly denies file-read* outside (workspace ∪ readPaths ∪ host temp
// dirs), and bwrap only ro-binds readPaths plus the workspace instead of the
// entire host root. readPaths defaults to standard system/toolchain
// directories (see defaultOSReadPaths) and deliberately excludes credential
// directories like ~/.ssh, ~/.aws, or ~/.config — those are simply never
// bound/allowed, so they are unreadable from inside the sandbox even though
// they exist on the host. This is still weaker than the container backend
// (which never mounts the host filesystem at all): a toolchain directory that
// happens to also hold a stray credential file would still be readable. See
// docs/security_scan.md.
type OSBackend struct {
	workspace  string   // absolute workspace root; writes are confined here
	readPaths  []string // additional host paths readable inside the sandbox
	denyNet    bool     // deny network egress inside the sandbox
	mechanism  string   // "seatbelt" or "bwrap"
	wrapperBin string   // resolved path to sandbox-exec / bwrap
	// stripEnv lists env var names excluded from the spawned command's
	// environment (P7.2). Always includes DefaultStripEnv. Seatbelt/bwrap
	// confine the filesystem and network but do not touch the process
	// environment, so this still runs on the host's inherited env otherwise.
	stripEnv []string
}

// NewOSBackend builds an OS sandbox for the workspace. Network is denied unless
// allowNetwork is true. strip lists additional env var names to exclude from
// commands beyond DefaultStripEnv (P7.2). extraReadPaths names additional host
// paths (beyond the built-in toolchain defaults, see defaultOSReadPaths) that
// commands may read from — use this when a project's toolchain lives
// somewhere non-standard (P27.18/FIND-19). Returns an error when no OS
// sandbox mechanism is available on this host (callers should fall back to
// local).
func NewOSBackend(workspace string, allowNetwork bool, strip []string, extraReadPaths []string) (*OSBackend, error) {
	mech, bin, ok := detectOSSandbox()
	if !ok {
		return nil, fmt.Errorf("no OS sandbox available on %s (need sandbox-exec on macOS or bwrap on Linux)", runtime.GOOS)
	}
	return &OSBackend{
		workspace:  workspace,
		readPaths:  mergeReadPaths(defaultOSReadPaths(), extraReadPaths),
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
	dir := o.dir(opts)
	// extraRoot extends write/read confinement to a session's own Workdir
	// when it differs from the daemon's own workspace (P25.9): dir already
	// comes only from effectiveRoot/tool.WorkdirFromContext, which the
	// engine sets from a session's Workdir already validated (existence +
	// trust boundary) by resolveSessionWorkdir at session-creation time — no
	// tool exposes a user-suppliable directory argument that could smuggle
	// an unvalidated path in here.
	extraRoot := ""
	if dir != "" && escapesRoot(o.workspace, dir) {
		extraRoot = dir
	}
	switch o.mechanism {
	case "seatbelt":
		profile := seatbeltProfile(o.workspace, extraRoot, o.denyNet, o.readPaths)
		args := []string{"-p", profile, shell}
		return o.wrapperBin, append(args, shellArgs...)
	case "bwrap":
		args := bwrapArgs(o.workspace, extraRoot, dir, o.denyNet, o.readPaths)
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
// file writes outside the workspace (plus temp/dev) and file reads outside
// (workspace ∪ readPaths ∪ host temp dirs) (P27.18/FIND-19), and optionally
// denies network. Later rules override earlier ones in seatbelt, so the
// broad allow comes first and the deny/allow refinements follow. Only
// file-read*/file-write* are narrowed — every other default-allowed
// operation (process exec, mach lookups, sysctl reads, signals) is left
// alone, since tightening those is out of scope here and seatbelt profiles
// that deny them are notoriously easy to break basic command execution with.
//
// extraRoot, when non-empty, is allow-listed for both read and write
// alongside workspace (P25.9): a session running under a Workdir outside
// the daemon's own workspace needs write/read access to its own directory
// too, not just the daemon's.
func seatbeltProfile(workspace, extraRoot string, denyNet bool, readPaths []string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	b.WriteString("(allow file-write*\n")
	fmt.Fprintf(&b, "  (subpath %q)\n", workspace)
	if extraRoot != "" {
		fmt.Fprintf(&b, "  (subpath %q)\n", extraRoot)
	}
	b.WriteString("  (subpath \"/private/tmp\")\n")
	b.WriteString("  (subpath \"/private/var/folders\")\n")
	b.WriteString("  (subpath \"/dev\")\n")
	b.WriteString("  (literal \"/dev/null\"))\n")
	b.WriteString("(deny file-read*)\n")
	b.WriteString("(allow file-read*\n")
	fmt.Fprintf(&b, "  (subpath %q)\n", workspace)
	if extraRoot != "" {
		fmt.Fprintf(&b, "  (subpath %q)\n", extraRoot)
	}
	b.WriteString("  (subpath \"/private/tmp\")\n")
	b.WriteString("  (subpath \"/private/var/folders\")\n")
	b.WriteString("  (subpath \"/dev\")\n")
	b.WriteString("  (literal \"/dev/null\")\n")
	for _, p := range readPaths {
		fmt.Fprintf(&b, "  (subpath %q)\n", p)
	}
	b.WriteString(")\n")
	if denyNet {
		b.WriteString("(deny network*)\n")
	}
	return b.String()
}

// bwrapArgs builds bubblewrap arguments: only the workspace (read-write) and
// readPaths (read-only) are bound into the sandbox's filesystem instead of
// the entire host root (P27.18/FIND-19), /tmp is a fresh tmpfs, and network
// is unshared when denied. A command that reads a path outside this
// allowlist gets ENOENT rather than the real host file.
//
// extraRoot, when non-empty, is bound read-write alongside workspace
// (P25.9), same rationale as seatbeltProfile's extraRoot.
func bwrapArgs(workspace, extraRoot, dir string, denyNet bool, readPaths []string) []string {
	args := make([]string, 0, 10+4*len(readPaths))
	for _, p := range readPaths {
		args = append(args, "--ro-bind", p, p)
	}
	args = append(args,
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", workspace, workspace,
	)
	if extraRoot != "" {
		args = append(args, "--bind", extraRoot, extraRoot)
	}
	args = append(args, "--die-with-parent")
	if dir != "" {
		args = append(args, "--chdir", dir)
	}
	if denyNet {
		args = append(args, "--unshare-net")
	}
	return args
}

// defaultOSReadPaths returns host paths — beyond the workspace itself — that
// the OS sandbox allows commands to read from, so common toolchains keep
// working under the read confinement added by P27.18/FIND-19. This is an
// allowlist: anything not returned here (or added via
// sandbox.os_extra_read_paths) is simply never readable from inside the
// sandbox — including credential directories like ~/.ssh, ~/.aws, or
// ~/.config, which this list deliberately omits. Entries are not filtered
// for existence here; mergeReadPaths does that once against the final
// combined list.
func defaultOSReadPaths() []string {
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, "/usr", "/bin", "/sbin", "/System", "/Library", "/opt", "/private/etc")
	case "linux":
		paths = append(paths, "/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc", "/opt")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, sub := range []string{
			"go", ".cargo", ".rustup", ".npm", ".nvm", ".pyenv", ".gem", ".bundle", ".local", ".cache",
		} {
			paths = append(paths, filepath.Join(home, sub))
		}
	}
	return paths
}

// mergeReadPaths combines base (the toolchain defaults) with extra
// (operator-configured, sandbox.os_extra_read_paths), deduping and dropping
// any entry that doesn't exist on this host — bwrap fails to bind a missing
// source path, and a nonexistent seatbelt subpath is a silent no-op, so
// filtering once here keeps both mechanisms' behavior predictable.
func mergeReadPaths(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, paths := range [][]string{base, extra} {
		for _, p := range paths {
			if p == "" {
				continue
			}
			clean := filepath.Clean(p)
			if seen[clean] {
				continue
			}
			seen[clean] = true
			if _, err := os.Stat(clean); err != nil {
				continue
			}
			out = append(out, clean)
		}
	}
	return out
}
