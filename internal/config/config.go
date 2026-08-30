// Package config loads layered Aegis configuration.
//
// Precedence (lowest to highest): built-in defaults -> global config file ->
// per-project config file (./.aegis/config.yaml) -> environment
// variables (AEGIS_*). API keys are always read from the environment.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/fiddler110/aegis/internal/fsguard"
	"github.com/fiddler110/aegis/internal/modelcaps"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/toolshim"
	"github.com/fiddler110/aegis/internal/workspacetrust"
)

// Config is the fully resolved harness configuration.
type Config struct {
	DataDir     string            `koanf:"data_dir"`
	LogLevel    string            `koanf:"log_level"`
	Log         LogConfig         `koanf:"log"`
	Provider    ProviderConfig    `koanf:"provider"`
	Server      ServerConfig      `koanf:"server"`
	Permission  PermissionConfig  `koanf:"permission"`
	Diagram     DiagramConfig     `koanf:"diagram"`
	Cost        CostConfig        `koanf:"cost"`
	Cleanup     CleanupConfig     `koanf:"cleanup"`
	TUI         TUIConfig         `koanf:"tui"`
	Swarm       SwarmConfig       `koanf:"swarm"`
	Sandbox     SandboxConfig     `koanf:"sandbox"`
	Security    SecurityConfig    `koanf:"security"`
	Git         GitConfig         `koanf:"git"`
	OutputGuard OutputGuardConfig `koanf:"output_guard"`
	Compaction  CompactionConfig  `koanf:"compaction"`
	// DefaultPersona names the persona new sessions start with when the
	// caller doesn't pass --persona. Set at the project level
	// (.aegis/config.yaml) to give a repo its own default focus; unset falls
	// back to "general". Not validated at load time (checking it would
	// require importing internal/persona here, which would create an import
	// cycle since persona already has no dependency on config) — an unknown
	// name is caught at session-creation time the same way an explicit
	// --persona typo is.
	DefaultPersona string                     `koanf:"default_persona"`
	Personas       map[string]PersonaOverride `koanf:"personas"`
	Skills         SkillsConfig               `koanf:"skills"`
	LSP            []LSPServerConfig          `koanf:"lsp"`
	Plugins        []ProcessToolConfig        `koanf:"plugins"`
	RepoMap        RepoMapConfig              `koanf:"repomap"`
	MCP            []MCPServerConfig          `koanf:"mcp"`
	MCPServer      MCPServerModeConfig        `koanf:"mcp_server"`
	Hooks          []HookConfig               `koanf:"hooks"`
	Search         SearchConfig               `koanf:"search"`
	// Commands overrides how external host binaries are located, keyed by the
	// names in internal/toolpath.Registry (ripgrep, git, gh, mmdc, plantuml).
	// A value may be an absolute path, a bare command name resolved on PATH, or
	// a disable keyword ("off"/"false"/"none") that forces Aegis's built-in
	// fallback. Aegis execs binaries directly rather than through a shell, so a
	// shell alias is never visible to it — this is how you point Aegis at a
	// binary that isn't on PATH under its usual name. Unset means PATH lookup.
	Commands   map[string]string `koanf:"commands"`
	Notify     NotifyConfig      `koanf:"notify"`
	Embeddings EmbeddingsConfig  `koanf:"embeddings"`
	Workspace  WorkspaceConfig   `koanf:"workspace"`
	Tools      ToolsConfig       `koanf:"tools"`

	// WorkspaceTrust reports the P27.1 workspace-trust gate's outcome for
	// this load. It is never read from a config file (computed by Load()
	// itself) — callers that want to warn or gate on it (server startup,
	// `aegis doctor`, the TUI) read this field rather than re-deriving it.
	WorkspaceTrust WorkspaceTrustStatus `koanf:"-"`
}

