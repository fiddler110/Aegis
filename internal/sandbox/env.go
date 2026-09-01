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
// This is applied as a *second* layer on top of DefaultEnvAllow (P81.26/
// FIND-26), not as the primary control: an allowlisted name could still, on
// some unusual setup, carry a secret (an operator exporting a token through
// PATH-adjacent tooling, say), so every command's environment is built by
// allowlistedEnv, which allowlists first and then strips this list from what
// survives.
//
// Callers can extend this list (e.g. with names of secrets loaded from
// .aegis/.env for MCP server auth) via sandbox.StripEnv config /
// NewLocalBackendWithEnv / NewOSBackend's stripEnv parameter; it is always
// merged with, not replaced by, the caller-supplied list.
var DefaultStripEnv = []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}

// DefaultEnvAllow lists the environment variable names passed through to a
// sandboxed command by default on the local/os backends (P81.26/FIND-26).
//
// Sandboxed command execution used to start from the daemon's own full
// environment — where CLAUDE.md says provider API keys live by design — and
// subtract DefaultStripEnv. A denylist over an inherited environment fails
// open: any secret-bearing variable the operator's shell happens to export,
// that nobody thought to add to the strip list, was visible to every
// model-issued shell command. This inverts the default: start from nothing
// and name only what a command legitimately needs.
//
// The set below is sized against a real run of this project's own toolchain
// (go build, go test — see CLAUDE.md) plus the ordinary cases the roadmap
// flagged as likely to break silently: npm ci, and git/go/npm behind a
// corporate proxy.
//   - Program discovery, working directory, locale: PATH, HOME/USERPROFILE,
//     TMPDIR/TEMP/TMP, LANG and friends — without these most interpreters and
//     shells fail before they even reach the command being run.
//   - Windows process-launch plumbing: SystemRoot/windir/COMSPEC/PATHEXT — a
//     spawned cmd.exe or PowerShell needs these to start at all, independent
//     of anything the command itself does.
//   - Go toolchain: GOPATH/GOCACHE/GOMODCACHE/GOROOT/GOTOOLCHAIN/GOFLAGS and
//     the module-proxy knobs (GOPROXY/GOSUMDB/GOPRIVATE/GONOSUMCHECK/
//     GONOPROXY/GOINSECURE) — a `go build`/`go test` without these either
//     fails or silently re-downloads/rebuilds everything from scratch.
//   - npm/node: APPDATA/LOCALAPPDATA (npm's config/cache locations on
//     Windows), NPM_CONFIG_CACHE, NODE_PATH.
//   - Corporate-proxy egress, both cases the ecosystem uses: HTTP_PROXY/
//     HTTPS_PROXY/NO_PROXY and their lowercase forms.
//
// An operator extends this via sandbox.env_allow for anything project- or
// environment-specific this list does not anticipate; DefaultStripEnv still
// applies on top either way.
var DefaultEnvAllow = []string{
	"PATH", "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
	"TMPDIR", "TEMP", "TMP",
	"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE",
	"SHELL", "USER", "USERNAME",
	"SystemRoot", "windir", "COMSPEC", "PATHEXT",
	"GOPATH", "GOCACHE", "GOMODCACHE", "GOROOT", "GOTOOLCHAIN", "GOFLAGS",
	"GOPROXY", "GOSUMDB", "GOPRIVATE", "GONOSUMCHECK", "GONOPROXY", "GOINSECURE",
	"CGO_ENABLED",
	"APPDATA", "LOCALAPPDATA", "NPM_CONFIG_CACHE", "NODE_PATH",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
}

// DefaultContainerEnvAllow lists the environment variable names forwarded
// from the host into a sandboxed *container* by default (P81.26/FIND-26).
//
// It is deliberately narrower than DefaultEnvAllow: PATH, HOME, GOPATH and
// the rest name host filesystem paths, and a container has its own
// filesystem — forwarding a host path into it would be wrong, not merely
// unnecessary, since the image already sets its own correct values for all
// of those. Only locale and proxy configuration (values, not paths) make
// sense to carry across, plus whatever an operator adds via
// sandbox.env_allow for a project-specific need.
var DefaultContainerEnvAllow = []string{
	"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
}

// mergeStripEnv combines DefaultStripEnv with any additional names, deduping.
func mergeStripEnv(extra []string) []string {
	return mergeNames(DefaultStripEnv, extra)
}

// mergeEnvAllow combines base (one of the Default*EnvAllow lists above) with
// any operator-configured additional names (sandbox.env_allow), deduping.
func mergeEnvAllow(base, extra []string) []string {
	return mergeNames(base, extra)
}

func mergeNames(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, names := range [][]string{base, extra} {
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

// filteredEnv returns environ with every variable named in strip removed.
// Matching is case-insensitive on Windows (where env var names are
// case-insensitive) and case-sensitive elsewhere. This is the "second layer"
// defensive strip applied on top of an allowlist by allowlistedEnv, and is
// also used standalone by anything that still wants denylist-only filtering.
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

// allowlistedEnv builds the environment for a sandboxed command (P81.26/
// FIND-26): start from nothing, keep only the names in allow, then apply
// strip as a defensive second pass over what survived (in case an allowlisted
// name — PATH, say — somehow carries a secret on some unusual setup).
func allowlistedEnv(environ []string, allow []string, strip []string) []string {
	allowed := make(map[string]bool, len(allow))
	for _, n := range allow {
		if n == "" {
			continue
		}
		allowed[envKey(n)] = true
	}
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || !allowed[envKey(name)] {
			continue
		}
		out = append(out, kv)
	}
	return filteredEnv(out, strip)
}

func envKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}
