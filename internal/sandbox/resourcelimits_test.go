package sandbox

import (
	"slices"
	"strings"
	"testing"
)

// argValue returns the value following flag in args, or "" when absent.
func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// TestResourceFlagsPerRuntime is the P60.1 CLI-surface guard. A resource flag
// a runtime does not know is not a weaker cap — it is a container that refuses
// to start, which is exactly how the pre-P24 hardening copy killed every wslc
// scanner run. So each runtime gets only the subset verified against its CLI.
func TestResourceFlagsPerRuntime(t *testing.T) {
	lim := ResourceLimits{Memory: "4G", CPUs: "2", PIDs: 1024}

	for _, rt := range []ContainerRuntime{RuntimeDocker, RuntimePodman} {
		args := ResourceFlags(rt, lim)
		if got := argValue(args, "--memory"); got != "4G" {
			t.Errorf("%s --memory = %q, want 4G", rt, got)
		}
		if got := argValue(args, "--cpus"); got != "2" {
			t.Errorf("%s --cpus = %q, want 2", rt, got)
		}
		if got := argValue(args, "--pids-limit"); got != "1024" {
			t.Errorf("%s --pids-limit = %q, want 1024", rt, got)
		}
	}

	// Apple Containers documents -m/--memory and -c/--cpus as run Resource
	// Options; --pids-limit is not among them.
	apple := ResourceFlags(RuntimeAppleContainers, lim)
	if argValue(apple, "--memory") != "4G" || argValue(apple, "--cpus") != "2" {
		t.Errorf("apple containers must carry memory and cpus, got %v", apple)
	}
	if slices.Contains(apple, "--pids-limit") {
		t.Errorf("apple containers CLI has no --pids-limit, got %v", apple)
	}

	// wslc: unverified resource surface, same stance OCIHardeningFlags takes.
	if got := ResourceFlags(RuntimeWSL, lim); len(got) != 0 {
		t.Errorf("wslc must carry no resource flags, got %v", got)
	}
	if SupportsResourceLimits(RuntimeWSL) {
		t.Error("SupportsResourceLimits(wslc) must be false so the daemon can warn the cap is not in force")
	}
	if !SupportsResourceLimits(RuntimeDocker) {
		t.Error("SupportsResourceLimits(docker) must be true")
	}
}

// TestResourceFlagsFieldsAreIndependent: each cap is separately optional, so an
// operator can bound memory without touching CPU, and the zero value restores
// the pre-P60.1 behavior of no flags at all.
func TestResourceFlagsFieldsAreIndependent(t *testing.T) {
	if got := ResourceFlags(RuntimeDocker, ResourceLimits{}); len(got) != 0 {
		t.Errorf("zero limits must emit no flags, got %v", got)
	}
	if !(ResourceLimits{}).Empty() {
		t.Error("zero ResourceLimits must report Empty")
	}
	if (ResourceLimits{PIDs: 1}).Empty() {
		t.Error("a set PIDs cap must not report Empty")
	}
	// A non-positive PIDs count is "uncapped", not "--pids-limit -1".
	if got := ResourceFlags(RuntimeDocker, ResourceLimits{PIDs: -1}); len(got) != 0 {
		t.Errorf("non-positive PIDs must emit no flag, got %v", got)
	}
	got := ResourceFlags(RuntimeDocker, ResourceLimits{Memory: "  512M  "})
	if len(got) != 2 || got[0] != "--memory" || got[1] != "512M" {
		t.Errorf("memory-only limits = %v, want [--memory 512M] with whitespace trimmed", got)
	}
}

// TestRunArgsCarryLimits: the flags have to reach the actual command lines, on
// every path that builds one — a limit that exists only in config is the bug.
func TestRunArgsCarryLimits(t *testing.T) {
	lim := ResourceLimits{Memory: "4G", CPUs: "2", PIDs: 1024}

	oci := (&ContainerBackend{runtime: RuntimeDocker, image: "img", limits: lim}).ociRunArgs("go build ./...", ExecOpts{Dir: "/work"})
	for _, want := range []string{"--memory", "--cpus", "--pids-limit"} {
		if !slices.Contains(oci, want) {
			t.Errorf("docker run args missing %s: %v", want, oci)
		}
	}
	// The hardening flags are a separate axis and must survive alongside.
	if !slices.Contains(oci, "--cap-drop=ALL") {
		t.Errorf("resource flags must not displace the hardening flags: %v", oci)
	}
	// The command still terminates the argv, after the image.
	if oci[len(oci)-1] != "go build ./..." {
		t.Errorf("last arg = %q, want the command", oci[len(oci)-1])
	}
	if i := slices.Index(oci, "img"); i < 0 || i > len(oci)-2 {
		t.Errorf("image must precede the shell invocation: %v", oci)
	}

	apple := (&ContainerBackend{runtime: RuntimeAppleContainers, image: "img", limits: lim}).appleContainerArgs("ls", ExecOpts{})
	if !slices.Contains(apple, "--memory") || slices.Contains(apple, "--pids-limit") {
		t.Errorf("apple args = %v", apple)
	}

	wsl := (&ContainerBackend{runtime: RuntimeWSL, image: "img", limits: lim}).wslRunArgs("ls", ExecOpts{})
	if strings.Contains(strings.Join(wsl, " "), "--memory") {
		t.Errorf("wslc args must stay free of unverified flags: %v", wsl)
	}
}