// WorkspaceTrustStatus is Load()'s report of the workspace-trust decision for
// the current directory (P27.1, FIND-01/FIND-02): whether it has been
// explicitly trusted, and — if not — which security-relevant settings the
// project's .aegis/config.yaml would have changed had it been honored, now
// frozen back to their user/global values instead.
type WorkspaceTrustStatus struct {
	// Dir is the directory the trust decision applies to (the process's cwd,
	// where .aegis/config.yaml is resolved from).
	Dir string
	// Trusted is true once `aegis trust` (or equivalent) has recorded an
	// explicit trust decision for Dir *and* that decision still covers the
	// security-relevant config currently on disk (P66.25/SEC-07).
	Trusted bool
	// Stale is true when a trust decision exists for Dir but no longer
	// matches: the project's security-relevant config changed since it was
	// granted, or the grant predates P66.25 and recorded no content at all.
	// A stale grant gates exactly like no grant — Trusted is false and the
	// freeze applies — and this field exists only so the operator is told
	// "what you approved has changed" instead of "you never approved this".
	Stale bool
	// Frozen is true when Trusted is false AND the project config would
	// otherwise have changed a security-relevant setting — meaning Changes
	// below were actually applied (reverted to the user/global baseline)
	// rather than just computed for display.
	Frozen bool
	// Changes describes, one line each, which security-relevant settings the
	// (untrusted) project config attempted to change. Empty when Trusted is
	// true or the project config carries no security-relevant overrides.
	Changes []string
}

const (
	// EnvPrefix is the environment-variable prefix for overrides.
	EnvPrefix = "AEGIS_"
	appDir    = "aegis"
)

