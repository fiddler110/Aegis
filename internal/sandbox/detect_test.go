package sandbox

import (
	"context"
	"strings"
	"testing"
)

// stubProbe returns canned RuntimeInfo per runtime, so detection tests run
// without any container runtime installed.
func stubProbe(avail map[ContainerRuntime]bool) probeFunc {
	return func(_ context.Context, rt ContainerRuntime) RuntimeInfo {
		if avail[rt] {
			return RuntimeInfo{Runtime: rt, Available: true, Version: string(rt) + " 1.0"}
		}
		return RuntimeInfo{Runtime: rt, Available: false, Detail: "not found"}
	}
}

func TestDetectRuntimesCandidatesByOS(t *testing.T) {
	probe := stubProbe(nil)
	cases := map[string][]ContainerRuntime{
		"windows": {RuntimePodman, RuntimeDocker, RuntimeWSL},
		"darwin":  {RuntimeDocker, RuntimePodman, RuntimeAppleContainers},
		"linux":   {RuntimeDocker, RuntimePodman},
	}
	for goos, want := range cases {
		got := detectRuntimes(context.Background(), goos, probe)
		if len(got) != len(want) {
			t.Fatalf("%s: got %d runtimes, want %d", goos, len(got), len(want))
		}
		for i, rt := range want {
			if got[i].Runtime != rt {
				t.Errorf("%s: runtime[%d] = %q, want %q", goos, i, got[i].Runtime, rt)
			}
		}
	}
}

func TestDetectBestPicksFirstAvailableInPriority(t *testing.T) {
	// Docker and podman available; priority puts podman first.
	probe := stubProbe(map[ContainerRuntime]bool{RuntimeDocker: true, RuntimePodman: true})
	rt, ok := detectBest(context.Background(), "linux", []ContainerRuntime{RuntimePodman, RuntimeDocker}, probe)
	if !ok || rt != RuntimePodman {
		t.Fatalf("got (%q, %v), want (podman, true)", rt, ok)
	}
}

// TestDetectBestDefaultWindowsPrefersPodman pins the Windows default order to
// podman → docker → wslc. wslc is last deliberately: it carries neither the
// hardening flags nor the persistent-container surface docker and podman do,
// and cannot build the scanner images at all, so preferring it on a machine
// that has a real engine produced failures that read as broken scanners.
func TestDetectBestDefaultWindowsPrefersPodman(t *testing.T) {
	probe := stubProbe(map[ContainerRuntime]bool{RuntimeWSL: true, RuntimeDocker: true, RuntimePodman: true})
	rt, ok := detectBest(context.Background(), "windows", nil, probe)
	if !ok || rt != RuntimePodman {
		t.Fatalf("got (%q, %v), want (podman, true)", rt, ok)
	}
}

func TestDetectBestSkipsUnavailable(t *testing.T) {
	// podman unavailable -> falls through to docker, still ahead of wslc.
	probe := stubProbe(map[ContainerRuntime]bool{RuntimeDocker: true, RuntimeWSL: true})
	rt, ok := detectBest(context.Background(), "windows", nil, probe)
	if !ok || rt != RuntimeDocker {
		t.Fatalf("got (%q, %v), want (docker, true)", rt, ok)
	}
}

// wslc is still reachable — last, not removed. A Windows machine with only
// Windows Containers must still auto-detect something.
func TestDetectBestWindowsFallsBackToWSL(t *testing.T) {
	probe := stubProbe(map[ContainerRuntime]bool{RuntimeWSL: true})
	rt, ok := detectBest(context.Background(), "windows", nil, probe)
	if !ok || rt != RuntimeWSL {
		t.Fatalf("got (%q, %v), want (wslc, true)", rt, ok)
	}
}

func TestDetectBestNoneAvailable(t *testing.T) {
	probe := stubProbe(nil)
	if rt, ok := detectBest(context.Background(), "linux", nil, probe); ok {
		t.Fatalf("got (%q, true), want (\"\", false)", rt)
	}
}

func TestParseRuntimesAliasAndBlanks(t *testing.T) {
	got := ParseRuntimes([]string{"wsl", "", "docker", "  "})
	want := []ContainerRuntime{RuntimeWSL, RuntimeDocker}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReportMarksAutoPickAndAvailability(t *testing.T) {
	old := probeRuntime
	probeRuntime = stubProbe(map[ContainerRuntime]bool{RuntimeDocker: true})
	defer func() { probeRuntime = old }()

	out := Report(context.Background(), []ContainerRuntime{RuntimeDocker, RuntimePodman})
	if !strings.Contains(out, "docker") || !strings.Contains(out, "yes") {
		t.Errorf("expected docker available in report:\n%s", out)
	}
	if !strings.Contains(out, "←") {
		t.Errorf("expected an auto-pick marker:\n%s", out)
	}
}

func TestReportNoRuntimeNote(t *testing.T) {
	old := probeRuntime
	probeRuntime = stubProbe(nil)
	defer func() { probeRuntime = old }()

	out := Report(context.Background(), nil)
	if !strings.Contains(out, "fall back to local") {
		t.Errorf("expected fallback note when no runtime available:\n%s", out)
	}
}
