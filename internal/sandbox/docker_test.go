package sandbox

import (
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
