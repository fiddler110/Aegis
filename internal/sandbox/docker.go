package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ContainerRuntime identifies a container engine.
type ContainerRuntime string

const (
	RuntimeDocker          ContainerRuntime = "docker"
	RuntimePodman          ContainerRuntime = "podman"
	RuntimeWSL             ContainerRuntime = "wslc"      // Windows WSL containers (via `wslc` CLI)
	RuntimeAppleContainers ContainerRuntime = "container" // macOS Apple Containers (via `container` CLI)
)

// ContainerBackend runs commands inside a container using whichever container
// runtime is available (Docker, Podman, WSL containers on Windows, or Apple
// Containers on macOS). The workspace is bind-mounted so file tools continue to
// work on the host.
type ContainerBackend struct {
	runtime ContainerRuntime
	image   string
	network bool
}

// ContainerOpts configures the container sandbox.
type ContainerOpts struct {
	Image    string             // container image (default "ubuntu:22.04")
	Network  bool               // allow network access inside the container
	Prefer   ContainerRuntime   // force a specific runtime; empty = auto-detect
	Priority []ContainerRuntime // auto-detect order when Prefer is empty; empty = OS default
}

// NewContainerBackend creates a container sandbox, auto-detecting the best
// available runtime. Returns ErrNoContainerRuntime if none is found.
func NewContainerBackend(opts ContainerOpts) (*ContainerBackend, error) {
	rt, err := selectRuntime(context.Background(), opts.Prefer, opts.Priority)
	if err != nil {
		return nil, err
	}
	if opts.Image == "" {
		opts.Image = "ubuntu:22.04"
	}
	return &ContainerBackend{runtime: rt, image: opts.Image, network: opts.Network}, nil
}

// ErrNoContainerRuntime is returned when no container engine is available.
var ErrNoContainerRuntime = fmt.Errorf("sandbox: no container runtime found (tried docker, podman, wsl, apple containers)")

// selectRuntime resolves which runtime to use. A non-empty prefer is honored if
// available; otherwise the best runtime in priority order (or the OS default)
// wins.
func selectRuntime(ctx context.Context, prefer ContainerRuntime, priority []ContainerRuntime) (ContainerRuntime, error) {
	if prefer != "" {
		if probeRuntime(ctx, prefer).Available {
			return prefer, nil
		}
		return "", fmt.Errorf("sandbox: preferred runtime %q is not available", prefer)
	}
	if rt, ok := DetectBest(ctx, priority); ok {
		return rt, nil
	}
	return "", ErrNoContainerRuntime
}

// DetectedRuntime returns the runtime this backend is using.
func (c *ContainerBackend) DetectedRuntime() ContainerRuntime { return c.runtime }

func (c *ContainerBackend) Name() string { return "container:" + string(c.runtime) }

func (c *ContainerBackend) Exec(ctx context.Context, command string, opts ExecOpts) (string, error) {
	runCtx, cancel := execWithTimeout(ctx, opts)
	defer cancel()

	args := c.runArgs(command, opts)
	cmd := exec.CommandContext(runCtx, string(c.runtime), args...)

	out, err := cmd.CombinedOutput()
	text := string(out)
	if runCtx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("command timed out after %s", opts.Timeout)
	}
	if err != nil {
		return text, fmt.Errorf("container exec error: %w\n%s", err, text)
	}
	if strings.TrimSpace(text) == "" {
		return "(no output)", nil
	}
	return text, nil
}

func (c *ContainerBackend) ExecStreaming(ctx context.Context, command string, opts ExecOpts, emit func(string)) error {
	runCtx, cancel := execWithTimeout(ctx, opts)
	defer cancel()

	args := c.runArgs(command, opts)
	cmd := exec.CommandContext(runCtx, string(c.runtime), args...)
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
		return fmt.Errorf("container exec error: %w", err)
	}
	return nil
}

func (c *ContainerBackend) Close() error { return nil }

// runArgs builds the CLI arguments for a container run command.
func (c *ContainerBackend) runArgs(command string, opts ExecOpts) []string {
	switch c.runtime {
	case RuntimeAppleContainers:
		return c.appleContainerArgs(command, opts)
	case RuntimeWSL:
		return c.wslRunArgs(command, opts)
	default:
		return c.ociRunArgs(command, opts)
	}
}

