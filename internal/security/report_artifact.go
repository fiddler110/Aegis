package security

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ReportArtifactDir is the directory every security scan report is persisted
// under, parallel to SBOMArtifactPath's .aegis/sbom.cdx.json — so a scan's
// findings survive the run that produced them instead of only existing in
// whatever ephemeral output captured it (terminal scrollback, a model turn).
func ReportArtifactDir(dir string) string {
	return filepath.Join(dir, ".aegis", "security")
}

// ReportArtifactPath is where WriteReportArtifact persists one named report
// (e.g. "scan", "image", "network", "dast") under dir.
func ReportArtifactPath(dir, name string) string {
	return filepath.Join(ReportArtifactDir(dir), name+".json")
}

// WriteReportArtifact persists rep as JSON under ReportArtifactPath(dir,
// name), overwriting on every run — the same "keep the latest artifact, not
// a growing history" posture WriteSBOMArtifact already uses. Best-effort: a
// write failure here must never fail the scan whose result is already held
// in memory and about to be returned to the caller.
func WriteReportArtifact(dir, name string, rep Report) {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return
	}
	path := ReportArtifactPath(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