func defaults() map[string]any {
	return map[string]any{
		"data_dir":                defaultDataDir(),
		"log_level":               "info",
		"log.max_size_mb":         20,
		"log.max_backups":         5,
		"provider.default":        "anthropic",
		"provider.model":          "claude-opus-4-8",
		"provider.max_tokens":     32768,
		"provider.max_retries":    4,
		"provider.prompt_profile": "auto",
		// Sample size for the tool-calling conformance probe (P53.4). Mirrors
		// toolcallprobe.DefaultTrials — spelled as a literal here to keep the
		// config package free of a dependency on the probe.
		"provider.tool_call_probe_trials": 5,
		// The non-native tool-calling fallback (P53.6) is opt-in: a shim that
		// turns model prose into executable tool calls must never arrive by
		// default. Spelled here rather than left empty so `aegis config` shows
		// the key exists and what its off value is.
		"provider.tool_call_shim": toolshim.ModeOff,
		// Admission control in front of the backend (P59.9). 0 is "auto", not
		// "unbounded": a local backend gets MaxConcurrentRequestsDefaultLocal
		// and a cloud one stays unbounded. Spelled here so the key is visible
		// in `aegis config` alongside its auto value.
		"provider.max_concurrent_requests": 0,
		// Resident-set planning (P69.6). 0 is "no budget stated", which means no
		// planning at all rather than an unbounded one — spelled here so the key
		// is visible in `aegis config` with its inert value, since the whole
		// feature is invisible until an operator fills it in. f16 matches Ollama's
		// own default KV cache type; it is a declaration Aegis cannot verify from
		// the API, only falsify from a spilled placement.
		"provider.vram_budget_gb": 0.0,
		"provider.kv_cache_type":  "f16",
		"server.addr":             "127.0.0.1:4127",
		// Conservative non-zero caps by default (P27.12/FIND-14) — see
		// ServerConfig's doc comments for why these values are safe for a
		// normal single-user session while still bounding a runaway/DoS case.
		"server.max_concurrent_runs":  10,
		"server.max_run_duration_sec": 1800,
		"server.sse_buffer_size":      DefaultSSEBufferSize,
		// Pinned-cert loopback TLS on by default (P27.5/FIND-13) — see
		// ServerTLSConfig's doc comment.
		"server.tls.enabled": true,
		// "build" is the intentional default: the agent can read and write
		// files freely, but shell/execute calls still prompt for approval
		// (or are denied non-interactively). Use "plan" for read-only
		// exploration and "auto" (with auto_approve_exec: true) only in
		// fully trusted, sandboxed environments.
		"permission.mode":                        "build",
		"permission.auto_approve_exec":           false,
		"permission.allow_unsandboxed_auto_exec": false,
		"diagram.kroki_url":                      "https://kroki.io",
		"cost.budget_usd":                        0.0,
		"cost.max_tokens_per_run":                0,
		"cost.session_cap_usd":                   0.0,
		"cost.daily_cap_usd":                     0.0,
		"cost.session_token_cap":                 0,
		"cost.daily_token_cap":                   0,
		"cost.alert_threshold":                   0.8,
		// P39.17: on by default, unlike every other cost bound. See
		// DefaultMaxTurnStallSec for why fifteen minutes and why silence is the
		// one time-shaped signal that needs no operator judgement.
		"cost.max_turn_stall": DefaultMaxTurnStallSec,
		"swarm.backend":       "in_process",
		// "container" (Docker/Podman) rather than the unsandboxed "local", so a
		// daemon started before any config file exists — the truest zero-setup
		// path, ahead of even --first-init's template writing this same value
		// explicitly — isn't silently unconfined. SelectSandbox cascades on
		// failure: no container runtime falls back to "os" (P4.7 OS-level
		// isolation via seatbelt/bwrap, no container runtime needed) before
		// giving up to unsandboxed "local" with a startup WARN (never a hard
		// failure, sandbox.strict aside). A host with Docker/Podman running
		// gets the container backend; a macOS/Linux host without one still
		// gets OS-level isolation exactly as it did when "os" was the bare
		// default; only a host with neither (every current Windows box, or a
		// macOS/Linux box missing both Docker and seatbelt/bwrap) lands on
		// local, same as before.
		"sandbox.backend": "container",
		"sandbox.image":   "ubuntu:22.04",
		"sandbox.network": false,
		// P60.1: conservative per-container caps. Sized to let ordinary
		// build/test work through (a `go build`, an `npm ci`) while making a
		// runaway inside the sandbox a failed command rather than a host-wide
		// OOM — the machine running the container is usually also running the
		// model server, so an unbounded container competes with the thing the
		// agent needs to keep thinking. Raise them for a heavy toolchain; empty
		// or 0 removes a cap entirely.
		"sandbox.limits.memory":     "4G",
		"sandbox.limits.cpus":       "2",
		"sandbox.limits.pids_limit": 1024,
		// P60.2: one container per session rather than per command, wherever
		// the runtime supports it. On by default because the per-command
		// alternative discards everything a command did the moment it returned
		// — the reason the container backend was hard to recommend for real
		// work. Containers are labelled, TTL-bounded and reaped, which is what
		// makes owning state affordable.
		"sandbox.persistent":           true,
		"sandbox.session_ttl_sec":      int(sandbox.DefaultSessionTTL / time.Second),
		"security.egress_then_write":   false,
		"security.redact_secrets":      true,
		"security.default_method":      "auto",
		"security.dast.allow_active":   false,
		"security.debate.threat_model": false,
		"security.debate.triage":       false,
		"output_guard.enabled":         true,
		"output_guard.mode":            "llm",
		"output_guard.max_retries":     1,
		"output_guard.rubric":          DefaultGuardRubric,
		"tui.humor_mode":               true,
		"tui.theme":                    "auto",
		"tui.notifications":            "both",
		"tui.image_rendering":          "auto",
		"embeddings.enabled":           false,
		"embeddings.provider":          "ollama",
		"embeddings.model":             "nomic-embed-text",
		"embeddings.base_url":          "http://localhost:11434",
		"mcp_server.auto_approve":      false,
		"mcp_server.default_mode":      "plan",
		// Heuristic invisible-char/base64 prompt-injection scan of
		// web_fetch/web_search output on by default (P27.13/FIND-12) — see
		// SearchConfig.ScanOutput's doc comment.
		"search.scan_output": true,
		// The repo-map budget (P62.1). Both values mirror internal/repomap's
		// DefaultMaxBytes/DefaultMaxSymbolsPerFile, spelled as literals here for
		// the same reason provider.tool_call_probe_trials is — so the config
		// package needs no dependency on the package it sizes. They are stated
		// rather than left at zero so `aegis config` shows the real budget an
		// operator is about to change, not a bare 0 meaning "whatever the binary
		// decides".
		"repomap.max_bytes":            8000,
		"repomap.max_symbols_per_file": 3,
	}
}

