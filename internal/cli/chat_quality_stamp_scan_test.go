package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/drive"
)

// writeSuiteFile writes one suite file into dir.
func writeSuiteFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestQualityStampInvisibleToScanners covers requirement (c): the dot-prefixed
// `.json` stamp must be invisible to the existing suite scanners so it can never
// be mistaken for suite content — it must not change suiteFileCount and must not
// be returned by scanPendingMarkers.
func TestQualityStampInvisibleToScanners(t *testing.T) {
	// Build an .aegis tree with a run dir whose suite files are all clean (no
	// PENDING markers) so scanPendingMarkers starts empty and any leak from the
	// stamp would show up as a spurious hit.
	root := t.TempDir()
	runDir := filepath.Join(root, ".aegis", "security", "threat-model", "stride-app-2026-07-27-1200")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSuiteFile(t, runDir, "0-assessment.md", "assessment, no marker\n")
	writeSuiteFile(t, runDir, "3-findings.md", "findings, no marker\n")
	writeSuiteFile(t, runDir, "inventory.yaml", "id: x\n")

	countBefore := suiteFileCount(root)
	pendingBefore := scanPendingMarkers(root)
	if len(pendingBefore) != 0 {
		t.Fatalf("precondition: clean suite must have no pending markers, got %v", pendingBefore)
	}

	// Write the stamp. To be maximally adversarial, prove that even if the JSON
	// body somehow contained the marker literal it wouldn't matter — but the real
	// stamp never does; here we just write the real stamp.
	fp, err := drive.SuiteFingerprint(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := drive.WriteQualityStamp(runDir, fp); err != nil {
		t.Fatal(err)
	}

	countAfter := suiteFileCount(root)
	if countAfter != countBefore {
		t.Errorf("stamp file changed suiteFileCount: before=%d after=%d", countBefore, countAfter)
	}

	pendingAfter := scanPendingMarkers(root)
	if len(pendingAfter) != 0 {
		t.Errorf("stamp file must not appear in scanPendingMarkers, got %v", pendingAfter)
	}
	for _, p := range pendingAfter {
		if filepath.Base(p) == drive.QualityStampFile {
			t.Errorf("stamp file %q leaked into scanPendingMarkers", p)
		}
	}
}
