package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Persistent (session-lifetime) container mode, P60.2.
//
// Before this, ContainerBackend.Exec built a `run --rm` for *every* invocation
// and Close() was a no-op because there was nothing to close. The visible cost
// was start latency; the real cost was that no state survived a tool call. An
// installed toolchain, a warmed build cache, a running dev server, a
// half-applied migration — every one of them was discarded the moment the
// command returned, and the next call started from the pristine image. An agent
// could not do the ordinary thing (`npm install`, then `npm test`) without
// collapsing it into a single shell string, which is why the container backend
// was behaviourally *worse* than `local` for multi-step work rather than merely
// slower.
//
// Persistent mode is the shape Orchard Env uses: `run -d` once per workspace
// directory, `exec` per command, teardown on Close(). Explicitly not adopted is
// Orchard's in-pod HTTP agent — it exists to bypass the Kubernetes API server
// across ~1000 sandboxes, and `docker exec` gets the whole benefit on one host.
//
// What it does and does not buy is worth being precise about, because the
// intuitive claim is too strong: filesystem and process state persist, shell
// state does not. Each `exec` is a new process with a new shell, so `cd` and
// `export` still die with the command — exactly as they do between two shell
// tool calls on the `local` backend. What now survives is everything written
// outside the bind mount, everything installed, and anything left running in
// the background.
//
// The honest cost is that `--rm`-per-command is what made the old design
// leak-free, and this trades that for owned state. Three things buy it back:
// every container carries labels a reaper can find, its entrypoint is a bounded
// `sleep` so an orphan exits on its own, and `--rm` still applies to that exit
// so the exit also removes it.

const (
	// persistentLabel marks every container this package starts, so a reaper
	// can find them without matching on names.
	persistentLabel = "aegis.sandbox"
	// persistentOwnerLabel records "<pid>@<hostname>" of the daemon that
	// started the container. The reaper uses it to tell an orphan from a
	// container belonging to another *live* Aegis process on this machine —
	// killing the latter would break a concurrent session.
	persistentOwnerLabel = "aegis.sandbox.owner"

	// DefaultSessionTTL bounds how long a persistent container lives if nobody
	// ever calls Close() — a daemon that is SIGKILLed, a laptop that sleeps and
	// never wakes the process. The container's entrypoint is a `sleep` of this
	// length under `--rm`, so expiry removes it without anything having to run.
	// Long enough not to interrupt a working session, short enough that a
	// forgotten container is not a permanent resident.
	DefaultSessionTTL = 4 * time.Hour

	// maxPersistentContainers caps how many live containers one backend owns.
	// The key is the working directory, and a session normally has one (plus,
	// at most, a few workspace.additional_roots). Beyond the cap a command runs
	// one-shot rather than starting an unbounded number of containers: losing
	// persistence for an unusual directory is a far smaller failure than
	// leaking containers for it.
	maxPersistentContainers = 4
)

// SupportsPersistentContainer reports whether rt's CLI has the verified
// `run -d` / `exec` / `rm -f` surface persistent mode needs.
//
// Same "only where verified" rule as OCIHardeningFlags and ResourceFlags, and
// for the same reason: a flag or subcommand a runtime does not know is not a
// degraded feature, it is a command that fails. docker and podman both document
// the full set. wslc's CLI is Docker-shaped but its detach/exec surface is
// unverified here, and Apple Containers runs each container as its own
// lightweight VM with a different lifecycle model — both keep the per-command
// behavior they have always had.
func SupportsPersistentContainer(rt ContainerRuntime) bool {
	return rt == RuntimeDocker || rt == RuntimePodman
}

