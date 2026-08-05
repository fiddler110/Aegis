package sandbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
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
	limits  ResourceLimits

	// Persistent-container state (P60.2). persistent is mutable: a failed
	// start turns it off for the rest of this backend's life so the fallback
	// warning is said once rather than per tool call. See persistent.go.
	logger     *slog.Logger
	sessionTTL time.Duration
	cli        containerCLI
	mu         sync.Mutex
	persistent bool
	containers map[string]string // working directory -> container id
}

// ContainerOpts configures the container sandbox.
type ContainerOpts struct {
	Image    string             // container image (default "ubuntu:22.04")
	Network  bool               // allow network access inside the container
	Prefer   ContainerRuntime   // force a specific runtime; empty = auto-detect
	Priority []ContainerRuntime // auto-detect order when Prefer is empty; empty = OS default
	// Limits caps what one container run may consume (P60.1). The zero value
	// means "no limits", which is what every run did before this existed.
	Limits ResourceLimits
	// Persistent keeps one long-lived container per workspace directory and
	// runs each command in it with `exec`, instead of a fresh `run --rm` per
	// command (P60.2). Ignored on a runtime SupportsPersistentContainer
	// rejects, which keeps the pre-P60.2 behavior there.
	Persistent bool
	// SessionTTL bounds a persistent container's life when Close() is never
	// called (a killed daemon). 0 uses DefaultSessionTTL. No effect unless
	// Persistent is set.
	SessionTTL time.Duration
	// Logger receives the lifecycle lines persistent mode produces (started,
	// reaped, degraded to one-shot). nil discards them.
	Logger *slog.Logger
}

// ResourceLimits caps a sandboxed container's resource consumption (P60.1).
//
// The hardening flags cover the *privilege* axis; this is the resource one,
// which was missing entirely: a model-driven `go build`, `npm ci` or test run
// could consume the whole host, and on a machine that is also running the model
// server the failure mode is the OOM killer choosing between the model and the
// daemon rather than a failed command. Per-command `--rm` bounds how long a
// runaway lasts, never its peak, and the peak is what binds — one `go build` is
// enough.
//
// Each field is independently optional: an empty string or a non-positive count
// omits that flag, so an operator can cap memory without touching CPU. Values
// are passed through to the runtime CLI verbatim rather than parsed here — the
// engines already own that vocabulary ("4G", "512M", "1.5" CPUs) and
// re-implementing it would only add a second, poorer parser.
type ResourceLimits struct {
	Memory string // --memory, e.g. "4G"; "" omits
	CPUs   string // --cpus, e.g. "2"; "" omits
	PIDs   int    // --pids-limit; <= 0 omits
}

