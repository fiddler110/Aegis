package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/security"
)

func runScan(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newScanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// TestScanScannerFlagUnknownSelectorErrors checks --scanner rejects an
// unrecognized name/category before ever attempting to run a scanner.
func TestScanScannerFlagUnknownSelectorErrors(t *testing.T) {
	chdirTemp(t)
	_, err := runScan(t, "--scanner", "not-a-real-scanner")
	if err == nil {
		t.Fatal("expected an error for an unrecognized --scanner value")
	}
}

// TestScanScannerFlagRestrictsSelection checks --scanner trufflehog runs
// only trufflehog (reported skipped in this environment, since it isn't
// installed) — trufflehog resolves to MethodNone immediately with no exec
// call, so this stays fast regardless of what other scanner binaries happen
// to be installed on the machine running the test.
func TestScanScannerFlagRestrictsSelection(t *testing.T) {
	dir := chdirTemp(t)
	out, err := runScan(t, "--scanner", "trufflehog", dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.Contains(out, "trufflehog") {
		t.Errorf("output = %q, want it to mention trufflehog", out)
	}
	for _, other := range []string{"opengrep", "kubescape", "hadolint"} {
		if strings.Contains(out, other) {
			t.Errorf("output = %q, should not mention %q outside the selection", out, other)
		}
	}
}

// TestScanPersistsReportArtifact is the "all security scan results go under
// .aegis/security/" regression for the CLI surface.
func TestScanPersistsReportArtifact(t *testing.T) {
	dir := chdirTemp(t)
	if _, err := runScan(t, "--scanner", "trufflehog", dir); err != nil {
		t.Fatalf("scan: %v", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := security.ReportArtifactPath(abs, "scan")
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("expected report artifact at %s: %v", path, readErr)
	}
	if len(data) == 0 {
		t.Error("report artifact is empty")
	}
}
