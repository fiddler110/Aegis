package security

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// SBOMFormat is the on-disk format GenerateSBOM produces.
const SBOMFormat = "cyclonedx-json"

// GenerateSBOM produces a CycloneDX SBOM for dir via syft, resolving
// host-vs-container the same way every other scanner does (P11.1/P11.11).
// A MethodNone result carries the reason as err rather than a second return
// value, since callers (grype's SBOM-first path, `aegis scan sbom`) treat
// "no SBOM" as a single failure case rather than something to route on.
func GenerateSBOM(ctx context.Context, dir string, opts Options) ([]byte, Method, error) {
	method, rt, image, reason := Resolve(ctx, "syft", opts)
	if method == MethodNone {
		return nil, MethodNone, errors.New(reason)
	}
	// The same build-artifact exclusion grype's dir: scan uses (P34.11): syft is
	// what catalogs a compiled binary's embedded module list, so excluding those
	// here keeps the persisted SBOM — and the grype run fed from it — scoped to
	// declared dependencies rather than local build output. See
	// scaBuildArtifactExcludes.
	if method == MethodContainer {
		args := append([]string{"dir:/src"}, scaExcludeArgs()...)
		args = append(args, "-o", SBOMFormat)
		// runScannerImage, not runContainerImage: the shared multiscanner image
		// carries a dozen tools and therefore has no ENTRYPOINT, so the binary
		// name has to lead the argument list. Passing bare args made the runtime
		// try to exec "/src/dir:/src" and die with exit 127 — container-mode
		// syft never worked, and nothing noticed, because the only caller that
		// could see it (grype's SBOM-first path) silently falls back to a direct
		// dir: scan on any error. Found by P55.3's syft canary.
		out, err := runScannerImage(ctx, rt, image, dir, opts, "syft", args...)
		return out, method, err
	}
	args := append([]string{"dir:."}, scaExcludeArgs()...)
	args = append(args, "-o", SBOMFormat)
	out, err := runJSON(ctx, dir, "syft", args...)
	return out, method, err
}

// SBOMArtifactPath is where WriteSBOMArtifact persists dir's SBOM.
func SBOMArtifactPath(dir string) string {
	return filepath.Join(dir, ".aegis", "sbom.cdx.json")
}

// WriteSBOMArtifact persists sbom under SBOMArtifactPath(dir) — the "keep the
// SBOM as a persisted supply-chain artifact" half of P11.4. Overwrites on
// every run rather than accumulating a history; best-effort (a write failure
// here shouldn't fail the CVE scan that's using the SBOM already held in
// memory).
func WriteSBOMArtifact(dir string, sbom []byte) {
	if len(sbom) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(SBOMArtifactPath(dir)), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(SBOMArtifactPath(dir), sbom, 0o644)
}
