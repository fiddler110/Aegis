package security

import (
	"context"
	"strings"
	"testing"
)

func TestRedactTextNoGitleaksOnPath(t *testing.T) {
	withEmptyPath(t)

	text := "some file content with nothing interesting"
	redacted, findings, err := RedactText(context.Background(), text)
	if err != nil {
		t.Fatalf("RedactText should never error just because gitleaks is missing, got %v", err)
	}
	if findings != nil {
		t.Errorf("clean text should yield nil findings, got %v", findings)
	}
	if redacted != text {
		t.Errorf("clean text should come back unchanged, got %q", redacted)
	}
}

// TestRedactTextFloorAppliesWithoutGitleaks pins SEC-C. RedactText used to open
// with a bare lookPath check, so an operator who set RedactSecrets on a cloud
// provider, on a host without gitleaks, got zero redaction — silently, and
// indistinguishably from "scanned, found nothing". The in-process floor now runs
// unconditionally, so a secret is masked on that host too.
//
// withEmptyPath is the whole point: this asserts the no-gitleaks path
// specifically, and fails against the pre-fix code with the secret intact.
func TestRedactTextFloorAppliesWithoutGitleaks(t *testing.T) {
	withEmptyPath(t)

	const secret = "AKIA" + "ABCDEFGHIJKLMNOP"
	text := "fixes the deploy script\n\nAWS_ACCESS_KEY_ID=" + secret
	redacted, findings, err := RedactText(context.Background(), text)
	if err != nil {
		t.Fatalf("RedactText: %v", err)
	}
	if strings.Contains(redacted, secret) {
		t.Errorf("secret survived with gitleaks absent: %q", redacted)
	}
	if len(findings) == 0 {
		t.Fatal("expected the floor to report what it redacted; a silent pass is the defect")
	}
	if findings[0].Tool != "internal/redact" {
		t.Errorf("finding.Tool = %q, want internal/redact", findings[0].Tool)
	}
}

// TestRedactTextLive exercises the real gitleaks binary when it's available
// (same posture as TestScanTextLive) rather than skipping entirely.
func TestRedactTextLive(t *testing.T) {
	if !lookPath("gitleaks") {
		t.Skip("gitleaks not installed on PATH")
	}

	t.Run("no secret", func(t *testing.T) {
		text := "chore: tidy up the README wording"
		redacted, findings, err := RedactText(context.Background(), text)
		if err != nil {
			t.Fatalf("RedactText: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("expected no findings for benign text, got %+v", findings)
		}
		if redacted != text {
			t.Errorf("expected benign text unchanged, got %q", redacted)
		}
	})

	t.Run("aws secret", func(t *testing.T) {
		// Same synthetic-looking but gitleaks-detectable AWS access key
		// pattern used by scantext_test.go's TestScanTextLive.
		const secret = "AKIA" + "ABCDEFGHIJKLMNOP"
		text := "fixes the deploy script\n\nAWS_ACCESS_KEY_ID=" + secret
		redacted, findings, err := RedactText(context.Background(), text)
		if err != nil {
			t.Fatalf("RedactText: %v", err)
		}
		if len(findings) == 0 {
			t.Fatalf("expected at least one finding for an embedded AWS key, got none")
		}
		if strings.Contains(redacted, secret) {
			t.Errorf("expected secret to be redacted from output, got %q", redacted)
		}
		// Which layer catches it is not the property under test — an AWS key
		// is in the in-process pattern set, so the floor now masks it before
		// gitleaks ever sees the temp file, and gitleaks correctly reports
		// nothing on the already-masked text. Assert a placeholder from
		// either layer rather than pinning the division of labour, which
		// would break every time the pattern set gains a class.
		if !strings.Contains(redacted, "[REDACTED:") && !strings.Contains(redacted, "[redacted: ") {
			t.Errorf("expected a redaction placeholder in output, got %q", redacted)
		}
	})
}
