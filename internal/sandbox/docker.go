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
	RuntimeAppleContainers ContainerRuntime = "container" // macOS Apple Containers (via `container` CLI)
)

// ContainerBackend runs commands inside a container using whichever container
// runtime is available (Docker, Podman, or Apple Containers on macOS). The
// workspace is bind-mounted so file tools continue to work on the host.
type ContainerBackend struct {
	runtime ContainerRuntime
	image   string
	network bool
}

// ContainerOpts configures the container sandbox.
type ContainerOpts struct {
	Image   string           // container image (default "ubuntu:22.04")
	Network bool             // allow network access inside the container
	Prefer  ContainerRuntime // force a specific runtime; empty = auto-detect
}

// NewContainerBackend creates a container sandbox, auto-detecting the best
// available runtime. Returns ErrNoContainerRuntime if none is found.
func NewContainerBackend(opts ContainerOpts) (*ContainerBackend, error) {
	rt, err := detectRuntime(opts.Prefer)
	if err != nil {
		return nil, err
	}
	if opts.Image == "" {
		opts.Image = "ubuntu:22.04"
	}
	return &ContainerBackend{runtime: rt, image: opts.Image, network: opts.Network}, nil
}

// ErrNoContainerRuntime is returned when no container engine is available.
var ErrNoContainerRuntime = fmt.Errorf("sandbox: no container runtime found (tried docker, podman, apple containers)")

// detectRuntime probes for available container runtimes. If prefer is set and
// available, it is used; otherwise the first available runtime wins.
func detectRuntime(prefer ContainerRuntime) (ContainerRuntime, error) {
	if prefer != "" {
		if runtimeAvailable(prefer) {
			return prefer, nil
		}
		return "", fmt.Errorf("sandbox: preferred runtime %q is not available", prefer)
	}

	// Probe order: Docker first (most common), then Podman, then Apple Containers.
	candidates := []ContainerRuntime{RuntimeDocker, RuntimePodman}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates, RuntimeAppleContainers)
	}
	for _, rt := range candidates {
		if runtimeAvailable(rt) {
			return rt, nil
		}
	}
	return "", ErrNoContainerRuntime
}

// runtimeAvailable checks whether a container runtime's CLI is on PATH and
// responds to a basic info/version command.
func runtimeAvailable(rt ContainerRuntime) bool {
	bin := string(rt)
	switch rt {
	case RuntimeDocker, RuntimePodman:
		err := exec.Command(bin, "info").Run()
		return err == nil
	case RuntimeAppleContainers:
		err := exec.Command(bin, "list").Run()
		return err == nil
	default:
		return false
	}
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
	default:
		return c.ociRunArgs(command, opts)
	}
}

// ociRunArgs builds `docker run` / `podman run` arguments.
func (c *ContainerBackend) ociRunArgs(command string, opts ExecOpts) []string {
	args := []string{"run", "--rm", "--cap-drop=ALL", "--security-opt=no-new-privileges"}
	if !c.network {
		args = append(args, "--network", "none")
	}
	if opts.Dir != "" {
		args = append(args, "-v", opts.Dir+":/workspace", "-w", "/workspace")
	}
	args = append(args, c.image, "/bin/sh", "-c", command)
	return args
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
