package security

import (
	"strings"
	"testing"
)

// TestScannerDefaultEnabledSplit is the P11.3 regression: opengrep is the
// new default SAST engine, semgrep and the four language-targeted engines
// are opt-in, and every scanner that predates this feature stays
// default-enabled (a flipped zero-value here would silently disable most of
// the existing scan surface).
func TestScannerDefaultEnabledSplit(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"opengrep", true},
		{"semgrep", false},
		{"gosec", false},
		{"bandit", false},
		{"brakeman", false},
		{"njsscan", false},
		{"trivy", true},
		{"gitleaks", true},
		{"trufflehog", false},
		{"kubescape", true},
		{"hadolint", true},
		{"grype", true},
		{"dockle", true},
		{"osv-scanner", true},
		{"syft", true},
	}
	for _, c := range cases {
		d, ok := DescriptorFor(c.name)
		if !ok {
			t.Errorf("no descriptor for %q", c.name)
			continue
		}
		if d.DefaultEnabled != c.want {
			t.Errorf("%s.DefaultEnabled = %v, want %v", c.name, d.DefaultEnabled, c.want)
		}
	}
}

func TestDefaultScannersIncludesSASTDepth(t *testing.T) {
	names := map[string]bool{}
	for _, sc := range DefaultScanners() {
		names[sc.Name()] = true
	}
	for _, want := range []string{"opengrep", "semgrep", "gosec", "bandit", "brakeman", "njsscan"} {
		if !names[want] {
			t.Errorf("DefaultScanners() missing %q: %v", want, names)
		}
	}
}

// TestSASTScanArgsPinsPacksNeverAuto is the P11.3 reproducibility/supply-
// chain requirement: rule packs must be pinned explicitly, never resolved
// via semgrep/opengrep's own "auto" registry lookup.
func TestSASTScanArgsPinsPacksNeverAuto(t *testing.T) {
	args := sastScanArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{"--sarif", "--quiet", "--config p/owasp-top-ten", "--config p/security-audit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("sastScanArgs() = %q, missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "auto") {
		t.Errorf("sastScanArgs() must never use --config auto, got %q", joined)
	}
}
