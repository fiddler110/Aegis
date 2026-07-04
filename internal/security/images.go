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

// --- trivy image ---

type trivyImageScanner struct{}

func (trivyImageScanner) Name() string { return "trivy" }
func (trivyImageScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "trivy", opts)
}
func (trivyImageScanner) ScanImage(ctx context.Context, ref string, method Method) ([]Finding, error) {
	out, err := runImageCmd(ctx, "trivy", "image", "--format", "sarif", "--quiet", ref)
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "trivy")
}

// --- grype ---

type grypeScanner struct{}

func (grypeScanner) Name() string { return "grype" }
func (grypeScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "grype", opts)
}
func (grypeScanner) ScanImage(ctx context.Context, ref string, method Method) ([]Finding, error) {
	out, err := runImageCmd(ctx, "grype", ref, "-o", "sarif")
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "grype")
}

// --- dockle ---

type dockleScanner struct{}

func (dockleScanner) Name() string { return "dockle" }
func (dockleScanner) Resolve(ctx context.Context, opts Options) (Method, sandbox.ContainerRuntime, string, string) {
	return Resolve(ctx, "dockle", opts)
}
func (dockleScanner) ScanImage(ctx context.Context, ref string, method Method) ([]Finding, error) {
	// dockle inspects an image's layers/config via the local container
	// engine, so ref must already be pulled/available to it (unlike
	// trivy/grype, which can pull directly from a registry).
	out, err := runImageCmd(ctx, "dockle", "-f", "sarif", ref)
	if err != nil {
		return nil, err
	}
	return ParseSARIF(out, "dockle")
}