// defaultDataDir returns the per-user data directory for the harness.
// Windows : %AppData%\aegis   (e.g. C:\Users\scott\AppData\Roaming\aegis)
// macOS   : ~/.config/aegis   (XDG-compatible; avoids ~/Library/Application Support)
// Linux   : ~/.config/aegis   (XDG default)
func defaultDataDir() string {
	if runtime.GOOS != "windows" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".config", appDir)
		}
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appDir)
}

// GlobalConfigPath returns the path to the user-level config file.
func GlobalConfigPath() string {
	return filepath.Join(defaultDataDir(), "config.yaml")
}

// ProjectConfigPath returns the path to the project-level config file.
func ProjectConfigPath() string {
	return filepath.Join(".aegis", "config.yaml")
}

// FileSecurity returns the security: block exactly as written in one config
// file, with no other layer merged in — no defaults, no environment, and in
// particular no *other* file.
//
// Load() deliberately cannot answer this: it returns the winner of the layer
// merge without saying which file it came from. Two callers need the
// unmerged view (P55.5):
//
//   - A writer. patchSecurity rewrites a file's whole security: block, so the
//     fields it carries through unchanged must come from the file it is about
//     to rewrite. Carrying them from the merged config copies one layer's
//     settings into the other — writing the user config from inside a repo
//     would promote that repo's security.tools/wsl_distro machine-wide.
//   - The shadowing check. Project config overrides user config, so a pin
//     left in a repo by an older `build-image` silently wins over the
//     machine-wide pin; seeing that requires reading the project file alone.
//
// A missing file is not an error — it yields the zero value, which
// buildSecurityBlock renders as exactly the built-in defaults. An unreadable
// or malformed one is: silently treating it as absent would hand a caller a
// blank block to write over the operator's settings.
func FileSecurity(path string) (SecurityConfig, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return SecurityConfig{}, nil
		}
		return SecurityConfig{}, fmt.Errorf("stat config %s: %w", path, err)
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return SecurityConfig{}, fmt.Errorf("load config %s: %w", path, err)
	}
	var sec SecurityConfig
	if err := k.Unmarshal("security", &sec); err != nil {
		return SecurityConfig{}, fmt.Errorf("unmarshal security block of %s: %w", path, err)
	}
	return sec, nil
}

// LoadFileRaw is FileSecurity's whole-Config counterpart: it parses one file
// with no other layer merged in — no built-in defaults, no environment, and
// no other file — so a caller sees exactly what that file declares. Fields
// it doesn't set come back at their Go zero value on both sides of a
// comparison, which is what makes it fit for diffing two files against each
// other (e.g. an existing config against a fresh template) without the
// built-in defaults manufacturing differences neither file actually states.
//
// A missing file is not an error — it yields the zero Config, same as
// FileSecurity. An unreadable or malformed one is.
func LoadFileRaw(path string) (*Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("stat config %s: %w", path, err)
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config %s: %w", path, err)
	}
	return &cfg, nil
}

// loadDotEnv reads a .env-style file and sets any variables it contains into
// the process environment, skipping variables already present so that real
// environment variables always take precedence. The file format is KEY=VALUE
// per line; lines beginning with # and blank lines are ignored. Values may be
// surrounded by single or double quotes (stripped on read).
//
// On a successful read, it also opportunistically hardens the file's
// permissions via fsguard.RestrictToOwner (FIND-29/P24.16; a no-op on
// POSIX). Unlike daemon.token or the session database, .env is a
// pre-existing file the user created, not one Aegis wrote itself, so a
// failure here only logs a warning rather than failing config.Load() — a
// locked-down host where the current user can't rewrite the ACL of their
// own file shouldn't break every command.
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := fsguard.RestrictToOwner(path); err != nil {
		slog.Default().Warn("failed to restrict .env file permissions", "path", path, "err", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if k == "" {
			continue
		}
		// P66.1/SEC-01: a .env file is documented for *secrets*. Letting it
		// set AEGIS_* would let it write the highest-precedence config layer —
		// an undeclared capability, and the one that made a two-line project
		// file enough to reach unprompted host execution. Drop and log those
		// keys unconditionally, trusted directory or not: `aegis trust` is a
		// decision about .aegis/config.yaml, which is reviewable and diffable,
		// not a blanket grant to configure Aegis through the secrets file.
		if strings.HasPrefix(k, EnvPrefix) {
			slog.Default().Warn("ignoring config override in .env file: use .aegis/config.yaml for settings",
				"path", path, "key", k)
			continue
		}
		// Real env vars take precedence; only inject missing keys.
		if _, exists := os.LookupEnv(k); !exists {
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("setenv %s: %w", k, err)
			}
		}
	}
	return nil
}

