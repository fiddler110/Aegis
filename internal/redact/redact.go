// Package redact holds the in-process credential pattern set and the two things
// callers do with it: ask which classes a string matches, and replace the matches
// with a placeholder.
//
// It exists because P66.11 gave the set its second and third consumers. The
// patterns were written for internal/mcp's outbound argument scan (P24.14), whose
// own comment recorded that no reusable in-process set existed elsewhere —
// internal/security's secret detection shells out to gitleaks/trufflehog binaries,
// which is a scanner rather than a filter and needs a workspace, and
// internal/trust's patterns target injection phrasing rather than credentials.
// That is still true of both, so the set moved here rather than being copied: a
// second copy is how the exported transcript would come to be filtered by an
// older list than the MCP boundary is.
//
// # What this is and is not
//
// It is a filter with a deliberately small, low-false-positive pattern set. It is
// not detection: a nil result from Classes means nothing matched, never that the
// input is clean. Anything relying on completeness wants a real scanner (`aegis
// security scan`), and anything relying on this to make a secret unreachable
// wants the secret not to be in the transcript in the first place.
package redact

import (
	"fmt"
	"regexp"
)

// pattern is one credential shape and the label used for it in logs and
// placeholders. The label is public-facing — it lands in an exported transcript —
// so it names the *class*, never the matched text.
type pattern struct {
	class string
	re    *regexp.Regexp
}

// patterns is the shared set, unchanged from internal/mcp/outbound.go where it
// was defined. Kept deliberately small and low-false-positive: this now filters a
// user-facing artifact as well as flagging an outbound call, and a pattern that
// fires on ordinary prose would quietly shred a transcript.
var patterns = []pattern{
	{"PEM private key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{"AWS access key ID", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"sk- API key (OpenAI/Anthropic style)", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[poursa]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	{"bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}`)},
	{"api_key/secret/password assignment", regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|password|passwd|access[_-]?token)\b["']?\s*[:=]\s*["']?[^\s"']{12,}`)},
}

// Classes returns the class name of every pattern that matched s, in pattern
// order. A nil result means nothing matched — not that s is verified clean.
//
// This is the flag-only form internal/mcp uses: it never returns the matched
// text, because a log line quoting a suspected secret has copied it.
func Classes(s string) []string {
	var out []string
	for _, p := range patterns {
		if p.re.MatchString(s) {
			out = append(out, p.class)
		}
	}
	return out
}

// Text replaces every match in s with a placeholder naming the class, and reports
// how many replacements were made.
//
// The count is the point of the second return value, not a convenience. A
// redaction pass that silently finds nothing looks exactly like one that was
// never wired up — which is the state internal/share was in before P66.11 — so
// every caller is expected to surface the count somewhere a reader will see it.
// Zero is a real answer and should be reported as one.
//
// The whole match is replaced, including the field name in an assignment
// (`api_key: hunter2xxxxxxxx` becomes the placeholder entirely rather than
// keeping the key). Losing the field name is a small cost against the
// alternative, which is a rule about how much of a match is safe to keep.
func Text(s string) (out string, n int) {
	for _, p := range patterns {
		placeholder := fmt.Sprintf("[redacted: %s]", p.class)
		s = p.re.ReplaceAllStringFunc(s, func(string) string {
			n++
			return placeholder
		})
	}
	return s, n
}
