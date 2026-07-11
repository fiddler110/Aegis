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
		t.Errorf("RedactText with no gitleaks on PATH should return nil findings, got %v", findings)
	}
	if redacted != text {
		t.Errorf("RedactText with no gitleaks on PATH should return text unchanged, got %q", redacted)
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
		if findings[0].Tool != "gitleaks" {
			t.Errorf("finding.Tool = %q, want gitleaks", findings[0].Tool)
		}
		if strings.Contains(redacted, secret) {
			t.Errorf("expected secret to be redacted from output, got %q", redacted)
		}
		if !strings.Contains(redacted, "[REDACTED:") {
			t.Errorf("expected a [REDACTED:...] placeholder in output, got %q", redacted)
		}
	})
}