// startPersistentArgs builds the `run -d` argument list for the long-lived
// container serving dir.
//
// It mirrors ociRunArgs exactly in posture — same hardening flags, same
// resource limits, same `--network none` unless network is enabled, same
// bind-mount — because persistence must not become a quiet way to get a weaker
// container. The only differences are the ones persistence requires: `-d`, the
// labels a reaper needs, and a bounded `sleep` as the process that holds the
// container open.
func (c *ContainerBackend) startPersistentArgs(dir string) []string {
	args := append([]string{"run", "-d", "--rm"}, OCIHardeningFlags(c.runtime)...)
	args = append(args, ResourceFlags(c.runtime, c.limits)...)
	args = append(args, c.containerEnvArgs()...)
	if !c.network {
		args = append(args, "--network", "none")
	}
	args = append(args,
		"--label", persistentLabel+"=1",
		"--label", persistentOwnerLabel+"="+ownerTag(),
	)
	if dir != "" {
		// Always read-write: the mount is fixed for the container's life and
		// reused across every future `exec` regardless of that command's own
		// capability verdict (P81.10/FIND-10) — see ExecOpts.ReadOnly's doc
		// comment. The secret-exclusion shadow mounts apply here too, since
		// those don't depend on ExecOpts at all.
		args = append(args, "-v", HostMountPath(c.runtime, dir)+":/workspace")
		args = append(args, c.secretShadowArgs(dir, func(p string) string { return HostMountPath(c.runtime, p) })...)
		args = append(args, "-w", "/workspace")
	}
	ttl := c.sessionTTL
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	args = append(args, c.image, "/bin/sh", "-c", "sleep "+strconv.Itoa(int(ttl.Seconds())))
	return args
}

// execPersistentArgs builds the `exec` argument list for one command inside an
// already-running container.
func execPersistentArgs(id, command string, opts ExecOpts) []string {
	args := []string{"exec"}
	if opts.Dir != "" {
		args = append(args, "-w", "/workspace")
	}
	args = append(args, id, "/bin/sh", "-c", command)
	return args
}

// ownerTag identifies the process that owns a container, as "<pid>@<hostname>".
func ownerTag() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return strconv.Itoa(os.Getpid()) + "@" + host
}

// container returns the running container serving dir, starting one if needed.
// The second return reports whether a persistent container is usable at all for
// this call — false means the caller should run the command one-shot.
func (c *ContainerBackend) container(ctx context.Context, dir string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.persistent {
		return "", false
	}
	if id, ok := c.containers[dir]; ok {
		return id, true
	}
	if len(c.containers) >= maxPersistentContainers {
		c.logger.Warn("sandbox: persistent-container cap reached; running this command one-shot",
			"cap", maxPersistentContainers, "dir", dir)
		return "", false
	}
	id, err := c.startContainer(ctx, dir)
	if err != nil {
		// Degrade to the pre-P60.2 behavior rather than failing the command:
		// a one-shot run still executes correctly, it just does not remember.
		// Said once, and persistence stays off for the rest of this backend's
		// life so the warning is not repeated per tool call.
		c.persistent = false
		c.logger.Warn("sandbox: could not start a persistent container; falling back to one container per command (state will not persist between tool calls)",
			"runtime", c.runtime, "err", err)
		return "", false
	}
	if c.containers == nil {
		c.containers = map[string]string{}
	}
	c.containers[dir] = id
	c.logger.Info("sandbox: started persistent container", "runtime", c.runtime, "id", shortID(id), "dir", dir)
	return id, true
}

// startContainer runs the detached container and returns its ID.
func (c *ContainerBackend) startContainer(ctx context.Context, dir string) (string, error) {
	startCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := c.cli.run(startCtx, c.startPersistentArgs(dir))
	if err != nil {
		return "", fmt.Errorf("%s run -d: %w: %s", c.runtime, err, strings.TrimSpace(out))
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return "", fmt.Errorf("%s run -d returned no container id", c.runtime)
	}
	// Some runtimes echo pull progress before the ID; the ID is the last line.
	if lines := strings.Fields(id); len(lines) > 0 {
		id = lines[len(lines)-1]
	}
	return id, nil
}

// forget drops the recorded container for dir, so the next call starts a fresh
// one. Used when an exec reports the container is gone — a TTL expiry, an
// operator's `docker rm`, an engine restart.
func (c *ContainerBackend) forget(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.containers, dir)
}