// envKeyCallback maps AEGIS_<SECTION>_<FIELD> environment variables onto
// koanf's dotted key space (e.g. AEGIS_PROVIDER_MODEL -> provider.model).
// Only the first underscore after a known section prefix becomes a dot;
// remaining underscores stay as-is so compound field names (base_url,
// max_tokens, etc.) are preserved.
var envSections = map[string]bool{
	"provider": true, "server": true, "permission": true,
	"diagram": true, "cost": true, "swarm": true,
	"sandbox": true, "security": true, "output_guard": true,
	"embeddings": true, "repomap": true,
}

func envKeyCallback(s string) string {
	s = strings.ToLower(strings.TrimPrefix(s, EnvPrefix))
	// server.tls.* is the one config surface nested two levels deep that's
	// also documented as AEGIS_*-overridable (AEGIS_SERVER_TLS_ENABLED, used
	// to opt back out of P27.5's now-on-by-default TLS). The generic
	// single-split heuristic below only ever inserts one dot
	// (section.field), which can't reach a field nested under a nested
	// struct — special-case its prefix so the env override actually reaches
	// ServerTLSConfig's fields instead of silently landing on an unused key.
	if rest, ok := strings.CutPrefix(s, "server_tls_"); ok && rest != "" {
		return "server.tls." + rest
	}
	if idx := strings.Index(s, "_"); idx > 0 {
		if envSections[s[:idx]] {
			return s[:idx] + "." + s[idx+1:]
		}
	}
	return s
}

// environSnapshot captures the AEGIS_* portion of the process environment as
// koanf-dotted keys, so a layer can be built over the environment *as it was
// at a chosen moment* rather than as it is now.
//
// P66.1/SEC-01: the baseline layer is the value the workspace-trust freeze
// restores *from*. Building it over `env.Provider` reads the live environment,
// which `loadDotEnv` has by then already written to — so a project-supplied
// value appeared identically in both sides of the diff, `securityRelevantDiff`
// returned empty, and the gate never fired. Taking the snapshot before any
// project-controlled input is read makes the diff honest by construction,
// independently of the .env key filter in loadDotEnv.
func environSnapshot() map[string]any {
	out := map[string]any{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, EnvPrefix) {
			continue
		}
		out[envKeyCallback(k)] = v
	}
	return out
}

// loadLayers builds a koanf instance from defaults -> global config file ->
// (optionally) project config file -> AEGIS_* env, in precedence order.
// includeProject is false to build the "baseline" layer used by the P27.1
// workspace-trust gate: what the effective config would be with the
// project's .aegis/config.yaml ignored entirely (only user/global settings
// and this process's own environment).
//
// envSnapshot, when non-nil, supplies the top env layer instead of the live
// process environment (see environSnapshot).
func loadLayers(includeProject bool, envSnapshot map[string]any) (*koanf.Koanf, error) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(defaults(), "."), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	paths := []string{GlobalConfigPath()}
	if includeProject {
		paths = append(paths, ProjectConfigPath())
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue // missing config files are fine
		}
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load config %s: %w", path, err)
		}
	}

	if envSnapshot != nil {
		if err := k.Load(confmap.Provider(envSnapshot, "."), nil); err != nil {
			return nil, fmt.Errorf("load env snapshot: %w", err)
		}
		return k, nil
	}
	if err := k.Load(env.Provider(EnvPrefix, ".", envKeyCallback), nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}
	return k, nil
}

