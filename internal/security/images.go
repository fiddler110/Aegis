package security

import (
	"context"
	"os/exec"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// runImageCmd runs a scanner binary against an image reference (no working
// directory or bind mount involved, unlike runJSON). A non-zero exit is
// tolerated as long as output was produced, matching runJSON's tolerance for
// scanners that exit non-zero when they find issues.
func runImageCmd(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	if len(out) == 0 && err != nil {
		return nil, err
	}
	return out, nil
}

// runImageScan runs one image-scanning tool either from the host or out of the
// netscanner image, so each scanner below states its arguments once.
//
// The container branch is the P55.7 half: the netscanner runs with network on
// (a registry pull is the job) and no workspace mounted (there is nothing local
// to scan — the subject is a remote reference). Note runNetscannerImage takes no
// directory, which is what makes that second half structural rather than
// remembered.
func runImageScan(ctx context.Context, method Method, rt sandbox.ContainerRuntime, scannerImage, bin string, args ...string) ([]byte, error) {
	if method == MethodContainer {
		return runNetscannerImage(ctx, rt, scannerImage, bin, args...)
	}
	return runImageCmd(ctx, bin, args...)
}

// --- trivy image ---

type trivyImageScanner struct{}

func (trivyImageScanner) Name() string { return "trivy" }
func (trivyImageScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return ResolveNetwork(ctx, "trivy", opts)
}
func (trivyImageScanner) ScanImage(ctx context.Context, ref string, method Method, rt sandbox.ContainerRuntime, scannerImage string) ([]Finding, error) {
	out, err := runImageScan(ctx, method, rt, scannerImage, "trivy", "image", "--format", "sarif", "--quiet", ref)
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "trivy")
}

// --- grype ---

type grypeScanner struct{}

func (grypeScanner) Name() string { return "grype" }
func (grypeScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return ResolveNetwork(ctx, "grype", opts)
}
func (grypeScanner) ScanImage(ctx context.Context, ref string, method Method, rt sandbox.ContainerRuntime, scannerImage string) ([]Finding, error) {
	out, err := runImageScan(ctx, method, rt, scannerImage, "grype", ref, "-o", "sarif")
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "grype")
}

// --- dockle ---

// dockleContainerUnsupported is the one carve-out P55.7 left standing, and it
// is deliberately narrower than the blanket "image scanning can't use a
// container" it replaces.
//
// dockle doesn't need egress — it needs the container ENGINE SOCKET, because it
// inspects an image's layers and config through the local engine rather than
// pulling it itself. That is a third privilege axis: socket access is
// effectively host root, not merely the ability to reach a registry. It could
// live in the netscanner image and run socket-mounted and workspace-free, but
// whether Aegis should mount a container socket at all is a decision that
// deserves to be made on its own rather than arriving as a side effect of
// building a second image.
const dockleContainerUnsupported = "dockle inspects an image through the local container engine, so it needs the engine socket mounted — a privilege level (effectively host root) neither Aegis scanner image is granted, and one that deserves an explicit decision rather than arriving as a side effect. It runs from a host binary instead (`aegis security install dockle`)."

type dockleScanner struct{}

func (dockleScanner) Name() string { return "dockle" }
func (dockleScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	method, rt, image, reason := ResolveNetwork(ctx, "dockle", opts)
	if method == MethodContainer {
		// Unreachable while dockle is in multiscannerExcludedTools and absent
		// from netscannerTools — kept because "dockle resolved to a container"
		// must fail with the real reason if either list ever changes, rather
		// than launching a run that cannot see the engine.
		return MethodNone, "", "", dockleContainerUnsupported
	}
	return method, rt, image, reason
}
func (dockleScanner) ScanImage(ctx context.Context, ref string, method Method, _ sandbox.ContainerRuntime, _ string) ([]Finding, error) {
	// dockle inspects an image's layers/config via the local container
	// engine, so ref must already be pulled/available to it (unlike
	// trivy/grype, which can pull directly from a registry).
	out, err := runImageCmd(ctx, "dockle", "-f", "sarif", ref)
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "dockle")
}