// containerGone reports whether an exec failure means the container no longer
// exists (as opposed to the command inside it failing, which is a normal result
// this backend reports to the model verbatim).
func containerGone(output string, err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(output + " " + err.Error())
	return strings.Contains(s, "no such container") ||
		strings.Contains(s, "is not running") ||
		strings.Contains(s, "container not running") ||
		strings.Contains(s, "no such object")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// closePersistent tears down every container this backend started. Best effort:
// a container that is already gone (TTL expiry, an operator's `rm`) is not an
// error, and one that refuses to die is logged rather than propagated — Close()
// is called on shutdown paths where returning an error changes nothing.
func (c *ContainerBackend) closePersistent() {
	c.mu.Lock()
	ids := make([]string, 0, len(c.containers))
	for dir, id := range c.containers {
		ids = append(ids, id)
		delete(c.containers, dir)
	}
	c.mu.Unlock()

	for _, id := range ids {
		// Its own context, not the caller's: Close() runs on a shutdown path
		// whose deadline is short (the daemon allows 5s for everything), and a
		// container that outlives the daemon is exactly what the reaper then has
		// to clean up. Bounded anyway — `rm -f` is a kill, not a wait.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := c.cli.run(ctx, []string{"rm", "-f", id})
		cancel()
		if err != nil {
			c.logger.Warn("sandbox: could not remove persistent container",
				"runtime", c.runtime, "id", shortID(id), "err", err, "output", strings.TrimSpace(out))
			continue
		}
		c.logger.Debug("sandbox: removed persistent container", "runtime", c.runtime, "id", shortID(id))
	}
}

// ReapOrphanSandboxes removes persistent sandbox containers left behind by an
// Aegis process that is no longer running, and reports how many it removed.
//
// It is the backstop for the case the TTL entrypoint cannot cover on its own: a
// daemon killed mid-session leaves a container that will not expire for hours.
// The safety property that matters is that it must never kill a container
// belonging to a *live* Aegis process — a second daemon, another session — so
// the owner label carries the starting process's pid and hostname, and a
// container is removed only when that pid is verifiably absent on this same
// host. PID reuse can only make this too conservative (an orphan mistaken for a
// live owner, left to its TTL), never too aggressive.
func ReapOrphanSandboxes(ctx context.Context, rt ContainerRuntime, logger *slog.Logger) (int, error) {
	if !SupportsPersistentContainer(rt) {
		return 0, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	cli := &execCLI{runtime: rt}
	out, err := cli.run(ctx, []string{
		"ps", "-a",
		"--filter", "label=" + persistentLabel + "=1",
		"--format", "{{.ID}} {{.Label \"" + persistentOwnerLabel + "\"}}",
	})
	if err != nil {
		return 0, fmt.Errorf("%s ps: %w: %s", rt, err, strings.TrimSpace(out))
	}

	host, _ := os.Hostname()
	reaped := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		id, owner, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || id == "" {
			continue
		}
		pidStr, ownerHost, ok := strings.Cut(strings.TrimSpace(owner), "@")
		if !ok || ownerHost != host {
			// A container from a different machine (a remote docker context)
			// is not ours to judge: its pid means nothing here.
			continue
		}
		pid, convErr := strconv.Atoi(pidStr)
		if convErr != nil || processAlive(pid) {
			continue
		}
		if rmOut, rmErr := cli.run(ctx, []string{"rm", "-f", id}); rmErr != nil {
			logger.Warn("sandbox: could not reap orphaned container", "id", shortID(id), "err", rmErr, "output", strings.TrimSpace(rmOut))
			continue
		}
		reaped++
		logger.Info("sandbox: reaped orphaned container from a dead Aegis process", "id", shortID(id), "owner_pid", pid)
	}
	return reaped, nil
}

// processAlive reports whether pid names a running process on this host.
//
// Deliberately biased toward "alive": on Windows, and on any error path, it
// says yes, because a false "alive" only postpones a reap to the container's own
// TTL while a false "dead" would kill a working session's sandbox.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return runtime.GOOS == "windows" // FindProcess only fails on Windows when the pid is gone
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// containerCLI is the seam between this backend's lifecycle logic and actually
// spawning a container-runtime process, so the lifecycle (start once, reuse,
// restart a vanished container, tear down on Close) is testable without a
// container runtime installed.
type containerCLI interface {
	run(ctx context.Context, args []string) (string, error)
	stream(ctx context.Context, args []string, emit func(string)) error
}

// execCLI is the real implementation: it shells out to the runtime binary.
type execCLI struct{ runtime ContainerRuntime }

func (e *execCLI) run(ctx context.Context, args []string) (string, error) {
	out, err := exec.CommandContext(ctx, string(e.runtime), args...).CombinedOutput()
	return string(out), err
}

func (e *execCLI) stream(ctx context.Context, args []string, emit func(string)) error {
	cmd := exec.CommandContext(ctx, string(e.runtime), args...)
	w := emitWriter{emit: emit}
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}
