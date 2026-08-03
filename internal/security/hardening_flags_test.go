package security

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// TestEveryContainerRunnerHandlesWSLC is the regression test for a live failure
// that made container-method scanning fail *completely* on Windows whenever
// DetectBest picked wslc — which it does by preference there.
//
// wslc presents a Docker-style CLI but does not accept the OCI hardening flags,
// so every run died on "Argument name was not recognized for the current
// command: '--cap-drop=ALL'". internal/sandbox had always known this and handled
// it for its own backend; internal/security carried a second copy of the rule
// that excluded only Apple Containers, so all 15 scanners reported "the tool is
// missing from the image or cannot start" against a perfectly good image.
//
// The fix is one exported helper, and this asserts every runner in this package
// goes through it — a sixth runner added with its own literal flags is exactly
// how this comes back.
func TestEveryContainerRunnerHandlesWSLC(t *testing.T) {
	const image = "localhost/img:v1"
	runners := map[string][]string{
		"multiscanner scan": containerRunArgs(sandbox.RuntimeWSL, image, "/work", "gitleaks"),
		"netscanner":        netscannerRunArgs(sandbox.RuntimeWSL, image, "nmap", "-sV"),
		"gosec warm phase":  goModuleWarmArgs(sandbox.RuntimeWSL, image, "/work"),
		"zap":               zapContainerRunArgs(sandbox.RuntimeWSL, image, "/work"),
	}
	for name, args := range runners {
		joined := strings.Join(args, " ")
		for _, flag := range []string{"--cap-drop", "--security-opt", "--cap-add"} {
			if strings.Contains(joined, flag) {
				t.Errorf("%s passes %s to wslc, which rejects it outright: %v", name, flag, args)
			}
		}
	}

	// And the flags must still be present for the runtimes that do take them —
	// dropping them everywhere would "fix" this test by removing the hardening.
	for name, args := range map[string][]string{
		"multiscanner scan": containerRunArgs(sandbox.RuntimePodman, image, "/work", "gitleaks"),
		"netscanner":        netscannerRunArgs(sandbox.RuntimePodman, image, "nmap", "-sV"),
		"gosec warm phase":  goModuleWarmArgs(sandbox.RuntimePodman, image, "/work"),
		"zap":               zapContainerRunArgs(sandbox.RuntimePodman, image, "/work"),
	} {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--cap-drop=ALL") || !strings.Contains(joined, "--security-opt=no-new-privileges") {
			t.Errorf("%s dropped its hardening flags on podman: %v", name, args)
		}
	}

	// nmap's capability grant has to follow the same rule: expressible on
	// podman, absent on wslc rather than passed and rejected.
	if !strings.Contains(strings.Join(netscannerRunArgs(sandbox.RuntimePodman, image, "nmap"), " "), "--cap-add=NET_RAW") {
		t.Error("nmap lost NET_RAW on podman")
	}
}
