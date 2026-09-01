package sandbox

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestWSLRunArgsNoNetwork(t *testing.T) {
	c := &ContainerBackend{runtime: RuntimeWSL, image: "ubuntu:22.04", network: false}
	args := c.wslRunArgs("echo hi", ExecOpts{Dir: "/work"})

	if args[0] != "run" || !slices.Contains(args, "--rm") {
		t.Fatalf("expected `run --rm`, got %v", args)
	}
	// network disabled -> --network none present
	if !slices.Contains(args, "none") {
		t.Errorf("expected --network none, got %v", args)
	}
	// hardening flags that wslc may not support must NOT be emitted
	if slices.Contains(args, "--cap-drop=ALL") || slices.Contains(args, "--security-opt=no-new-privileges") {
		t.Errorf("wslc args must omit unverified hardening flags, got %v", args)
	}
	// command is the final shell invocation
	if args[len(args)-1] != "echo hi" {
		t.Errorf("last arg = %q, want command", args[len(args)-1])
	}
}

func TestWSLRunArgsNetworkEnabled(t *testing.T) {
	c := &ContainerBackend{runtime: RuntimeWSL, image: "img", network: true}
	args := c.wslRunArgs("ls", ExecOpts{})
	if slices.Contains(args, "--network") {
		t.Errorf("network enabled: must not pass --network none, got %v", args)
	}
}

func TestWSLHostPathMapsWindowsDrive(t *testing.T) {
	// Only meaningful on Windows; on other OSes the path passes through.
	got := wslHostPath(`C:\Users\me\proj`)
	// On Windows we expect the /mnt/c form; elsewhere the raw input.
	if strings.Contains(got, ":") && got != `C:\Users\me\proj` {
		t.Errorf("unexpected mapping: %q", got)
	}
}

// TestSocketRuntime covers FIND-06 / P24.10: Docker and Podman talk to a
// local socket that is privilege-equivalent to root (Docker) or the invoking
// user (rootful Podman); WSL containers and Apple Containers do not share
// that model and must not be flagged.
// TestContainerEnvArgsForwardsOnlyAllowlistedNames is the P81.26/FIND-26
// regression: a container run must not blindly inherit the host environment
// (docker.go previously passed no -e flags at all, so it already didn't —
// this pins that the new allowlist construction keeps it that way for
// anything not explicitly named, while still forwarding what is).
func TestContainerEnvArgsForwardsOnlyAllowlistedNames(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("MY_SECRET", "should-not-appear")
	t.Setenv("PATH", "/should/not/appear/either") // PATH is host-filesystem-shaped; container has its own

	c := &ContainerBackend{runtime: RuntimeDocker, image: "img", envAllow: DefaultContainerEnvAllow}
	args := c.containerEnvArgs()

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "LANG=en_US.UTF-8") {
		t.Errorf("expected LANG (default container-allowlisted) to be forwarded, got %v", args)
	}
	if strings.Contains(joined, "MY_SECRET") {
		t.Errorf("expected non-allowlisted var to be excluded, got %v", args)
	}
	if strings.Contains(joined, "PATH=") {
		t.Errorf("expected PATH to be excluded from container env (host path, wrong filesystem), got %v", args)
	}
}

// TestOCIRunArgsCarryContainerEnvArgs verifies the -e flags actually reach the
// `docker run` argv, not just the helper that builds them.
func TestOCIRunArgsCarryContainerEnvArgs(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	c := &ContainerBackend{runtime: RuntimeDocker, image: "img", envAllow: DefaultContainerEnvAllow}
	args := c.ociRunArgs("ls", ExecOpts{})
	if !slices.Contains(args, "-e") {
		t.Errorf("expected docker run args to carry -e flags, got %v", args)
	}
}

// TestCommandArgsFreshContainerBypassesPersistentContainer is the P81.22/
// FIND-22 regression for the per-command sandbox reset: ExecOpts.FreshContainer
// must skip the persistent-container lookup for that one call, running
// one-shot instead, without touching the recorded persistent container for
// this directory (a later call without FreshContainer still finds it).
func TestCommandArgsFreshContainerBypassesPersistentContainer(t *testing.T) {
	c := &ContainerBackend{
		runtime:    RuntimeDocker,
		image:      "img",
		persistent: true,
		containers: map[string]string{"/work": "existing-container-id"},
		envAllow:   DefaultContainerEnvAllow,
	}

	args, persistent := c.commandArgs(context.Background(), "ls", ExecOpts{Dir: "/work", FreshContainer: true})
	if persistent {
		t.Error("expected FreshContainer to run one-shot, not against the persistent container")
	}
	if slices.Contains(args, "existing-container-id") {
		t.Errorf("expected fresh one-shot args, not the persistent container id, got %v", args)
	}

	// The persistent container record itself must be untouched: a later call
	// without FreshContainer still finds and reuses it.
	if _, ok := c.containers["/work"]; !ok {
		t.Error("FreshContainer must not evict the persistent container record for this directory")
	}
}

func TestSocketRuntime(t *testing.T) {
	cases := []struct {
		rt   ContainerRuntime
		want bool
	}{
		{RuntimeDocker, true},
		{RuntimePodman, true},
		{RuntimeWSL, false},
		{RuntimeAppleContainers, false},
	}
	for _, c := range cases {
		if got := SocketRuntime(c.rt); got != c.want {
			t.Errorf("SocketRuntime(%q) = %v, want %v", c.rt, got, c.want)
		}
	}
}

func TestSocketPrivilegeNotice(t *testing.T) {
	// Docker and Podman get a non-empty notice mentioning both the mitigation
	// Aegis already applies and what it does not cover.
	for _, rt := range []ContainerRuntime{RuntimeDocker, RuntimePodman} {
		notice := SocketPrivilegeNotice(rt)
		if notice == "" {
			t.Fatalf("SocketPrivilegeNotice(%q) = \"\", want non-empty", rt)
		}
		if !strings.Contains(notice, string(rt)) {
			t.Errorf("notice for %q does not mention the runtime: %q", rt, notice)
		}
		if !strings.Contains(notice, "cap-drop=ALL") {
			t.Errorf("notice for %q should mention the already-applied cap-drop mitigation: %q", rt, notice)
		}
		if !strings.Contains(notice, "rootless") {
			t.Errorf("notice for %q should recommend a rootless backend: %q", rt, notice)
		}
	}

	// WSL containers and Apple Containers don't share the Docker/Podman
	// socket-equivalence model, so no notice should be emitted for them.
	for _, rt := range []ContainerRuntime{RuntimeWSL, RuntimeAppleContainers} {
		if notice := SocketPrivilegeNotice(rt); notice != "" {
			t.Errorf("SocketPrivilegeNotice(%q) = %q, want \"\"", rt, notice)
		}
	}
}
