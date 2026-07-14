// Package config loads layered Aegis configuration.
//
// Precedence (lowest to highest): built-in defaults -> global config file ->
// per-project config file (./.aegis/config.yaml) -> environment
// variables (AEGIS_*). API keys are always read from the environment.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/fiddler110/aegis/internal/fsguard"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/workspacetrust"
)

// Config is the fully resolved harness configuration.
type Config struct {
	DataDir     string            `koanf:"data_dir"`
	LogLevel    string            `koanf:"log_level"`
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
	OutputGuard OutputGuardConfig `koanf:"output_guard"`
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
	MCP            []MCPServerConfig          `koanf:"mcp"`
	MCPServer      MCPServerModeConfig        `koanf:"mcp_server"`
	Hooks          []HookConfig               `koanf:"hooks"`
	Search         SearchConfig               `koanf:"search"`
	Notify         NotifyConfig               `koanf:"notify"`
	Embeddings     EmbeddingsConfig           `koanf:"embeddings"`

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
	// explicit trust decision for Dir.
	Trusted bool
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

// MCPServerModeConfig controls `aegis mcp-serve` (P6.3): exposing this
// daemon's sessions as MCP tools to other MCP-speaking harnesses. This is the
// reverse direction of MCP (above) — Aegis acting as the server rather than
// the client.
type MCPServerModeConfig struct {
	// AutoApprove lets a tool call needing interactive approval proceed
	// automatically instead of being denied. Off by default: an MCP
	// tools/call is a synchronous request/response with no human in the loop
	// to ask, so the safe default is to deny anything the session's
	// permission mode doesn't already allow outright, not to silently grant
	// it. Overridable per-invocation with `aegis mcp-serve --auto-approve`.
	AutoApprove bool `koanf:"auto_approve"`
	// DefaultMode is the permission mode a new session gets when a caller
	// doesn't specify one. Defaults to "plan" (read + network only, nothing
	// needs approval) rather than the daemon's own configured default, since
	// an external MCP client is a lower-trust caller than the local TUI/CLI.
	DefaultMode string `koanf:"default_mode"`
}

// EmbeddingsConfig enables the optional semantic recall layer (P5.8) over the
// project knowledge base and long-term entity memory. Disabled by default —
// both stores remain BM25-only (FTS5) unless this is turned on.
type EmbeddingsConfig struct {
	Enabled  bool   `koanf:"enabled"`  // opt-in; false keeps FTS5-only search
	Provider string `koanf:"provider"` // only "ollama" is supported today
	Model    string `koanf:"model"`    // embedding model name, e.g. "nomic-embed-text"
	BaseURL  string `koanf:"base_url"` // Ollama base URL; defaults to http://localhost:11434
}

// HookConfig declares one user-configurable lifecycle hook (P4.4). The command
// receives a JSON event on stdin. For tool events, exit code 2 vetoes the tool
// call and the command's stderr is surfaced to the model; any other non-zero
// exit is logged but does not block.
type HookConfig struct {
	Event      string   `koanf:"event"`       // pre_tool_use, post_tool_use, session_start, stop, subagent_stop
	Command    string   `koanf:"command"`     // shell command run via `sh -c`; JSON event on stdin
	Tools      []string `koanf:"tools"`       // optional tool-name filter for tool events (empty = all)
	TimeoutSec int      `koanf:"timeout_sec"` // command timeout; 0 -> default (30s)
}

// SearchConfig selects the web-search provider (P5.3). Empty provider keeps the
// zero-config DuckDuckGo HTML scrape.
type SearchConfig struct {
	Provider string `koanf:"provider"` // "", "duckduckgo", "brave", "tavily", "searxng"
	APIKey   string `koanf:"api_key"`  // may reference $ENV; expanded on load
	BaseURL  string `koanf:"base_url"` // required for searxng self-host
	// ScanOutput opts web_fetch/web_search output into the same heuristic
	// prompt-injection scan used for MCP servers (FIND-04/FIND-12). On by
	// default since P27.13 — it's a best-effort heuristic (invisible/
	// zero-width characters, base64-encoded payloads) that never blocks or
	// mutates content on a hit, only adds a visible "[SECURITY WARNING]"
	// note inside the existing untrusted-content wrapper (see
	// internal/trust.Wrap), so a false positive costs nothing beyond an
	// extra line of context; a false negative costs nothing beyond the
	// status quo. The untrusted-content provenance marker on fetched/search
	// output is always applied regardless of this setting.
	ScanOutput bool `koanf:"scan_output"`
}

// NotifyConfig configures notifications when a background session finishes or
// needs input (P5.4).
type NotifyConfig struct {
	Desktop bool   `koanf:"desktop"` // fire an OS desktop notification
	Webhook string `koanf:"webhook"` // POST the event JSON to this URL (may reference $ENV)
}

// TUIConfig holds terminal UI preferences.
type TUIConfig struct {
	// HumorMode enables D&D-themed thinking phrases while the model generates.
	// Set to false for plain "thinking…" / "working…" status text.
	HumorMode bool `koanf:"humor_mode"`
	// Theme selects the TUI color scheme: "dark" (default), "light", an
	// embedded builtin (catppuccin, dracula, gruvbox, tokyonight), or a
	// custom name loaded from .aegis/themes/<name>.json (project) or
	// ~/.aegis/themes/<name>.json (user) — see internal/tui/theme_loader.go.
	Theme string `koanf:"theme"`
	// Notifications controls the P16.1 attention system fired on stream-end,
	// approval-pending, and error while the terminal isn't focused: "off",
	// "bell", "desktop", or "both" (default).
	Notifications string `koanf:"notifications"`
	// ImageRendering controls the P16.9 inline thumbnail shown in the
	// transcript when an image is attached: "auto" (default — rendered when
	// the terminal's detected color profile supports it) or "off".
	ImageRendering string `koanf:"image_rendering"`
	// Keybindings remaps named TUI actions (P13.3.5). Keys are the binding
	// names from internal/tui's keyMap (e.g. "terminal", "palette",
	// "diagnose" — lowercased struct field name), values are one or more
	// key sequences in bubbles/key form (e.g. "ctrl+x", "alt+t"). Unlisted
	// actions keep their hardcoded default. Unknown action names are
	// rejected at TUI startup.
	Keybindings map[string][]string `koanf:"keybindings"`
}

// CleanupConfig controls automatic pruning of old sessions.
type CleanupConfig struct {
	// SessionTTLDays is how many days since last update before a non-archived
	// session is automatically deleted. 0 disables auto-cleanup.
	SessionTTLDays int `koanf:"session_ttl_days"`
	// IntervalHours is how often the pruner runs. Defaults to 24.
	IntervalHours int `koanf:"interval_hours"`
}

// SwarmConfig configures multi-agent sub-agent execution.
type SwarmConfig struct {
	Backend string `koanf:"backend"` // "in_process" (default) or "subprocess"
}

// SandboxConfig configures command execution isolation.
type SandboxConfig struct {
	Backend  string   `koanf:"backend"`  // "local" (default), "container", or "auto" (detect & pick)
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

// CostConfig configures spend tracking.
type CostConfig struct {
	BudgetUSD float64 `koanf:"budget_usd"` // abort a run past this estimated cost; 0 = unlimited

	// MaxTokensPerRun aborts a run past this cumulative token count (input +
	// output + cache, across every turn); 0 = unlimited. The primary spend
	// guardrail (P10.5): unlike BudgetUSD, it is always enforceable because
	// token counts are present even for unpriced or local/Ollama models where
	// BudgetUSD silently never fires (estimated usage carries no dollar cost).
	MaxTokensPerRun int `koanf:"max_tokens_per_run"`

	// SessionCapUSD refuses to start a new turn once a session's cumulative
	// (persisted) cost reaches this amount; 0 = unlimited (P9.5).
	SessionCapUSD float64 `koanf:"session_cap_usd"`
	// DailyCapUSD refuses to start a new turn once total spend across all
	// sessions for the current UTC day reaches this amount; 0 = unlimited (P9.5).
	DailyCapUSD float64 `koanf:"daily_cap_usd"`
	// SessionTokenCap refuses to start a new turn once a session's cumulative
	// (persisted) token count reaches this amount; 0 = unlimited (P10.5). The
	// token-denominated counterpart to SessionCapUSD — always enforceable.
	SessionTokenCap int `koanf:"session_token_cap"`
	// DailyTokenCap refuses to start a new turn once total tokens across all
	// sessions for the current UTC day reaches this amount; 0 = unlimited (P10.5).
	DailyTokenCap int `koanf:"daily_token_cap"`
	// AlertThreshold is the fraction (0-1) of SessionCapUSD/DailyCapUSD/
	// SessionTokenCap/DailyTokenCap at which a warning event is surfaced to
	// the client instead of a hard stop. Only takes effect for whichever cap
	// is non-zero. Default 0.8 (P9.5).
	AlertThreshold float64 `koanf:"alert_threshold"`
}

// LSPServerConfig configures one LSP language server.
type LSPServerConfig struct {
	Name       string   `koanf:"name"`       // e.g. "gopls"
	Command    string   `koanf:"command"`    // executable
	Args       []string `koanf:"args"`       // CLI arguments
	Extensions []string `koanf:"extensions"` // file extensions (e.g. [".go"])
	// Trust opts this server into starting even if Command isn't a
	// recognized LSP binary basename (internal/lsp's built-in allowlist).
	// LSP servers start eagerly at daemon boot with no interactive
	// approver present, so an unrecognized command is refused unless this
	// is explicitly set — see internal/lsp/trust.go.
	Trust bool `koanf:"trust"`
}

// ProcessToolConfig declares one external process tool (plugin).
type ProcessToolConfig struct {
	Name        string   `koanf:"name"`
	Description string   `koanf:"description"`
	Command     string   `koanf:"command"`
	Args        []string `koanf:"args"`
	InputSchema string   `koanf:"input_schema"` // JSON Schema as a string
	Capability  string   `koanf:"capability"`   // "read", "write", "execute", "network"
	TimeoutSec  int      `koanf:"timeout_sec"`
}

// MCPServerConfig configures one external MCP server to connect over stdio or HTTP.
type MCPServerConfig struct {
	Name    string            `koanf:"name"`
	Command string            `koanf:"command"`
	Args    []string          `koanf:"args"`
	Env     map[string]string `koanf:"env"`
	Auth    string            `koanf:"auth"` // Bearer token for HTTP servers
	// Capability is the default tool.Capability ("read", "write", "network",
	// "execute", or "spawn") assigned to every tool this server exposes.
	// Empty/unrecognized defaults to "execute" (most restrictive) so an
	// unlabeled or untrusted MCP server cannot bypass the permission gate.
	Capability string `koanf:"capability"`
	// ToolCapabilities overrides Capability per remote tool name for servers
	// that expose a known mix of tools with different risk levels.
	ToolCapabilities map[string]string `koanf:"tool_capabilities"`
	// ScanOutput opts this server's output into a heuristic prompt-injection
	// scan before it reaches the model (P21.6). On by default since
	// P27.13/FIND-12 — a best-effort heuristic (invisible/zero-width
	// characters, base64-encoded payloads) that never blocks or mutates
	// content on a hit, only adds a visible warning inside the existing
	// untrusted-content wrapper, so a false positive is low-cost. A *bool
	// (not bool), mirroring SecurityToolConfig.Enabled, so "unset" (use the
	// default) is distinguishable from an explicit false when merging config
	// layers — a plain bool field has no koanf-level default mechanism for
	// elements of a list like MCP (unlike top-level scalar keys, which
	// defaults() covers). Read via ScanOutputEnabled(), never this field
	// directly. The output's provenance marker noting it came from an
	// external MCP server is always applied regardless.
	ScanOutput *bool `koanf:"scan_output"`
	// ScanArguments opts outbound tool-call arguments bound for this server
	// into a heuristic secret-pattern check before they are forwarded
	// (P24.14, FIND-12). Tool-call arguments are model-constructed and may
	// carry anything the model has read into context, making an untrusted
	// server a potential exfiltration channel. Off by default; a hit logs a
	// Warn naming the server, tool, and matched pattern class — flag-only,
	// the call is never blocked or mutated. The outbound mirror of
	// ScanOutput.
	ScanArguments bool `koanf:"scan_arguments"`
}

// ScanOutputEnabled reports whether c's output goes through the heuristic
// prompt-injection scan (default true, P27.13/FIND-12 — see ScanOutput's
// doc comment).
func (c MCPServerConfig) ScanOutputEnabled() bool {
	return c.ScanOutput == nil || *c.ScanOutput
}

// ProviderConfig selects and configures the model provider.
type ProviderConfig struct {
	Default       string `koanf:"default"`        // adapter name: "anthropic", "openai", "ollama"
	Model         string `koanf:"model"`          // model id
	SmallModel    string `koanf:"small_model"`    // optional fast model for title gen + compaction (falls back to Model)
	MaxTokens     int    `koanf:"max_tokens"`     // response token cap
	BaseURL       string `koanf:"base_url"`       // optional API base override
	MaxRetries    int    `koanf:"max_retries"`    // transient-failure retries; 0 disables
	MaxIterations int    `koanf:"max_iterations"` // max engine turns per run; 0 = harness default (40)
	LoopThreshold int    `koanf:"loop_threshold"` // identical-turn abort count; 0 = harness default (5)
	// ZeroToolNudge (P28.3) bounds the corrective-nudge retries fired when a
	// turn that plainly reads as actionable produces zero tool calls (a model
	// dumping its reasoning as prose instead of calling a tool). 0 = harness
	// default (1 retry); negative disables the nudge entirely.
	ZeroToolNudge   int               `koanf:"zero_tool_nudge"`
	Headers         map[string]string `koanf:"headers"`          // extra HTTP headers sent with every request (e.g. gateway auth)
	Think           *bool             `koanf:"think"`            // controls extended thinking for Ollama reasoning models (nil/false = disable; true = enable)
	ReasoningEffort string            `koanf:"reasoning_effort"` // OpenAI o1/o3 reasoning_effort: "low", "medium", "high", or "" (omit)
	ContextWindow   int               `koanf:"context_window"`   // model context window in tokens; 0 = auto (skips compaction for local models)
	// TaskRouting opts a session's user-facing turns into per-turn model
	// routing (P9.4): a local heuristic classifies each turn as "simple" or
	// "complex" and simple turns run on SmallModel instead of Model. Off by
	// default — this is speculative, opt-in-only work; a daemon that never
	// sets this sees zero behavior change. Has no effect unless SmallModel is
	// also set, mirroring the existing "no small_model configured = no
	// behavior change" precedent guardModel/generateTitle/compaction already
	// follow. Never applies when an explicit per-session /model override
	// (P14.7) is in effect — that override always wins.
	TaskRouting bool `koanf:"task_routing"`
	// Fallback lists ordered (provider, model) pairs tried in order after the
	// primary adapter exhausts its own retries (P5.9). Empty = no failover.
	Fallback []ProviderFallbackConfig `koanf:"fallback"`
	// AllowCloudFallback must be explicitly set to fail over from a local
	// provider (ollama) to a cloud provider (anthropic, openai). Cloud-to-cloud
	// and any-to-local failover never requires this flag. Guards against a
	// local-only session silently sending data off the machine on an outage.
	AllowCloudFallback bool `koanf:"allow_cloud_fallback"`
	// PromptProfile selects the system-prompt/tool-exposure shape (P25.6):
	// "auto" (default) infers from BaseURL — loopback/localhost gets the
	// "local" profile (trimmed prompt, web_search/web_fetch/security_scan/
	// git_pr deferred, repo map capped) tuned for small local models; any
	// other value is the unchanged "default" profile. "local" or "default"
	// force the choice regardless of BaseURL.
	PromptProfile string `koanf:"prompt_profile"`
	// APIKey is populated from the environment, never from config files.
	APIKey string `koanf:"-"`
}

// LocalPromptProfile reports whether the "local" prompt profile (P25.6)
// applies: a trimmed system prompt and deferred network/security tool
// schemas, tuned for the latency and instruction-following limits of small
// local models. An explicit PromptProfile of "local"/"default" wins;
// otherwise this auto-detects from BaseURL resolving to loopback.
func (p ProviderConfig) LocalPromptProfile() bool {
	switch strings.ToLower(strings.TrimSpace(p.PromptProfile)) {
	case "local":
		return true
	case "default":
		return false
	default:
		return isLoopbackBaseURL(p.BaseURL)
	}
}

// IsLoopbackBaseURL reports whether raw is a URL whose host resolves to
// loopback (127.0.0.0/8, ::1, or the literal "localhost"). Exported so
// providerfactory can reuse the same loopback test for the P27.2
// (FIND-03) provider.base_url validation as LocalPromptProfile's local-model
// auto-detection below.
func IsLoopbackBaseURL(raw string) bool {
	return isLoopbackBaseURL(raw)
}

// isLoopbackBaseURL reports whether raw is a URL whose host resolves to
// loopback (127.0.0.0/8, ::1, or the literal "localhost") — the signal used
// to auto-detect a local model server (e.g. Ollama's default
// http://localhost:11434).
func isLoopbackBaseURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ProviderFallbackConfig is one entry in ProviderConfig.Fallback.
type ProviderFallbackConfig struct {
	Provider string `koanf:"provider"` // "anthropic", "openai", or "ollama"
	Model    string `koanf:"model"`    // model id for this fallback
	BaseURL  string `koanf:"base_url"` // optional API base override
}

// ServerConfig configures the local daemon.
type ServerConfig struct {
	Addr string `koanf:"addr"` // host:port the daemon listens on

	// AllowRemote must be explicitly set to bind Addr to a non-loopback
	// address (FIND-08). The daemon's API is protected only by a bearer
	// token with no rate limiting; binding it to a network-reachable address
	// (e.g. "0.0.0.0:4127") silently exposes it to anyone who can reach that
	// address unless the operator has deliberately acknowledged the
	// tradeoff. Loopback addresses (127.0.0.1, ::1, localhost) never require
	// this flag. Off by default.
	AllowRemote bool `koanf:"allow_remote"`

	// SessionWorkdirAllowlist bounds which directories a client may request
	// as a session's working directory (P25.1) once AllowRemote is set: the
	// resolved path must be the daemon's own default workspace (or nested
	// under it) or nested under one of these entries. Ignored — every
	// existing-directory request is accepted — on the default loopback-only
	// bind, where a client is already as trusted as a local shell user.
	// Without this, a remote-accessible daemon combined with per-session
	// workdirs would let any bearer-token holder point a session at an
	// arbitrary directory the daemon process can read, turning it into a
	// filesystem oracle far beyond its own project.
	SessionWorkdirAllowlist []string `koanf:"session_workdir_allowlist"`

	// TLS encrypts client<->daemon traffic (FIND-32/P24.18). On by default
	// since P27.5/FIND-13: the loopback-only bind (AllowRemote above) already
	// limits exposure to off-host attackers, but plaintext HTTP still leaves
	// the bearer token and full conversation content readable to another
	// local account on a shared host with packet-capture privilege — a real,
	// if narrow, gap defense-in-depth closes at effectively no cost (the
	// pinned self-signed cert is auto-generated and every in-repo client
	// already pins it transparently via client.NewFromConfig). See
	// ServerTLSConfig's doc comment for exactly what enabling it does.
	TLS ServerTLSConfig `koanf:"tls"`

	// MaxConcurrentRuns caps how many message-turn runs (POST
	// /sessions/{id}/messages) may be actively executing across all sessions
	// at once (P21.5). A request that would exceed the cap is rejected
	// immediately with 429 rather than queued indefinitely — the per-session
	// sessionSems gate only prevents two runs racing on the *same* session,
	// so with no global cap a caller that fans out many sessions (e.g. a
	// hostile or misbehaving `aegis mcp-serve` client) could still exhaust
	// host resources. Defaults to 10 (P27.12/FIND-14): only top-level
	// HTTP-driven runs consume a slot here — sub-agents spawned in-process by
	// the `agent`/swarm tool run directly through the engine, not through
	// this HTTP path, so a normal single-user TUI/web-UI session (which
	// drives at most one or two runs at a time) never gets close to this
	// ceiling; it exists to bound a lower-trust caller like `aegis
	// mcp-serve`. 0 = unlimited; set explicitly to opt back out.
	MaxConcurrentRuns int `koanf:"max_concurrent_runs"`

	// MaxRunDurationSec aborts a single run once it has been active this many
	// seconds, the same way an interrupted/cancelled request is handled.
	// cost.max_tokens_per_run/budget_usd are the primary spend guardrails;
	// this is a coarser wall-clock backstop for a run that never trips those
	// (e.g. a local model stuck making tool calls with near-zero token cost,
	// or a hostile caller trying to hold a run, and the session/global
	// concurrency slot it occupies, open forever). Defaults to 1800 (30
	// minutes, P27.12/FIND-14) — generous enough for a long agentic run with
	// many tool calls, short enough to reclaim a wedged slot well within a
	// working session. 0 = unlimited; set explicitly to opt back out.
	MaxRunDurationSec int `koanf:"max_run_duration_sec"`

	// SSEBufferSize bounds how many not-yet-flushed SSE events are queued for
	// a single run's HTTP connection before the daemon drops the oldest
	// queued event to make room for the newest (P21.5). Protects daemon
	// memory from a slow or stalled SSE consumer (TUI, web UI, or an
	// mcp-serve client) that reads events slower than the engine produces
	// them; the run itself keeps executing and persisting to the session
	// store regardless of how far behind the client falls. 0 falls back to
	// the built-in default (256).
	SSEBufferSize int `koanf:"sse_buffer_size"`
}

// DefaultSSEBufferSize is the fallback per-connection SSE event queue depth
// used when ServerConfig.SSEBufferSize is left at 0 (P21.5).
const DefaultSSEBufferSize = 256

// ServerTLSConfig configures transport encryption for the daemon's HTTP API
// (FIND-32/P24.18). Loopback binding already keeps client<->daemon traffic
// off the network, but plain HTTP still leaves the bearer token and full
// conversation content observable to another local account on a shared host
// with packet-capture privilege. Enabled defaults to true since P27.5/
// FIND-13 to close that gap; it does not protect against Host/OS-level
// compromise of the same account, which can already read daemon.token (and,
// with TLS enabled, daemon.key) directly off disk — see docs/configuration.md.
//
// When Enabled is true and CertFile/KeyFile are left empty, the daemon
// generates a self-signed ECDSA P-256 certificate on first start and persists
// it under DataDir as daemon.crt/daemon.key (mirroring the daemon.token
// convention — generated once, reused across restarts unless missing). The
// client must be told to trust that specific certificate (see
// client.WithTLS); this is certificate pinning to a file that never leaves
// the local machine, not verification against a public CA or hostname, so
// there is no browser/OS trust store involved and no renewal workflow. Every
// in-repo client goes through client.NewFromConfig, which wires this up
// automatically — a browser opening `aegis ui` is the one consumer that
// isn't pinned and will show a self-signed-certificate warning, which the
// CLI calls out explicitly when TLS is on (see internal/cli/ui.go).
type ServerTLSConfig struct {
	// Enabled turns on TLS for the daemon's HTTP listener and switches
	// client.NewFromConfig to https:// with the pinned cert. On by default
	// (P27.5/FIND-13); set to false to keep the pre-P27.5 plain-HTTP
	// behavior (no cert/key files written, no scheme change).
	Enabled bool `koanf:"enabled"`

	// CertFile/KeyFile let an operator supply their own certificate instead
	// of the auto-generated self-signed one (e.g. one issued by an internal
	// CA). Both empty (the default) means auto-generate/reuse
	// <DataDir>/daemon.crt and <DataDir>/daemon.key.
	CertFile string `koanf:"cert_file"`
	KeyFile  string `koanf:"key_file"`
}

// PermissionConfig sets the default agent permission posture.
type PermissionConfig struct {
	Mode            string   `koanf:"mode"`              // "plan" or "build"
	AutoApproveExec bool     `koanf:"auto_approve_exec"` // auto-approve shell/execute tool calls
	Rules           []string `koanf:"rules"`             // text-based allow/deny rules, e.g. "allow bash(npm test*)", "deny write(/etc/*)"

	// AllowUnsandboxedAutoExec opts into the combination the daemon otherwise
	// refuses to start with (P25.2): auto_approve_exec: true while the
	// effective sandbox backend is the unsandboxed local one. That pairing
	// means every model-issued shell command runs on the host with no
	// approval and no isolation — previously only a startup WARN line, easy
	// to miss, for what amounts to unattended remote code execution. Set
	// this only when that's a deliberate, understood choice (e.g. an
	// already-isolated CI container running Aegis itself).
	AllowUnsandboxedAutoExec bool `koanf:"allow_unsandboxed_auto_exec"`
}

// SecurityConfig configures contextual security policies.
type SecurityConfig struct {
	EgressThenWrite  bool     `koanf:"egress_then_write"` // require approval for writes after network egress
	NetworkAllowList []string `koanf:"network_allowlist"` // restrict network calls to these domains (empty = no restriction)

	// RedactSecrets runs a read-capability tool's output through the same
	// gitleaks-backed secret detection used for PR title/body scanning
	// (security.ScanText, P24.6 / FIND-13) before it's appended to the
	// conversation sent to the configured model provider (P24.12 /
	// FIND-09). Any detected secret pattern is masked as
	// "[REDACTED:<rule>]". Defaults to true (P27.3/FIND-05): sending file/
	// conversation content carrying an unredacted secret to a cloud model
	// provider is a real exposure with no other default control. It
	// remains a best-effort, gitleaks-only pass (needs the binary on PATH,
	// only catches its known secret patterns, fails open if the binary is
	// missing) and shells out per read-tool call, adding latency — set
	// redact_secrets: false only when that cost is unacceptable and
	// content is otherwise known-safe, or prefer local Ollama usage, which
	// never sends file content off the machine at all and remains the
	// strongest mitigation for genuinely sensitive codebases. See
	// docs/providers.md "Data Exposure & Redaction".
	RedactSecrets bool `koanf:"redact_secrets"`

	// Tools configures per-scanner behavior for `aegis scan`/the security_scan
	// tool (P11.11): whether it's enabled, how it runs (host binary vs
	// container image), and its digest-pinned image override. Keyed by
	// scanner name (semgrep, trivy, gitleaks, ...); a name with no entry uses
	// DefaultMethod and runs enabled with no image override.
	Tools map[string]SecurityToolConfig `koanf:"tools"`
	// DefaultMethod is the resolver method for any scanner with no entry in
	// Tools: "host" (never fall back to a container), "container" (always
	// prefer the container image), or "auto"/"" (host if present, else
	// container) — the default.
	DefaultMethod string `koanf:"default_method"`

	// WSLDistro names a specific registered WSL distro (e.g. "kali-linux") to
	// target for every WSLCapable scanner (nmap, nuclei, opengrep, kubescape;
	// P14.x), instead of whatever `wsl --set-default` currently points at.
	// Empty uses WSL's own default-distro selection. On Windows, a Linux
	// distro purpose-built for security tooling (Kali) is the recommended
	// target for red-team/recon work — see docs/security.md.
	WSLDistro string `koanf:"wsl_distro"`

	// DAST configures the dast_scan tool's target-authorization policy
	// (P11.7) — enforced unconditionally inside the tool itself, not just
	// advisory permission rules, since an agent pointing an active scanner
	// at an arbitrary host is an abuse primitive.
	DAST DASTConfig `koanf:"dast"`

	// Debate gates the two opt-in integration points between the P12
	// multi-agent-debate mechanism and existing security workflows (P12.5):
	// threat-model entries and audit-triage findings. Both default false —
	// debate multiplies model calls per item, so it's a deliberate opt-in,
	// never a silent behavior change to the existing single-pass workflows.
	Debate DebateIntegrationConfig `koanf:"debate"`
}

// DebateIntegrationConfig toggles routing specific existing security
// workflows through a P12 debate round before they finalize their output.
// This only controls the instruction text injected into the system prompt
// (server.effectiveSystem) — the model still decides per-item whether a
// debate is warranted; the toggle controls whether it's told to consider one
// at all.
type DebateIntegrationConfig struct {
	// ThreatModel enables the security-architect persona's threat-modeling
	// workflow to route each identified threat/mitigation pair through a
	// debate round before writing it into the threat model document.
	ThreatModel bool `koanf:"threat_model"`
	// Triage enables the security-audit skill's triage loop to route a
	// borderline or disputed-severity finding through a debate round before
	// deciding whether to suppress it via the baseline (P11.8).
	Triage bool `koanf:"triage"`
}

// DASTConfig is the hard authorization gate for DAST scanning (P11.7): a
// dast_scan call always resolves its target's host against this policy
// before ever launching ZAP, regardless of permission mode. Loopback and
// RFC-1918 private addresses are always allowed (the common "scan my
// locally running app" case needs no config); anything else must be
// explicitly declared here.
type DASTConfig struct {
	// AllowedTargets is a list of exact hostnames, ".suffix" subdomain
	// wildcards, or CIDR ranges an operator has explicitly authorized for
	// scanning, in addition to the built-in loopback/RFC-1918 default-allow.
	// Hostnames are matched as literal strings, never DNS-resolved — a
	// target's declared identity can't be silently changed by whatever it
	// happens to resolve to at scan time (ZAP does its own resolution inside
	// the container, outside Aegis's control).
	//
	// Sourced from user/global config only (P27.9/FIND-11): Load()
	// unconditionally overwrites this field with the project-excluded
	// baseline after unmarshalling, so a project .aegis/config.yaml can
	// never widen it — not even once the directory is `aegis trust`-ed,
	// unlike the P27.1 trust gate's other frozen-until-trusted keys. An
	// active scanner authorized against arbitrary Internet hosts via a
	// cloned repo's config is a materially different risk than that repo
	// merely widening its own permission mode.
	AllowedTargets []string `koanf:"allowed_targets"`
	// AllowActive gates active/api scan modes (which send real attack
	// payloads, not just passive observation) behind an explicit one-time
	// opt-in, separate from the per-call approval prompt every dast_scan
	// call already gets from its execute capability. Default false.
	AllowActive bool `koanf:"allow_active"`
}

// SecurityToolConfig configures one security scanner (P11.11).
type SecurityToolConfig struct {
	// Enabled defaults to true (the zero value); set false to always skip
	// this tool. A *bool (not bool) so "unset" is distinguishable from an
	// explicit false when merging config layers.
	Enabled *bool `koanf:"enabled"`
	// Method overrides SecurityConfig.DefaultMethod for this tool: "host",
	// "container", or "auto".
	Method string `koanf:"method"`
	// Install controls whether Aegis may install this tool automatically
	// when missing (P11.10): "prompt" (default — ask before installing),
	// "always" (pre-authorized, no prompt), or "never" (use only if already
	// present, don't offer to install).
	Install string `koanf:"install"`
	// Image is a digest-pinned container image reference
	// (image@sha256:...) used for this tool's container fallback. Required
	// to enable container execution — see security.ScannerDescriptor's doc
	// comment for why Aegis ships no built-in default.
	Image string `koanf:"image"`
	// TemplatesVersion pins the nuclei scanner's nuclei-templates release tag
	// (P13.5.6) — meaningless for every other tool. Templates are executable
	// network-probe logic, so nuclei never runs against an unpinned "latest"
	// template set the same way a scanner container image is never used
	// without a digest pin.
	TemplatesVersion string `koanf:"templates_version"`
	// Verify enables trufflehog's live credential verification (P13.2):
	// each detected secret is confirmed against the real provider API
	// (AWS/GitHub/etc.) instead of just pattern/entropy-matched. Meaningless
	// for every other tool. Default false — verification makes real calls to
	// third-party services using the actual discovered secret, and is
	// host-only (security.Resolve refuses container mode when this is set,
	// the same host-only carve-out image scanning already has).
	Verify bool `koanf:"verify"`
}

// ToolEnabled reports whether c enables the tool (default true).
func (c SecurityToolConfig) ToolEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// OutputGuardConfig sets the default output-validation behaviour applied to
// every persona unless the persona overrides or disables it.
type OutputGuardConfig struct {
	Enabled    bool   `koanf:"enabled"`     // global default; per-session /guard toggles from this
	Mode       string `koanf:"mode"`        // "llm" (default) or "schema"
	Rubric     string `koanf:"rubric"`      // default llm rubric
	MaxRetries int    `koanf:"max_retries"` // corrective retries on failure
}

// PersonaOverride holds per-persona config overrides keyed by persona name.
type PersonaOverride struct {
	Model string `koanf:"model"` // "" = use global provider.model
}

// SkillsConfig controls which of the skills embedded in the Aegis binary
// (see `aegis skills list`) are active for this project/user.
type SkillsConfig struct {
	// BuiltinEnabled names which embedded built-in skills are active. Empty
	// by default: built-ins ship in the binary but stay dormant (no
	// system-prompt cost) until named here, via `aegis skills enable
	// <name>`, or the /skills TUI command. Project-local (.aegis/skills) and
	// user (~/.aegis/skills) skill files are unaffected by this list — those
	// are always active since a user chose to author them.
	BuiltinEnabled []string `koanf:"builtin_enabled"`
}

// DefaultGuardRubric is the generic quality rubric applied when output guarding
// is on and a persona declares no rubric of its own.
const DefaultGuardRubric = "The response must directly and completely address the request, " +
	"contain no unfinished work (TODO markers, \"left as an exercise\", stubbed-out logic), " +
	"and ground factual claims in tool output where applicable. Example or placeholder values " +
	"clearly used as such in documentation (e.g. an illustrative IP address, hostname, or " +
	"<your-api-key>-style token) are acceptable, especially when the real value depends on the " +
	"reader's own environment and was never supplied to the model."

// DiagramConfig configures diagram rendering.
type DiagramConfig struct {
	KrokiURL string `koanf:"kroki_url"` // Kroki endpoint for multi-format rendering
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
		"provider.default":        "anthropic",
		"provider.model":          "claude-opus-4-8",
		"provider.max_tokens":     32768,
		"provider.max_retries":    4,
		"provider.prompt_profile": "auto",
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
		"swarm.backend":                          "in_process",
		"sandbox.backend":                        "local",
		"sandbox.image":                          "ubuntu:22.04",
		"sandbox.network":                        false,
		"security.egress_then_write":             false,
		"security.redact_secrets":                true,
		"security.default_method":                "auto",
		"security.dast.allow_active":             false,
		"security.debate.threat_model":           false,
		"security.debate.triage":                 false,
		"output_guard.enabled":                   true,
		"output_guard.mode":                      "llm",
		"output_guard.max_retries":               1,
		"output_guard.rubric":                    DefaultGuardRubric,
		"tui.humor_mode":                         true,
		"tui.theme":                              "dark",
		"tui.notifications":                      "both",
		"tui.image_rendering":                    "auto",
		"embeddings.enabled":                     false,
		"embeddings.provider":                    "ollama",
		"embeddings.model":                       "nomic-embed-text",
		"embeddings.base_url":                    "http://localhost:11434",
		"mcp_server.auto_approve":                false,
		"mcp_server.default_mode":                "plan",
		// Heuristic invisible-char/base64 prompt-injection scan of
		// web_fetch/web_search output on by default (P27.13/FIND-12) — see
		// SearchConfig.ScanOutput's doc comment.
		"search.scan_output": true,
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
	"embeddings": true,
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

// loadLayers builds a koanf instance from defaults -> global config file ->
// (optionally) project config file -> AEGIS_* env, in precedence order.
// includeProject is false to build the "baseline" layer used by the P27.1
// workspace-trust gate: what the effective config would be with the
// project's .aegis/config.yaml ignored entirely (only user/global settings
// and this process's own environment).
func loadLayers(includeProject bool) (*koanf.Koanf, error) {
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

	if err := k.Load(env.Provider(EnvPrefix, ".", envKeyCallback), nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}
	return k, nil
}

// Load resolves configuration from all layers and returns the result.
func Load() (*Config, error) {
	// Load .aegis/.env before other layers so secrets (MCP tokens, API keys)
	// can be set there without living in version-controlled config files.
	// Real environment variables always override values in the .env file.
	if err := loadDotEnv(filepath.Join(".aegis", ".env")); err != nil {
		return nil, fmt.Errorf("load .aegis/.env: %w", err)
	}

	full, err := loadLayers(true)
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

	baseline, err := loadLayers(false)
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

	// P27.9/FIND-11: recon_scan/dast_scan's target-authorization allowlist is
	// sourced from the user/global baseline only, unconditionally — never
	// from the project layer, trusted or not. This is stronger than the
	// P27.1 trust gate above (which lets a *trusted* project widen
	// permission/sandbox/mcp/hooks): a hostile or merely careless project
	// .aegis/config.yaml widening this specific list would authorize an
	// active scanner (recon_scan/dast_scan) against arbitrary Internet
	// hosts, a different and broader risk shape than the project's own
	// permission mode, so it stays out of the project's control even after
	// `aegis trust`.
	cfg.Security.DAST.AllowedTargets = baseCfg.Security.DAST.AllowedTargets

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

	applyWorkspaceTrust(&cfg, &baseCfg)

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

// securityRelevantDiff returns a human-readable line for each
// security-relevant setting (per FIND-01/FIND-02: permission.*, sandbox.*,
// mcp.servers, notify.webhook, hooks) where full differs from base.
func securityRelevantDiff(full, base *Config) []string {
	var diffs []string
	if !reflect.DeepEqual(full.Permission, base.Permission) {
		diffs = append(diffs, fmt.Sprintf("permission: %+v -> %+v", base.Permission, full.Permission))
	}
	if !reflect.DeepEqual(full.Sandbox, base.Sandbox) {
		diffs = append(diffs, fmt.Sprintf("sandbox: %+v -> %+v", base.Sandbox, full.Sandbox))
	}
	if !reflect.DeepEqual(full.MCP, base.MCP) {
		diffs = append(diffs, fmt.Sprintf("mcp.servers: %d configured -> %d configured", len(base.MCP), len(full.MCP)))
	}
	if full.Notify.Webhook != base.Notify.Webhook {
		diffs = append(diffs, fmt.Sprintf("notify.webhook: %q -> %q", base.Notify.Webhook, full.Notify.Webhook))
	}
	if !reflect.DeepEqual(full.Hooks, base.Hooks) {
		diffs = append(diffs, fmt.Sprintf("hooks: %d configured -> %d configured", len(base.Hooks), len(full.Hooks)))
	}
	return diffs
}

// applyWorkspaceTrust implements the P27.1 workspace-trust gate: a project's
// .aegis/config.yaml is auto-merged with no confirmation today (FIND-02),
// letting a cloned repository silently widen permission.mode, add an
// attacker MCP server, set a notify.webhook exfiltration channel, or run
// session_start/pre_tool_use hooks (FIND-01). Until an operator explicitly
// trusts the current directory (`aegis trust`), the security-relevant keys
// are frozen to their user/global values — cfg (already unmarshalled with
// the project layer applied) is mutated in place to fall back to baseline
// (project layer excluded) for exactly those keys.
func applyWorkspaceTrust(cfg, baseline *Config) {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	cfg.WorkspaceTrust = WorkspaceTrustStatus{Dir: dir}

	// Nothing to gate if there's no project config file — the diff would be
	// empty anyway, but skip the trust-store lookup/allocation entirely.
	if _, err := os.Stat(ProjectConfigPath()); err != nil {
		cfg.WorkspaceTrust.Trusted = true
		return
	}

	store := workspacetrust.Open(WorkspaceTrustStorePath())
	trusted := store.IsTrusted(dir)
	cfg.WorkspaceTrust.Trusted = trusted

	diffs := securityRelevantDiff(cfg, baseline)
	if trusted || len(diffs) == 0 {
		return
	}

	cfg.Permission = baseline.Permission
	cfg.Sandbox = baseline.Sandbox
	cfg.MCP = baseline.MCP
	cfg.Notify.Webhook = baseline.Notify.Webhook
	cfg.Hooks = baseline.Hooks
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
		return os.Getenv("OPENAI_API_KEY")
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

// LogPath returns the path to the harness log file.
func (c *Config) LogPath() string {
	return filepath.Join(c.DataDir, "aegis.log")
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
