package mcp

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
)

// Outbound tool-call argument inspection (P24.14, FIND-12).
//
// Tool-call arguments are constructed by the model and may contain anything
// the model has read into context — file contents, environment output,
// earlier tool results. They are forwarded verbatim to whichever MCP server
// the call targets, so a model manipulated via prompt injection into
// stuffing sensitive data into an argument bound for an untrusted server is
// an exfiltration channel this layer otherwise never sees.
//
// When a server opts in via `scan_arguments`, the serialized arguments are
// checked against a small, conservative set of credential-shaped patterns
// before the call is forwarded. A hit produces a Warn-level log naming the
// server, tool, and matched pattern class — never the matched text itself,
// which would copy the suspected secret into the log. Matching the inbound
// scan_output philosophy, the call is flagged, never blocked or mutated: a
// false positive must not break a legitimate tool call, and the operator
// (not this heuristic) decides what to do about a flagged server.
//
// No reusable in-process secret pattern set exists elsewhere in the
// codebase — internal/security's secret detection shells out to gitleaks/
// trufflehog binaries, and internal/trust's patterns target injection
// phrasing, not credentials — so this set is defined here and kept
// deliberately small and low-false-positive.
var secretPatterns = []struct {
	class string
	re    *regexp.Regexp
}{
	{"PEM private key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{"AWS access key ID", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"sk- API key (OpenAI/Anthropic style)", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[poursa]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	{"bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}`)},
	{"api_key/secret/password assignment", regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|password|passwd|access[_-]?token)\b["']?\s*[:=]\s*["']?[^\s"']{12,}`)},
}

// scanArgsForSecrets runs secretPatterns over the serialized tool-call
// arguments and returns the class name of every pattern that matched. A nil
// result means nothing matched — not that the arguments are verified clean.
func scanArgsForSecrets(args json.RawMessage) []string {
	s := string(args)
	var classes []string
	for _, p := range secretPatterns {
		if p.re.MatchString(s) {
			classes = append(classes, p.class)
		}
	}
	return classes
}

// warnOutboundSecrets scans args and, on any hit, logs a Warn identifying
// the target server, the tool being called, and the matched pattern classes.
// It never blocks, drops, or rewrites the call — flag-only, symmetric with
// the inbound scan_output behavior — and never logs the matched text itself.
func warnOutboundSecrets(logger *slog.Logger, server, toolName string, args json.RawMessage) {
	classes := scanArgsForSecrets(args)
	if len(classes) == 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("mcp outbound argument scan flagged possible secret in tool-call arguments",
		"server", server, "tool", toolName, "patterns", strings.Join(classes, ", "))
}
