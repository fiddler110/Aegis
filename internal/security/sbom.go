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
	if method == MethodContainer {
		out, err := runContainerImage(ctx, rt, image, dir, "dir:/src", "-o", SBOMFormat)
		return out, method, err
	}
	out, err := runJSON(ctx, dir, "syft", "dir:.", "-o", SBOMFormat)
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
