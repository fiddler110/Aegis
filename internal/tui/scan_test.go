package tui

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdScanImageMissingRefIsUsageError checks the argument-parsing fast
// path that returns before ever touching the daemon client — the client is
// nil here specifically to prove this branch never calls it.
func TestCmdScanImageMissingRefIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "scan", Args: []string{"image"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/scan image` with no ref")
	}
}

// TestCmdScanNetworkMissingTargetIsUsageError mirrors
// TestCmdScanImageMissingRefIsUsageError for the P13.5 `/scan network` branch:
// a bare `/scan network` with no target list must fail before ever touching
// the daemon client.
func TestCmdScanNetworkMissingTargetIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "scan", Args: []string{"network"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/scan network` with no target")
	}
}

// TestScanSelectorTokens is the "/scan trufflehog" vs "/scan <path>"
// dispatch decision: every comma-separated token must resolve via
// security.ResolveSelector for the whole arg to be treated as a scanner
// selector rather than a literal path.
func TestScanSelectorTokens(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"trufflehog", []string{"trufflehog"}},
		{"secrets", []string{"secrets"}},
		{"gitleaks,trufflehog", []string{"gitleaks", "trufflehog"}},
		{"gitleaks, trufflehog", []string{"gitleaks", "trufflehog"}}, // whitespace around comma
		{".", nil},                // literal path, not a selector
		{"src/internal", nil},     // literal path
		{"trufflehog,bogus", nil}, // one bad token invalidates the whole selector list
		{"", nil},
	}
	for _, c := range cases {
		got := scanSelectorTokens(c.raw)
		if len(got) != len(c.want) {
			t.Errorf("scanSelectorTokens(%q) = %v, want %v", c.raw, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("scanSelectorTokens(%q) = %v, want %v", c.raw, got, c.want)
				break
			}
		}
	}
}

// TestCmdScanSelectorSetsScannersField proves the selector branch actually
// reaches the daemon-call path with req.Scanners populated (rather than
// misrouting to req.Path) — using a nil client the same way the usage-error
// tests above do would panic once the code reaches d.client.Scan, so this
// only exercises the branch selection indirectly via scanSelectorTokens
// itself (TestScanSelectorTokens) plus a path-vs-selector sanity check here.
func TestCmdScanSelectorTokensDoNotMisfireOnOrdinaryPaths(t *testing.T) {
	for _, p := range []string{".", "..", "src", "internal/security", "cmd/aegis"} {
		if got := scanSelectorTokens(p); got != nil {
			t.Errorf("scanSelectorTokens(%q) = %v, want nil (ordinary path, not a selector)", p, got)
		}
	}
}

// TestCmdScanListDoesNotTouchDaemonClient proves `/scan list` is resolved
// entirely locally (config.Load + security.Resolve, same as /security
// status) — a nil client would panic if this ever reached d.client.Scan.
func TestCmdScanListDoesNotTouchDaemonClient(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "scan", Args: []string{"list"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	for _, want := range []string{"trufflehog", "secrets", "gitleaks", "SCANNER", "CATEGORY"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output missing %q:\n%s", want, res.Output)
		}
	}
}
