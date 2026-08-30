package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// SandboxConfig configures command execution isolation.
type SandboxConfig struct {
	Backend  string   `koanf:"backend"`  // "container" (default; Docker/Podman, cascades to "os" then "local"), "os" (P4.7 OS-level isolation), "auto" (detect & pick a runtime), or "local"
	Runtime  string   `koanf:"runtime"`  // forced runtime when backend=container: "docker", "podman", "wslc", "container" (Apple); empty = auto-detect
	Priority []string `koanf:"priority"` // auto-detect order, e.g. ["wslc","docker","podman"]; empty = OS default
	Image    string   `koanf:"image"`    // container image (default "ubuntu:22.04")
	Network  bool     `koanf:"network"`  // allow network access inside containers (default false)
	// Strict, when true, makes the daemon refuse to start (rather than
	// silently falling back to the unsandboxed local backend) if the
	// configured "container" or "os" backend cannot be initialized (P7.4).
	Strict bool `koanf:"strict"`
	// StripEnv names additional environment variables to exclude from
	// commands run by the local/os backends, on top of the built-in default
	// (provider API keys) (P7.2). Use this for secrets loaded via
	// .aegis/.env for MCP server auth or gateway headers that the shell tool
	// has no legitimate reason to read.
	StripEnv []string `koanf:"strip_env"`
	// OSExtraReadPaths names additional host paths the "os" backend
	// (seatbelt/bwrap) may read from, on top of the workspace and the
	// built-in toolchain defaults (FIND-19/P27.18) — see
	// sandbox.defaultOSReadPaths. Use this when a project's build needs a
	// toolchain installed somewhere non-standard. Each entry that doesn't
	// exist on the host is silently skipped.
	OSExtraReadPaths []string `koanf:"os_extra_read_paths"`
	// Limits caps what a single sandboxed container run may consume (P60.1).
	// Applies to the "container"/"auto" backends only — the local and os
	// backends run on the host, where there is no per-command resource knob to
	// set. Defaults are conservative (see the defaults map); set a field empty
	// or zero to remove that cap.
	Limits SandboxLimits `koanf:"limits"`
	// Persistent keeps one long-lived container per workspace directory for the
	// daemon's lifetime and runs each command in it, instead of a fresh
	// `run --rm` per command (P60.2). On by default (see the defaults map)
	// wherever the runtime supports it, because per-command containers make the
	// container backend behaviourally worse than `local` for multi-step work:
	// an installed toolchain, a warmed cache or a background server does not
	// survive the call that created it. Set false to go back to a fresh
	// container per command — the strictly leak-free posture, at the cost of
	// remembering nothing.
	Persistent bool `koanf:"persistent"`
	// SessionTTLSec bounds a persistent container's life when the daemon never
	// gets to tear it down (SIGKILL, a host that sleeps forever). The container
	// holds itself open with a `sleep` of this length under `--rm`, so expiry
	// removes it with nothing needing to run. 0 uses sandbox.DefaultSessionTTL
	// (4h). No effect unless Persistent is set.
	SessionTTLSec int `koanf:"session_ttl_sec"`
}

// SandboxSessionTTL returns sandbox.session_ttl_sec as a duration, substituting
// sandbox.DefaultSessionTTL when unset (P60.2).
func (s SandboxConfig) SandboxSessionTTL() time.Duration {
	if s.SessionTTLSec <= 0 {
		return sandbox.DefaultSessionTTL
	}
	return time.Duration(s.SessionTTLSec) * time.Second
}

// SandboxLimits is the resource cap applied to each sandboxed container run
// (P60.1). It is a separate axis from the capability drops in
// sandbox.OCIHardeningFlags: those stop a container doing something
// privileged, these stop it eating the machine the daemon and the model server
// are also living on.
//
// The values are strings in the container runtime's own vocabulary ("4G",
// "512M", "1.5") rather than typed quantities, so an operator writes what the
// engine documents and Aegis does not interpose a second, poorer parser between
// them. Sandbox.Limits{} (all empty) restores the pre-P60.1 behavior of no cap
// at all.
type SandboxLimits struct {
	Memory string `koanf:"memory"`     // --memory, e.g. "4G"; empty = uncapped
	CPUs   string `koanf:"cpus"`       // --cpus, e.g. "2"; empty = uncapped
	PIDs   int    `koanf:"pids_limit"` // --pids-limit; 0 = uncapped
}

// Sandbox returns the limits in the form the sandbox package takes them.
func (l SandboxLimits) Sandbox() sandbox.ResourceLimits {
	return sandbox.ResourceLimits{Memory: l.Memory, CPUs: l.CPUs, PIDs: l.PIDs}
}

// sandboxBackendAliases maps the container-runtime names CLAUDE.md/the docs
// advertise ("Docker, Podman, WSL containers, Apple Containers") to the
// backend+runtime pair that actually selects them. Before P25.2, typing one
// of these directly into sandbox.backend (instead of the correct
// `backend: container` + `runtime: <name>`) silently fell through
// SelectSandbox's default case and ran every command unsandboxed on the
// host, with no warning — sandbox.ParseRuntimes's vocabulary is the source
// of truth these aliases resolve to.
var sandboxBackendAliases = map[string]string{
	"docker": string(sandbox.RuntimeDocker),
	"podman": string(sandbox.RuntimePodman),
	"wsl":    string(sandbox.RuntimeWSL),
	"wslc":   string(sandbox.RuntimeWSL),
	"apple":  string(sandbox.RuntimeAppleContainers),
}

// sandboxKnownBackends are the values SelectSandbox actually switches on
// ("" and "local" both mean the unsandboxed local backend).
var sandboxKnownBackends = map[string]bool{
	"":          true,
	"local":     true,
	"container": true,
	"auto":      true,
	"os":        true,
}

// Normalize rewrites a runtime-name typed into Backend (e.g. "podman") into
// the correct backend+runtime pair, and hard-errors on anything else
// unrecognized rather than letting it silently reach SelectSandbox's
// default case, which treats an unknown backend the same as "local" (P25.2).
// Exported so callers that build a SandboxConfig outside of Load() — e.g.
// the PATCH /config/sandbox handler validating a request before it's
// written to disk — apply the identical alias/validation table.
func (c *SandboxConfig) Normalize() error {
	if canonicalRuntime, ok := sandboxBackendAliases[strings.ToLower(strings.TrimSpace(c.Backend))]; ok {
		c.Backend = "container"
		if c.Runtime == "" {
			c.Runtime = canonicalRuntime
		}
		return nil
	}
	if !sandboxKnownBackends[c.Backend] {
		return fmt.Errorf(
			"sandbox.backend %q is not a valid backend; use \"local\", \"container\", \"auto\", or \"os\" "+
				"— for a specific container runtime, set sandbox.backend: container and sandbox.runtime: %q",
			c.Backend, c.Backend,
		)
	}
	return nil
}