// Load resolves configuration from all layers and returns the result.
func Load() (*Config, error) {
	// P66.1/SEC-01: resolve workspace trust FIRST, before any file the project
	// directory controls is read. The trust store needs nothing that config
	// provides — only the fixed user-level data dir and the cwd — so there is
	// no chicken-and-egg here, and deciding trust ahead of the loaders is what
	// lets .aegis/.env be gated the same way .aegis/config.yaml already is.
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	// P66.25/SEC-07: the grant is checked against a fingerprint of the
	// security-relevant config, not just against the path, so a `git pull` that
	// adds a `hooks:` block re-prompts instead of being silently inherited. The
	// fingerprint reads .aegis/config.yaml — project-controlled content — but
	// only to *hash* it: nothing parsed here is applied, the layer loaders below
	// still run after the decision, and .aegis/.env is deliberately outside the
	// covered set precisely so this ordering survives. See SecurityFingerprint.
	trustStatus := workspacetrust.Open(WorkspaceTrustStorePath()).Check(dir, SecurityFingerprint(dir))
	trusted := trustStatus == workspacetrust.Trusted

	// Snapshot the environment before .aegis/.env can add to it; the baseline
	// layer is built over this rather than over the live environment.
	baseEnv := environSnapshot()

	// Load .aegis/.env before other layers so secrets (MCP tokens, API keys)
	// can be set there without living in version-controlled config files.
	// Real environment variables always override values in the .env file.
	//
	// Only for a trusted directory: the file sets variables into this process
	// and every child it spawns (LD_PRELOAD, GIT_SSH_COMMAND, NODE_OPTIONS,
	// *_BASE_URL, PATH ...), which is a host-execution primitive handed to
	// whoever wrote the repo. There is no safe enumeration of the dangerous
	// names — the escaping-variable families are open-ended — so the gate is
	// the trust decision, not a denylist.
	if trusted {
		if err := loadDotEnv(filepath.Join(".aegis", ".env")); err != nil {
			return nil, fmt.Errorf("load .aegis/.env: %w", err)
		}
	}

	full, err := loadLayers(true, nil)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := full.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.Sandbox.Normalize(); err != nil {
		return nil, err
	}

	baseline, err := loadLayers(false, baseEnv)
	if err != nil {
		return nil, err
	}
	var baseCfg Config
	if err := baseline.Unmarshal("", &baseCfg); err != nil {
		return nil, fmt.Errorf("unmarshal baseline config: %w", err)
	}
	// A project config asserting an invalid sandbox.backend alias must not
	// itself defeat the trust gate meant to catch it; only the trusted
	// (project-inclusive) config's Normalize error is fatal.
	_ = baseCfg.Sandbox.Normalize()

	applyWorkspaceTrust(&cfg, &baseCfg, dir, trustStatus)

	// P66.5/SEC-06, generalizing P27.9/FIND-11: the baselineOnly keys
	// (data_dir, security.dast.allowed_targets) take their value from the
	// user/global baseline unconditionally — never from the project layer,
	// trusted or not. This is stronger than the P27.1 trust gate above (which
	// lets a *trusted* project widen permission/sandbox/mcp/hooks) because
	// their blast radius reaches past the workspace being trusted: an active
	// scanner authorized against arbitrary Internet hosts, or the audit trail
	// relocated into the repository that is being audited. See
	// configTrustPolicy for the per-key reasoning.
	applyBaselineOnlyKeys(&cfg, &baseCfg)

	// SEC-02: a `commands:` override naming a relative path resolves against
	// the workspace, so it names a binary the repository ships. Rejected even
	// in a trusted workspace, hence after the freeze rather than inside it.
	rejectRelativeCommandOverrides(&cfg, &baseCfg)

	// Everything below reads or rewrites the *effective* values, so it runs
	// after the freeze: resolving the API key for a provider the project asked
	// for — and then had frozen away — would key the wrong service, and
	// expanding $VAR before the diff would compare an expanded project value
	// against an unexpanded baseline one and report a change that isn't there.
	cfg.Provider.APIKey = ProviderAPIKey(cfg.Provider.Default)

	// Expand $VAR / ${VAR} references in MCP auth tokens so secrets can be
	// kept in environment variables or .aegis/.env rather than in the YAML.
	for i := range cfg.MCP {
		if cfg.MCP[i].Auth != "" {
			cfg.MCP[i].Auth = os.ExpandEnv(cfg.MCP[i].Auth)
		}
	}
	cfg.Search.APIKey = os.ExpandEnv(cfg.Search.APIKey)
	cfg.Notify.Webhook = os.ExpandEnv(cfg.Notify.Webhook)

	return &cfg, nil
}

