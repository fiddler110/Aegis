package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSBOMArtifactPersistsFile(t *testing.T) {
	dir := t.TempDir()
	WriteSBOMArtifact(dir, []byte(`{"bomFormat":"CycloneDX"}`))

	data, err := os.ReadFile(filepath.Join(dir, ".aegis", "sbom.cdx.json"))
	if err != nil {
		t.Fatalf("expected sbom.cdx.json to be written: %v", err)
	}
	if !strings.Contains(string(data), "CycloneDX") {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestWriteSBOMArtifactSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	WriteSBOMArtifact(dir, nil)

	if _, err := os.Stat(filepath.Join(dir, ".aegis", "sbom.cdx.json")); err == nil {
		t.Error("expected no file written for an empty SBOM")
	}
}

// TestGenerateSBOMReturnsResolveError proves GenerateSBOM surfaces the
// resolver's reason as an error rather than panicking or silently returning
// empty bytes. Overrides the real "syft" descriptor for the duration of the
// test (restored after, unlike withTestDescriptor's delete-on-cleanup, which
// would drop the real built-in descriptor for the rest of the test binary)
// so the result is deterministic regardless of whether syft happens to be
// installed on the machine running the tests.
func TestGenerateSBOMReturnsResolveError(t *testing.T) {
	orig := descriptors["syft"]
	descriptors["syft"] = ScannerDescriptor{Name: "syft", Binary: "aegis-does-not-exist-xyz", DefaultEnabled: true}
	t.Cleanup(func() { descriptors["syft"] = orig })

	_, method, err := GenerateSBOM(context.Background(), t.TempDir(), Options{})
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone", method)
	}
	if err == nil {
		t.Fatal("expected an error explaining why no SBOM could be generated")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("err = %q, want mention of not installed", err)
	}
}

func TestDefaultScannersIncludesSCADepth(t *testing.T) {
	names := map[string]bool{}
	for _, sc := range DefaultScanners() {
		names[sc.Name()] = true
	}
	for _, want := range []string{"osv-scanner", "grype"} {
		if !names[want] {
			t.Errorf("DefaultScanners() missing %q: %v", want, names)
		}
	}
}