// wslRunArgs builds `wslc run` arguments. wslc presents a Docker-style CLI.
// Verified against wslc 2.9.3.0: it accepts --rm, --network, -v, and -w, but
// does NOT expose the OCI hardening flags (--cap-drop, --security-opt), so those
// are omitted. The bind-mount source is a path inside the WSL VM, where Windows
// drives are mounted under /mnt/<drive>, so C:\work becomes /mnt/c/work.
func (c *ContainerBackend) wslRunArgs(command string, opts ExecOpts) []string {
	args := []string{"run", "--rm"}
	if !c.network {
		args = append(args, "--network", "none")
	}
	if opts.Dir != "" {
		args = append(args, "-v", wslHostPath(opts.Dir)+":/workspace", "-w", "/workspace")
	}
	args = append(args, c.image, "/bin/sh", "-c", command)
	return args
}

// wslHostPath converts a Windows host directory to its path inside the WSL VM
// (C:\foo\bar -> /mnt/c/foo/bar). Non-Windows paths pass through unchanged.
func wslHostPath(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if len(p) >= 2 && p[1] == ':' {
		p = "/mnt/" + strings.ToLower(string(p[0])) + p[2:]
	}
	return p
}

// ociRunArgs builds `docker run` / `podman run` arguments.
func (c *ContainerBackend) ociRunArgs(command string, opts ExecOpts) []string {
	args := []string{"run", "--rm", "--cap-drop=ALL", "--security-opt=no-new-privileges"}
	if !c.network {
		args = append(args, "--network", "none")
	}
	if opts.Dir != "" {
		args = append(args, "-v", hostPathForMount(opts.Dir)+":/workspace", "-w", "/workspace")
	}
	args = append(args, c.image, "/bin/sh", "-c", command)
	return args
}

// HostMountPath converts a host directory path to the form the given
// container runtime's -v flag expects: the WSL-VM path form for wslc
// (C:\foo -> /mnt/c/foo), the Docker/Podman path form for everything else
// (C:\foo -> /c/foo on Windows; unchanged elsewhere). Exported so other
// packages that run their own container invocations against a runtime
// selected via sandbox.DetectBest (e.g. internal/security's scanner
// container fallback, P11.1) don't need to reimplement this conversion.
func HostMountPath(rt ContainerRuntime, hostDir string) string {
	if rt == RuntimeWSL {
		return wslHostPath(hostDir)
	}
	return hostPathForMount(hostDir)
}

// hostPathForMount converts a host directory path to the format expected by
// Docker/Podman's -v flag. On Windows, C:\foo\bar becomes /c/foo/bar.
func hostPathForMount(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + strings.ToLower(string(p[0])) + p[2:]
	}
	return p
}

// SocketRuntime reports whether rt talks to its container engine over a local
// socket whose access is, by the engine's own design, privilege-equivalent to
// root (Docker) or the invoking user's full privileges (rootful Podman) — see
// "Docker/Podman socket privilege equivalence" in docs/security_scan.md
// (FIND-06 / P24.10). WSL containers and Apple Containers are deliberately
// excluded: wslc talks to a service inside the WSL VM rather than a
// host-privileged socket, and Apple Containers runs each container in a
// dedicated per-user, unprivileged lightweight VM — neither shares the
// Docker/Podman socket-equivalence model this flags.
func SocketRuntime(rt ContainerRuntime) bool {
	return rt == RuntimeDocker || rt == RuntimePodman
}

// SocketPrivilegeNotice returns a one-line informational message about
// Docker/Podman socket privilege equivalence for runtimes SocketRuntime
// flags, or "" for runtimes it doesn't apply to. Aegis cannot reliably tell a
// rootless from a rootful engine install from the client side across
// platforms, so this is a static, always-shown notice rather than a claim
// about this particular install's configuration — it is logged once when the
// container backend is selected (see internal/server.SelectSandbox).
func SocketPrivilegeNotice(rt ContainerRuntime) string {
	if !SocketRuntime(rt) {
		return ""
	}
	return fmt.Sprintf("sandbox: %s socket access is privilege-equivalent to local root (rootful Podman: the invoking user's full host privileges) — Aegis applies --cap-drop=ALL and --security-opt=no-new-privileges to every spawned container, but this does not mitigate the socket-level privilege equivalence itself; prefer rootless Podman or a userns-remapped Docker daemon where feasible (see docs/security_scan.md, \"Docker/Podman socket privilege equivalence\")", rt)
}

// appleContainerArgs builds arguments for Apple Containers CLI. Apple
// Containers uses a different invocation model; adapt as the CLI evolves.
func (c *ContainerBackend) appleContainerArgs(command string, opts ExecOpts) []string {
	args := []string{"run", "--rm"}
	if !c.network {
		args = append(args, "--network", "none")
	}
	if opts.Dir != "" {
		args = append(args, "-v", opts.Dir+":/workspace", "-w", "/workspace")
	}
	args = append(args, c.image, "/bin/sh", "-c", command)
	return args
}