// WorkspaceTrustStorePath returns the path to the JSON file recording
// per-directory workspace-trust decisions (P27.1). Deliberately anchored to
// the fixed user-level data directory rather than cfg.DataDir — cfg.DataDir
// can itself be overridden by project config, and resolving the trust store
// through a value the untrusted layer influences would let a hostile project
// config point it at a location where an attacker controls the "trusted"
// marker.
func WorkspaceTrustStorePath() string {
	return filepath.Join(defaultDataDir(), "workspace_trust.json")
}

// applyWorkspaceTrust implements the P27.1 workspace-trust gate: a project's
// .aegis/config.yaml is auto-merged with no confirmation today (FIND-02),
// letting a cloned repository silently widen permission.mode, add an
// attacker MCP server or process-tool plugin, set a notify.webhook
// exfiltration channel, or run session_start/pre_tool_use hooks (FIND-01).
// Until an operator explicitly trusts the current directory (`aegis trust`),
// every key `configTrustPolicy` does not mark projectSettable is frozen to its
// user/global value — cfg (already unmarshalled with the project layer
// applied) is mutated in place to fall back to baseline (project layer
// excluded) for those keys. Which keys those are is P66.5's subject: the
// classification is exhaustive over Config and defaults to frozen, so this
// function no longer carries a list of its own to fall out of date.
// dir and status are resolved by Load before any project-controlled file is
// applied (P66.1), and passed in rather than recomputed here so that one trust
// decision governs both the .env load and this freeze.
//
// P66.25/SEC-07: status carries three answers, not two. Stale — a grant whose
// fingerprint no longer matches, or a pre-fingerprint grant — freezes exactly
// like Untrusted; the distinction only reaches the operator-facing message.
func applyWorkspaceTrust(cfg, baseline *Config, dir string, status workspacetrust.Status) {
	trusted := status == workspacetrust.Trusted
	// P66.1/SEC-01: Trusted reports what the trust store says, and nothing
	// else. It used to be forced true whenever .aegis/config.yaml was absent —
	// "no project config, nothing to gate" — but absence of a file is not a
	// trust decision, and other project-controlled inputs (.aegis/.env,
	// personas, skills) are gated on this same answer. The persona loader had
	// already worked around it by querying the store directly
	// (server.go:787); this makes the field itself honest so the next such
	// caller does not need the same workaround.
	cfg.WorkspaceTrust = WorkspaceTrustStatus{Dir: dir, Trusted: trusted, Stale: status == workspacetrust.Stale}

	// Nothing to freeze if there's no project config file — the diff would be
	// empty anyway, so skip building it.
	if _, err := os.Stat(ProjectConfigPath()); err != nil {
		return
	}

	diffs := securityRelevantDiff(cfg, baseline)
	if trusted || len(diffs) == 0 {
		return
	}

	freezeToBaseline(cfg, baseline, func(p trustPolicy) bool { return p != projectSettable })
	cfg.WorkspaceTrust.Frozen = true
	cfg.WorkspaceTrust.Changes = diffs
}

// ProviderAPIKey reads the API key for the given provider from the
// environment. Exported so the provider factory can resolve keys for
// fallback providers (P5.9), not just the primary.
func ProviderAPIKey(provider string) string {
	switch provider {
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "openai":
		if k := os.Getenv("OPENAI_API_KEY"); k != "" {
			return k
		}
		// Groq and OpenRouter are OpenAI-compatible endpoints reached via
		// provider.default: openai + a custom base_url (see docs/providers.md),
		// not distinct provider names — fall back to their named env vars so
		// docs/configuration.md's GROQ_API_KEY/OPENROUTER_API_KEY entries (P29.4)
		// actually work without requiring OPENAI_API_KEY reuse.
		if k := os.Getenv("GROQ_API_KEY"); k != "" {
			return k
		}
		return os.Getenv("OPENROUTER_API_KEY")
	case "ollama":
		if k := os.Getenv("OPENAI_API_KEY"); k != "" {
			return k
		}
		return "ollama"
	default:
		return ""
	}
}

