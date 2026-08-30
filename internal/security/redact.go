package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/redact"
)

// gitleaksSecretDoc mirrors the subset of gitleaks' JSON report fields needed
// to redact the literal matched secret text, in addition to the fields
// parseGitleaks already extracts. Kept as a separate unexported struct
// (rather than widening parseGitleaks's doc type) so ScanText/parseGitleaks's
// existing behavior and callers (notably gitpr.go's FIND-13 usage) are
// completely untouched by this addition.
type gitleaksSecretDoc struct {
	RuleID      string `json:"RuleID"`
	Description string `json:"Description"`
	File        string `json:"File"`
	StartLine   int    `json:"StartLine"`
	Secret      string `json:"Secret"`
	Match       string `json:"Match"`
}

// parseGitleaksWithSecrets is a sibling of parseGitleaks that additionally
// captures the literal matched-secret substring (gitleaks' "Secret" field,
// falling back to "Match") for each finding, needed by RedactText to mask
// the exact text. It does not replace or call parseGitleaks — both parse the
// same report shape independently so parseGitleaks's existing signature and
// behavior (used by gitpr.go's FIND-13 secret check) stay exactly as-is.
func parseGitleaksWithSecrets(data []byte) ([]gitleaksSecretDoc, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	var doc []gitleaksSecretDoc
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return nil, fmt.Errorf("parse gitleaks output: %w", err)
	}
	return doc, nil
}

// RedactText runs the same gitleaks-backed secret detection ScanText uses
// (P24.6 / FIND-13) against an arbitrary in-memory string — here, tool-read
// file content that's about to be sent to a cloud model provider (P24.12 /
// FIND-09) — and returns a copy with every detected secret substring masked
// as "[REDACTED:<RuleID>]", plus the findings that drove the redaction.
//
// Replacement is done by exact substring match rather than reconstructing
// byte offsets from gitleaks' line/column report fields: real secrets are
// near-unique high-entropy strings, so a straightforward strings.ReplaceAll
// per finding is robust enough in practice and far simpler than offset
// bookkeeping across potential line-ending/encoding differences between what
// gitleaks scanned on disk and the in-memory text.
//
// The scrub is two layers, and the order matters. internal/redact runs first
// and unconditionally: it is a dependency-free, in-process pattern set (PEM
// keys, AWS IDs, sk- keys, GitHub/Slack tokens, JWTs, bearer headers) already
// trusted at the MCP outbound boundary, the audit trail and transcript export.
// gitleaks then runs on top when it is on PATH, catching what the pattern set
// does not.
//
// The layering closes a silent hole. This function used to open with a bare
// `if !lookPath("gitleaks") { return text, nil, nil }`, so an operator who set
// RedactSecrets on a cloud provider, on a host without gitleaks, got exactly
// zero redaction — no warning, and no way to tell that state apart from
// "scanned, found nothing." The fail-open posture below is deliberate and
// stays: a scrubbing pass must never block a tool result from reaching the
// model. What it now fails open *to* is the in-process floor rather than
// nothing at all.
//
// Running the floor first also means fewer raw secrets reach disk: gitleaks
// scans a temp file, and it now writes the already-floor-redacted text.
func RedactText(ctx context.Context, text string) (string, []Finding, error) {
	redacted, findings := redactInProcess(text)

	if !lookPath("gitleaks") {
		return redacted, findings, nil
	}

	dir, err := os.MkdirTemp("", "gitleaks-redact-*")
	if err != nil {
		return redacted, findings, nil
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "content.txt"), []byte(redacted), 0o600); err != nil {
		return redacted, findings, nil
	}

	report, err := runGitleaksHostDirReport(ctx, dir)
	if err != nil {
		return redacted, findings, nil
	}

	docs, err := parseGitleaksWithSecrets(report)
	if err != nil || len(docs) == 0 {
		return redacted, findings, nil
	}

	for _, d := range docs {
		secret := d.Secret
		if secret == "" {
			secret = d.Match
		}
		if secret != "" && strings.Contains(redacted, secret) {
			placeholder := fmt.Sprintf("[REDACTED:%s]", firstNonEmpty(d.RuleID, "secret"))
			redacted = strings.ReplaceAll(redacted, secret, placeholder)
		}
		findings = append(findings, Finding{
			Tool:        "gitleaks",
			RuleID:      d.RuleID,
			Severity:    SevHigh, // leaked secrets are high severity by default
			Title:       firstNonEmpty(d.Description, "potential secret"),
			Location:    fmt.Sprintf("%s:%d", filepath.ToSlash(d.File), d.StartLine),
			Remediation: "rotate the exposed credential and remove it from the codebase",
		})
	}
	return redacted, findings, nil
}

// redactInProcess applies the internal/redact pattern set and reports one
// Finding per class that matched, so the count travels with the text.
//
// The findings exist so callers can tell "scrubbed, nothing found" apart from
// "never ran" — the property redact.Text's own doc comment argues for, and the
// reason this layer is not silent. Location is the class name rather than a
// file:line: the pattern set works on an in-memory string with no line
// bookkeeping, and inventing an offset would be worse than naming the class.
func redactInProcess(text string) (string, []Finding) {
	classes := redact.Classes(text)
	out, n := redact.Text(text)
	if n == 0 || len(classes) == 0 {
		return out, nil
	}
	findings := make([]Finding, 0, len(classes))
	for _, c := range classes {
		findings = append(findings, Finding{
			Tool:        "internal/redact",
			RuleID:      c,
			Severity:    SevHigh,
			Title:       "potential secret (" + c + ")",
			Location:    c,
			Remediation: "rotate the exposed credential and remove it from the codebase",
		})
	}
	return out, findings
}