// Empty reports whether no limit at all is set.
func (l ResourceLimits) Empty() bool {
	return strings.TrimSpace(l.Memory) == "" && strings.TrimSpace(l.CPUs) == "" && l.PIDs <= 0
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
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &ContainerBackend{
		runtime: rt,
		image:   opts.Image,
		network: opts.Network,
		limits:  opts.Limits,
		logger:  logger,
		// P60.2: honored only where the detach/exec CLI surface is verified;
		// elsewhere the backend keeps its per-command behavior rather than
		// failing on a subcommand the runtime does not have.
		persistent: opts.Persistent && SupportsPersistentContainer(rt),
		sessionTTL: opts.SessionTTL,
		containers: map[string]string{},
		cli:        &execCLI{runtime: rt},
	}, nil
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

	args, persistent := c.commandArgs(ctx, command, opts)
	out, err := c.cli.run(runCtx, args)
	if persistent && containerGone(out, err) {
		// The container expired or was removed underneath us. Start a fresh
		// one and run the command once more: from the caller's side this is a
		// slow command, not a failed one. Only once — a second failure is a
		// real error and is reported as such.
		c.forget(opts.Dir)
		args, _ = c.commandArgs(ctx, command, opts)
		out, err = c.cli.run(runCtx, args)
	}
	text := out
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

	args, persistent := c.commandArgs(ctx, command, opts)
	err := c.cli.stream(runCtx, args, emit)
	if persistent && ctx.Err() == nil && runCtx.Err() == nil && containerGone("", err) {
		c.forget(opts.Dir)
		args, _ = c.commandArgs(ctx, command, opts)
		err = c.cli.stream(runCtx, args, emit)
	}
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

// Close tears down the session-lifetime containers this backend started
// (P60.2). It stays a no-op in per-command mode, where there has never been
// anything to close.
func (c *ContainerBackend) Close() error {
	c.closePersistent()
	return nil
}

// commandArgs returns the argument list for running command, and whether it
// runs inside a persistent container. It is the one place the two modes
// diverge: everything above it — timeouts, output handling, error wrapping — is
// shared, so a persistent command is not quietly a different kind of command.
func (c *ContainerBackend) commandArgs(ctx context.Context, command string, opts ExecOpts) ([]string, bool) {
	if id, ok := c.container(ctx, opts.Dir); ok {
		return execPersistentArgs(id, command, opts), true
	}
	return c.runArgs(command, opts), false
}

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
	args = append(args, ResourceFlags(c.runtime, c.limits)...)
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

// OCIHardeningFlags returns the run flags that drop every capability and block
// privilege escalation, or nil for a runtime whose CLI does not accept them.
//
// Exported because this is a per-runtime CLI *fact*, and every package that
// builds its own container command line needs it. internal/security had its own
// copy that excluded only Apple Containers, which meant every scanner container
// run under wslc died on "Argument name was not recognized for the current
// command: '--cap-drop=ALL'" — a total failure of container-method scanning on
// any Windows machine where DetectBest picked wslc, reported per tool as "the
// tool is missing from the image or cannot start". One helper, so a runtime added
// here can't leave a second call site guessing.
//
// Returning nil is a real hardening difference, not a formality: a wslc or Apple
// Containers run keeps its default capability set. Both are accepted for the
// reason SocketRuntime gives — each already isolates in a per-user VM rather than
// through a host-privileged socket — and this mirrors the stance wslRunArgs and
// appleContainerArgs have always taken for the sandbox's own containers.
func OCIHardeningFlags(rt ContainerRuntime) []string {
	switch rt {
	case RuntimeWSL, RuntimeAppleContainers:
		return nil
	default:
		return []string{"--cap-drop=ALL", "--security-opt=no-new-privileges"}
	}
}

// ResourceFlags returns the run flags that apply lim on rt, restricted to the
// subset that runtime's CLI actually accepts (P60.1).
//
// The subset is a per-runtime CLI *fact*, asked the same way OCIHardeningFlags
// asks it and for the same reason: passing a flag a runtime does not know is
// not a weaker limit, it is a container that refuses to start — which is
// exactly how the pre-P24 hardening copy silently killed every wslc scanner run.
// So the rule here is that a flag appears only where it is verified:
//
//   - docker/podman: --memory, --cpus, --pids-limit, all three.
//   - Apple Containers: -m/--memory and -c/--cpus are documented Resource
//     Options of `container run`; --pids-limit is not, so it is omitted. Each
//     container is its own lightweight VM, so the memory cap is the load-bearing
//     one there anyway.
//   - wslc: nothing. Its CLI is Docker-shaped but does not expose the hardening
//     flags, and its resource surface is unverified — the same precedent that
//     makes OCIHardeningFlags return nil for it. Applying an unverified flag to
//     the one runtime already known to reject unknown arguments is the trade
//     this codebase has already paid for once.
//
// A runtime that accepts nothing therefore gets no resource cap, which is a
// real difference and not a formality — SelectSandbox says so at startup rather
// than letting an operator infer a bound that isn't there.
func ResourceFlags(rt ContainerRuntime, lim ResourceLimits) []string {
	var flags []string
	mem := strings.TrimSpace(lim.Memory)
	cpus := strings.TrimSpace(lim.CPUs)
	switch rt {
	case RuntimeWSL:
		return nil
	case RuntimeAppleContainers:
		if mem != "" {
			flags = append(flags, "--memory", mem)
		}
		if cpus != "" {
			flags = append(flags, "--cpus", cpus)
		}
		return flags
	default:
		if mem != "" {
			flags = append(flags, "--memory", mem)
		}
		if cpus != "" {
			flags = append(flags, "--cpus", cpus)
		}
		if lim.PIDs > 0 {
			flags = append(flags, "--pids-limit", strconv.Itoa(lim.PIDs))
		}
		return flags
	}
}

// SupportsResourceLimits reports whether rt's CLI accepts any resource flag at
// all. Same CLI-surface question as SupportsCapAdd, asked so a caller can warn
// that a configured limit will not be enforced on this runtime instead of
// leaving the operator to assume it was.
func SupportsResourceLimits(rt ContainerRuntime) bool {
	return len(ResourceFlags(rt, ResourceLimits{Memory: "1G"})) > 0
}

// SupportsCapAdd reports whether rt's CLI accepts --cap-add. Same CLI-surface
// question as OCIHardeningFlags, asked separately because a caller granting a
// capability (nmap's NET_RAW) needs to know whether the grant is even
// expressible, not just whether the drops are.
func SupportsCapAdd(rt ContainerRuntime) bool {
	return len(OCIHardeningFlags(rt)) > 0
}

// ociRunArgs builds `docker run` / `podman run` arguments.
func (c *ContainerBackend) ociRunArgs(command string, opts ExecOpts) []string {
	args := append([]string{"run", "--rm"}, OCIHardeningFlags(c.runtime)...)
	args = append(args, ResourceFlags(c.runtime, c.limits)...)
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
	args = append(args, ResourceFlags(c.runtime, c.limits)...)
	if !c.network {
		args = append(args, "--network", "none")
	}
	if opts.Dir != "" {
		args = append(args, "-v", opts.Dir+":/workspace", "-w", "/workspace")
	}
	args = append(args, c.image, "/bin/sh", "-c", command)
	return args
}
