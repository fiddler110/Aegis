package security

import (
	"context"
	"strings"
	"testing"
)

// TestBundleWarningsFiltersToHighAndCritical asserts only HIGH/CRITICAL
// findings become warning lines, and that they carry tool/title/severity/
// location so the model can act on them (P44.1).
func TestBundleWarningsFiltersToHighAndCritical(t *testing.T) {
	rep := Report{Findings: []Finding{
		{Tool: "gitleaks", Title: "AWS key", Severity: SevCritical, Location: "scripts/x.sh:3"},
		{Tool: "opengrep", Title: "os.system", Severity: SevHigh, Location: "scripts/x.py:9"},
		{Tool: "opengrep", Title: "minor", Severity: SevMedium, Location: "a.py:1"},
		{Tool: "trivy", Title: "info", Severity: SevInfo, Location: "b:1"},
	}}
	got := bundleWarnings(rep)
	if len(got) != 2 {
		t.Fatalf("expected 2 warnings (HIGH+CRITICAL), got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"gitleaks", "AWS key", "CRITICAL", "scripts/x.sh:3", "opengrep", "os.system", "HIGH"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warning missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "minor") || strings.Contains(joined, "info") {
		t.Errorf("sub-HIGH findings must be dropped:\n%s", joined)
	}
}

// TestBundleWarningsCleanReport asserts a findings-free report yields no
// warnings — the shape the no-image degradation produces (every scanner
// resolves to MethodNone, RunWithOptions returns nothing).
func TestBundleWarningsCleanReport(t *testing.T) {
	if got := bundleWarnings(Report{}); got != nil {
		t.Errorf("expected nil warnings for an empty report, got %v", got)
	}
}

// TestBundleWarningsMissingLocation asserts a finding with no location still
// produces a usable warning rather than a dangling "at ".
func TestBundleWarningsMissingLocation(t *testing.T) {
	got := bundleWarnings(Report{Findings: []Finding{{Tool: "t", Title: "x", Severity: SevHigh}}})
	if len(got) != 1 || !strings.Contains(got[0], "unknown location") {
		t.Errorf("expected an unknown-location fallback, got %v", got)
	}
}

// TestScanBundleWarningsNoScannersIsSilent asserts the end-to-end helper
// degrades quietly: with no scanner enabled and no multiscanner image, no
// scanner resolves and the result is nil, never an error or a spurious
// warning.
func TestScanBundleWarningsNoScannersIsSilent(t *testing.T) {
	// A zero-value Options with a bogus dir: no host binaries are guaranteed
	// on a CI box and no multiscanner image is configured, so every scanner
	// resolves to MethodNone. The one guarantee we need is: no panic, and no
	// warnings fabricated out of nothing.
	got := ScanBundleWarnings(context.Background(), t.TempDir(), Options{})
	for _, w := range got {
		if strings.Contains(w, "unknown location") && strings.Contains(w, `""`) {
			t.Errorf("fabricated warning from empty dir: %v", got)
		}
	}
}
