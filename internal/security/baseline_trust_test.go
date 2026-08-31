package security

import (
	"strings"
	"testing"
	"time"
)

// withBaselineTrust pins the trust answer for the duration of a test, so the
// gate can be exercised in both directions without writing to the real
// workspace-trust store.
func withBaselineTrust(t *testing.T, trusted bool) {
	t.Helper()
	prev := baselineTrustCheck
	baselineTrustCheck = func(string) bool { return trusted }
	t.Cleanup(func() { baselineTrustCheck = prev })
}

func plantedBaseline(t *testing.T, dir string) {
	t.Helper()
	writeBaseline(t, dir, `
suppressions:
  - rule_id: G404
    reason: "we accept this"
    expires: `+time.Now().AddDate(1, 0, 0).Format("2006-01-02")+`
`)
}

func plantedFindings() []Finding {
	return []Finding{{Tool: "gosec", RuleID: "G404", Severity: SevHigh, Title: "weak random", Location: "main.go:12"}}
}

// P76.3: a baseline read out of an untrusted scan target must suppress
// nothing. The scanner subsystem's whole threat model is a hostile repo, and
// a repo that ships .aegis/security-baseline.yaml covering the vulnerability
// it planted would otherwise hand the operator a clean report.
func TestBaselineFromUntrustedTargetSuppressesNothing(t *testing.T) {
	withBaselineTrust(t, false)
	dir := t.TempDir()
	plantedBaseline(t, dir)

	r := newReport()
	r.Findings = plantedFindings()
	r.applyBaseline(dir)

	if len(r.Findings) != 1 {
		t.Fatalf("untrusted baseline hid a finding: %d kept, want 1", len(r.Findings))
	}
	if len(r.Suppressed) != 0 {
		t.Fatalf("untrusted baseline suppressed %d finding(s), want 0", len(r.Suppressed))
	}
	if len(r.BaselineUntrusted) != 1 {
		t.Fatalf("BaselineUntrusted = %v, want the one ignored entry named", r.BaselineUntrusted)
	}
	out := r.Format()
	if !strings.Contains(out, "Baseline IGNORED") || !strings.Contains(out, "G404") {
		t.Fatalf("report does not say the baseline was ignored or which entry it was:\n%s", out)
	}
	if !strings.Contains(out, "aegis trust") {
		t.Fatalf("report does not name the remedy:\n%s", out)
	}
}

// The other side of the gate: an operator who has trusted the directory keeps
// the accepted-risk workflow P11.8 built, unchanged.
func TestBaselineFromTrustedTargetStillSuppresses(t *testing.T) {
	withBaselineTrust(t, true)
	dir := t.TempDir()
	plantedBaseline(t, dir)

	r := newReport()
	r.Findings = plantedFindings()
	r.applyBaseline(dir)

	if len(r.Findings) != 0 || len(r.Suppressed) != 1 {
		t.Fatalf("trusted baseline did not suppress: %d kept, %d suppressed", len(r.Findings), len(r.Suppressed))
	}
	if len(r.BaselineUntrusted) != 0 {
		t.Fatalf("BaselineUntrusted set for a trusted directory: %v", r.BaselineUntrusted)
	}
	// The disclosure half of P76.3: what was hidden is named, not counted.
	out := r.Format()
	if !strings.Contains(out, "Suppressed by baseline: 1") || !strings.Contains(out, "G404") {
		t.Fatalf("a suppressed finding was hidden rather than disclosed:\n%s", out)
	}
}

// A directory with no baseline file is the common case and must not acquire a
// trust question — untrusted or not, the report says nothing about baselines.
func TestNoBaselineFileIsSilentEvenUntrusted(t *testing.T) {
	withBaselineTrust(t, false)
	r := newReport()
	r.Findings = plantedFindings()
	r.applyBaseline(t.TempDir())

	if len(r.BaselineUntrusted) != 0 || len(r.Findings) != 1 {
		t.Fatalf("a directory with no baseline was treated as having one: %+v", r)
	}
	if strings.Contains(r.Format(), "Baseline") {
		t.Fatalf("report mentions a baseline that does not exist:\n%s", r.Format())
	}
}
