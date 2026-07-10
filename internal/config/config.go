// Package config loads layered Aegis configuration.
//
// Precedence (lowest to highest): built-in defaults -> global config file ->
// per-project config file (./.aegis/config.yaml) -> environment
// variables (AEGIS_*). API keys are always read from the environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
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
	// scan before it reaches the model. Off by default (P21.6) — a
	// best-effort check with false positives, meant for MCP sources you
	// haven't explicitly trusted. The output's provenance marker noting it
	// came from an external MCP server is always applied regardless.
	ScanOutput bool `koanf:"scan_output"`
}

// ProviderConfig selects and configures the model provider.
type ProviderConfig struct {
	Default         string            `koanf:"default"`          // adapter name: "anthropic", "openai", "ollama"
	Model           string            `koanf:"model"`            // model id
	SmallModel      string            `koanf:"small_model"`      // optional fast model for title gen + compaction (falls back to Model)
	MaxTokens       int               `koanf:"max_tokens"`       // response token cap
	BaseURL         string            `koanf:"base_url"`         // optional API base override
	MaxRetries      int               `koanf:"max_retries"`      // transient-failure retries; 0 disables
	MaxIterations   int               `koanf:"max_iterations"`   // max engine turns per run; 0 = harness default (40)
	LoopThreshold   int               `koanf:"loop_threshold"`   // identical-turn abort count; 0 = harness default (5)
	Headers         map[string]string `koanf:"headers"`          // extra HTTP headers sent with every request (e.g. gateway auth)
	Think           *bool             `koanf:"think"`            // controls extended thinking for Ollama reasoning models (nil/false = disable; true = enable)
	ReasoningEffort string            `koanf:"reasoning_effort"` // OpenAI o1/o3 reasoning_effort: "low", "medium", "high", or "" (omit)
	ContextWindow   int               `koanf:"context_window"`   // model context window in tokens; 0 = auto (skips compaction for local models)
	// Fallback lists ordered (provider, model) pairs tried in order after the
	// primary adapter exhausts its own retries (P5.9). Empty = no failover.
	Fallback []ProviderFallbackConfig `koanf:"fallback"`
	// AllowCloudFallback must be explicitly set to fail over from a local
	// provider (ollama) to a cloud provider (anthropic, openai). Cloud-to-cloud
	// and any-to-local failover never requires this flag. Guards against a
	// local-only session silently sending data off the machine on an outage.
	AllowCloudFallback bool `koanf:"allow_cloud_fallback"`
	// APIKey is populated from the environment, never from config files.
	APIKey string `koanf:"-"`
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
}

// PermissionConfig sets the default agent permission posture.
type PermissionConfig struct {
	Mode            string   `koanf:"mode"`              // "plan" or "build"
	AutoApproveExec bool     `koanf:"auto_approve_exec"` // auto-approve shell/execute tool calls
	Rules           []string `koanf:"rules"`             // text-based allow/deny rules, e.g. "allow bash(npm test*)", "deny write(/etc/*)"
}

// SecurityConfig configures contextual security policies.
type SecurityConfig struct {
	EgressThenWrite  bool     `koanf:"egress_then_write"` // require approval for writes after network egress
	NetworkAllowList []string `koanf:"network_allowlist"` // restrict network calls to these domains (empty = no restriction)

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
		"data_dir":             defaultDataDir(),
		"log_level":            "info",
		"provider.default":     "anthropic",
		"provider.model":       "claude-opus-4-8",
		"provider.max_tokens":  32768,
		"provider.max_retries": 4,
		"server.addr":          "127.0.0.1:4127",
		// "build" is the intentional default: the agent can read and write
		// files freely, but shell/execute calls still prompt for approval
		// (or are denied non-interactively). Use "plan" for read-only
		// exploration and "auto" (with auto_approve_exec: true) only in
		// fully trusted, sandboxed environments.
		"permission.mode":              "build",
		"permission.auto_approve_exec": false,
		"diagram.kroki_url":            "https://kroki.io",
		"cost.budget_usd":              0.0,
		"cost.max_tokens_per_run":      0,
		"cost.session_cap_usd":         0.0,
		"cost.daily_cap_usd":           0.0,
		"cost.session_token_cap":       0,
		"cost.daily_token_cap":         0,
		"cost.alert_threshold":         0.8,
		"swarm.backend":                "in_process",
		"sandbox.backend":              "local",
		"sandbox.image":                "ubuntu:22.04",
		"sandbox.network":              false,
		"security.egress_then_write":   false,
		"security.default_method":      "auto",
		"security.dast.allow_active":   false,
		"security.debate.threat_model": false,
		"security.debate.triage":       false,
		"output_guard.enabled":         true,
		"output_guard.mode":            "llm",
		"output_guard.max_retries":     1,
		"output_guard.rubric":          DefaultGuardRubric,
		"tui.humor_mode":               true,
		"tui.theme":                    "dark",
		"tui.notifications":            "both",
		"tui.image_rendering":          "auto",
		"embeddings.enabled":           false,
		"embeddings.provider":          "ollama",
		"embeddings.model":             "nomic-embed-text",
		"embeddings.base_url":          "http://localhost:11434",
		"mcp_server.auto_approve":      false,
		"mcp_server.default_mode":      "plan",
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
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
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

// Load resolves configuration from all layers and returns the result.
func Load() (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(defaults(), "."), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	// Load .aegis/.env before other layers so secrets (MCP tokens, API keys)
	// can be set there without living in version-controlled config files.
	// Real environment variables always override values in the .env file.
	if err := loadDotEnv(filepath.Join(".aegis", ".env")); err != nil {
		return nil, fmt.Errorf("load .aegis/.env: %w", err)
	}

	for _, path := range []string{GlobalConfigPath(), ProjectConfigPath()} {
		if _, err := os.Stat(path); err != nil {
			continue // missing config files are fine
		}
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load config %s: %w", path, err)
		}
	}

	// Env: AEGIS_PROVIDER_MODEL -> provider.model, AEGIS_PROVIDER_BASE_URL -> provider.base_url
	// Only the first underscore after a known section prefix becomes a dot;
	// remaining underscores stay as-is so compound field names (base_url,
	// max_tokens, etc.) are preserved.
	sections := map[string]bool{
		"provider": true, "server": true, "permission": true,
		"diagram": true, "cost": true, "swarm": true,
		"sandbox": true, "security": true, "output_guard": true,
		"embeddings": true,
	}
	envCb := func(s string) string {
		s = strings.ToLower(strings.TrimPrefix(s, EnvPrefix))
		if idx := strings.Index(s, "_"); idx > 0 {
			if sections[s[:idx]] {
				return s[:idx] + "." + s[idx+1:]
			}
		}
		return s
	}
	if err := k.Load(env.Provider(EnvPrefix, ".", envCb), nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

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
