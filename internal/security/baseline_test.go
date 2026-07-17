package security

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeBaseline(t *testing.T, dir, content string) {
	t.Helper()
	path := BaselinePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadBaselineMissingFileReturnsNil(t *testing.T) {
	b, err := LoadBaseline(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b != nil {
		t.Fatalf("expected nil baseline for a missing file, got %+v", b)
	}
}

func TestLoadBaselineParsesEntries(t *testing.T) {
	dir := t.TempDir()
	writeBaseline(t, dir, `
suppressions:
  - rule_id: "CVE-2024-1111"
    location: "package-lock.json"
    reason: "no fix upstream yet, mitigated by network policy"
    expires: "2099-01-01"
    added_by: "scott"
`)
	b, err := LoadBaseline(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || len(b.Suppressions) != 1 {
		t.Fatalf("expected 1 suppression entry, got %+v", b)
	}
	if b.Suppressions[0].RuleID != "CVE-2024-1111" {
		t.Errorf("got %+v", b.Suppressions[0])
	}
}

func TestLoadBaselineMalformedYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeBaseline(t, dir, "suppressions: [this is not valid: yaml: at all")
	if _, err := LoadBaseline(dir); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestBaselineApplySuppressesMatchingActiveEntry(t *testing.T) {
	b := &Baseline{Suppressions: []SuppressionEntry{
		{RuleID: "CVE-2024-1111", Location: "package-lock.json", Reason: "accepted risk", Expires: "2099-01-01"},
	}}
	findings := []Finding{
		{Tool: "osv-scanner", RuleID: "CVE-2024-1111", Location: "left-pad@1.0.0 (package-lock.json)"},
		{Tool: "opengrep", RuleID: "other-rule", Location: "app.go:1"},
	}
	kept, suppressed, expired, invalid := b.Apply(findings, time.Now())
	if len(kept) != 1 || kept[0].RuleID != "other-rule" {
		t.Fatalf("expected only the unrelated finding kept, got %+v", kept)
	}
	if len(suppressed) != 1 || suppressed[0].RuleID != "CVE-2024-1111" {
		t.Fatalf("expected the matching finding suppressed, got %+v", suppressed)
	}
	if len(expired) != 0 || len(invalid) != 0 {
		t.Fatalf("expected no expired/invalid entries, got expired=%v invalid=%v", expired, invalid)
	}
}

func TestBaselineApplyDoesNotSuppressExpiredEntry(t *testing.T) {
	b := &Baseline{Suppressions: []SuppressionEntry{
		{RuleID: "CVE-2024-1111", Reason: "accepted risk", Expires: "2000-01-01"},
	}}
	findings := []Finding{{Tool: "trivy", RuleID: "CVE-2024-1111", Location: "go.sum"}}
	kept, suppressed, expired, invalid := b.Apply(findings, time.Now())
	if len(kept) != 1 {
		t.Fatalf("expected the finding to come back into view once expired, got kept=%v", kept)
	}
	if len(suppressed) != 0 {
		t.Fatalf("expected nothing suppressed, got %v", suppressed)
	}
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired entry reported, got %v", expired)
	}
	if len(invalid) != 0 {
		t.Fatalf("expected no invalid entries, got %v", invalid)
	}
}

func TestBaselineApplyFlagsInvalidEntryMissingExpires(t *testing.T) {
	b := &Baseline{Suppressions: []SuppressionEntry{
		{RuleID: "CVE-2024-1111", Reason: "accepted risk"}, // no Expires
	}}
	findings := []Finding{{Tool: "trivy", RuleID: "CVE-2024-1111", Location: "go.sum"}}
	kept, suppressed, expired, invalid := b.Apply(findings, time.Now())
	if len(kept) != 1 || len(suppressed) != 0 {
		t.Fatalf("a missing-expires entry must never suppress, got kept=%v suppressed=%v", kept, suppressed)
	}
	if len(invalid) != 1 {
		t.Fatalf("expected 1 invalid entry reported, got %v", invalid)
	}
	if len(expired) != 0 {
		t.Fatalf("expected no expired entries, got %v", expired)
	}
}

func TestBaselineApplyFlagsInvalidEntryUnparseableExpires(t *testing.T) {
	b := &Baseline{Suppressions: []SuppressionEntry{
		{RuleID: "CVE-2024-1111", Reason: "accepted risk", Expires: "not-a-date"},
	}}
	findings := []Finding{{Tool: "trivy", RuleID: "CVE-2024-1111", Location: "go.sum"}}
	kept, _, _, invalid := b.Apply(findings, time.Now())
	if len(kept) != 1 {
		t.Fatalf("expected the finding kept (not suppressed), got %v", kept)
	}
	if len(invalid) != 1 {
		t.Fatalf("expected 1 invalid entry, got %v", invalid)
	}
}

func TestBaselineApplyLocationScoping(t *testing.T) {
	b := &Baseline{Suppressions: []SuppressionEntry{
		{RuleID: "rule-x", Location: "vendor/", Reason: "vendored code, not ours", Expires: "2099-01-01"},
	}}
	findings := []Finding{
		{Tool: "opengrep", RuleID: "rule-x", Location: "vendor/lib/foo.go:10"},
		{Tool: "opengrep", RuleID: "rule-x", Location: "internal/app.go:10"},
	}
	kept, suppressed, _, _ := b.Apply(findings, time.Now())
	if len(suppressed) != 1 || suppressed[0].Location != "vendor/lib/foo.go:10" {
		t.Fatalf("expected only the vendor/ location suppressed, got %+v", suppressed)
	}
	if len(kept) != 1 || kept[0].Location != "internal/app.go:10" {
		t.Fatalf("expected the non-vendor finding kept, got %+v", kept)
	}
}

func TestBaselineApplyNoEntriesKeepsEverything(t *testing.T) {
	findings := []Finding{{Tool: "opengrep", RuleID: "r", Location: "a.go:1"}}
	var b *Baseline
	kept, suppressed, expired, invalid := b.Apply(findings, time.Now())
	if len(kept) != 1 || len(suppressed) != 0 || len(expired) != 0 || len(invalid) != 0 {
		t.Fatalf("nil baseline should be a no-op, got kept=%v suppressed=%v", kept, suppressed)
	}
}
