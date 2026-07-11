package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// This mirrors ScanText's exact fail-open posture: if gitleaks isn't on PATH
// (checked via the package's lookPath seam) or anything in the scan pipeline
// fails, the original text is returned unchanged with nil findings and a nil
// error — callers must never gain a hard dependency on gitleaks being
// installed, and a scrubbing pass must never block a tool result from
// reaching the model.
func RedactText(ctx context.Context, text string) (string, []Finding, error) {
	if !lookPath("gitleaks") {
		return text, nil, nil
	}

	dir, err := os.MkdirTemp("", "gitleaks-redact-*")
	if err != nil {
		return text, nil, nil
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "content.txt"), []byte(text), 0o600); err != nil {
		return text, nil, nil
	}

	report, err := runGitleaksHostDirReport(ctx, dir)
	if err != nil {
		return text, nil, nil
	}

	docs, err := parseGitleaksWithSecrets(report)
	if err != nil || len(docs) == 0 {
		return text, nil, nil
	}

	redacted := text
	findings := make([]Finding, 0, len(docs))
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
