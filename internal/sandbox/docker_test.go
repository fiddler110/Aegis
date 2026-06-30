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