// EnsureDataDir creates the configured data directory if it does not exist.
func (c *Config) EnsureDataDir() error {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir %s: %w", c.DataDir, err)
	}
	return nil
}

// SessionDBPath returns the path to the session database.
func (c *Config) SessionDBPath() string {
	return filepath.Join(c.DataDir, "sessions.db")
}

// ModelCapsPath returns the path to the persisted per-model capability cache
// (P53.5). Anchored to cfg.DataDir rather than the fixed user-level directory
// the workspace-trust store uses: this file is a discovery cache with no
// security decision behind it, so a project-level data_dir pointing it
// elsewhere costs at most a re-probe.
// An empty DataDir yields an empty path, which modelcaps.Open turns into a
// working in-memory-only store. That matters: filepath.Join("", name) is a
// *relative* path, so without this guard a config with no data dir (every
// hand-built test Config) would drop the cache file into the working
// directory.
func (c *Config) ModelCapsPath() string {
	if c.DataDir == "" {
		return ""
	}
	return filepath.Join(c.DataDir, modelcaps.FileName)
}

// OpenModelCaps opens the per-model capability cache with this config's
// declared overrides applied. Always returns a usable store — a missing or
// unreadable file simply starts empty.
func (c *Config) OpenModelCaps() *modelcaps.Store {
	return modelcaps.Open(c.ModelCapsPath(), modelcaps.WithDeclared(c.Provider.ModelCapabilities))
}

// LogPath returns the path to the harness log file.
func (c *Config) LogPath() string {
	return filepath.Join(c.DataDir, "aegis.log")
}

// LogMaxSizeBytes returns c.Log.MaxSizeMB in bytes, for logging.Options.
// MaxSizeBytes (GAP-02). <= 0 means rotation is disabled.
func (c *Config) LogMaxSizeBytes() int64 {
	if c.Log.MaxSizeMB <= 0 {
		return 0
	}
	return int64(c.Log.MaxSizeMB) * 1024 * 1024
}

// AuthTokenPath returns the path to the daemon auth token file.
func (c *Config) AuthTokenPath() string {
	return filepath.Join(c.DataDir, "daemon.token")
}

// MCPTokenPath returns the path to the auto-generated `aegis mcp-serve`
// stdio auth token file, used when AEGIS_MCP_TOKEN is not set in the
// environment (P27.4/FIND-06).
func (c *Config) MCPTokenPath() string {
	return filepath.Join(c.DataDir, "mcp.token")
}

// ACPTokenPath returns the path to the auto-generated `aegis acp` stdio
// auth token file, used when AEGIS_ACP_TOKEN is not set in the environment
// (P27.4/FIND-06).
func (c *Config) ACPTokenPath() string {
	return filepath.Join(c.DataDir, "acp.token")
}

// TLSCertPath returns the path to the daemon's TLS certificate (FIND-32/
// P24.18): Server.TLS.CertFile if the operator configured one, otherwise
// <DataDir>/daemon.crt, auto-generated on first start with TLS enabled.
func (c *Config) TLSCertPath() string {
	if c.Server.TLS.CertFile != "" {
		return c.Server.TLS.CertFile
	}
	return filepath.Join(c.DataDir, "daemon.crt")
}

// TLSKeyPath is TLSCertPath's counterpart for the private key.
func (c *Config) TLSKeyPath() string {
	if c.Server.TLS.KeyFile != "" {
		return c.Server.TLS.KeyFile
	}
	return filepath.Join(c.DataDir, "daemon.key")
}

// KnowledgeDBPath returns the path to the project knowledge base (P3.3).
// Project-scoped (under root's .aegis/ directory, like memory.md and
// repomap.json) rather than DataDir-scoped, so separate projects don't share
// (and collide in) the same index.
func (c *Config) KnowledgeDBPath(root string) string {
	return filepath.Join(root, ".aegis", "knowledge.db")
}

// LongMemDBPath returns the path to the long-term entity memory store (P3.1).
func (c *Config) LongMemDBPath() string {
	return filepath.Join(c.DataDir, "longmem.db")
}
