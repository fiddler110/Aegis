package sandbox

import (
	"runtime"
	"strings"
)

// DefaultStripEnv lists environment variable names that are always excluded
// from commands run by the local and OS sandbox backends (P7.2). These are
// the provider credentials Aegis itself reads from the environment to
// authenticate with the LLM API — a prompt-injected instruction that gets the
// agent to run `shell` should not be able to read them back out, then use
// web_fetch to exfiltrate them to a public host.
//
// Callers can extend this list (e.g. with names of secrets loaded from
// .aegis/.env for MCP server auth) via sandbox.StripEnv config /
// NewLocalBackendWithEnv / NewOSBackend's stripEnv parameter; it is always
// merged with, not replaced by, the caller-supplied list.
var DefaultStripEnv = []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}

// mergeStripEnv combines DefaultStripEnv with any additional names, deduping.
func mergeStripEnv(extra []string) []string {
	seen := make(map[string]bool, len(DefaultStripEnv)+len(extra))
	out := make([]string, 0, len(DefaultStripEnv)+len(extra))
	for _, names := range [][]string{DefaultStripEnv, extra} {
		for _, n := range names {
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// filteredEnv returns the current process environment with every variable
// named in strip removed. Matching is case-insensitive on Windows (where env
// var names are case-insensitive) and case-sensitive elsewhere.
func filteredEnv(environ []string, strip []string) []string {
	if len(strip) == 0 {
		return environ
	}
	blocked := make(map[string]bool, len(strip))
	for _, n := range strip {
		blocked[envKey(n)] = true
	}
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if ok && blocked[envKey(name)] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func envKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}
