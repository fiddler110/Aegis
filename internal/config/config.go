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
	"github.com/fiddler110/aegis/internal/provider/sse"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/toolshim"
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

// RepoMapConfig sizes the structural repository map injected as <repo_map>
// (P62.1). Both knobs were previously compile-time constants in
// internal/repomap, which made the map's shape a property of the binary rather
// than of the model it feeds — the byte budget in particular was calibrated as
// a ~2000-token slice of a small context window, so an operator running a
// 128k-context model had no way to spend 1% of it on a better map.
//
// The two knobs trade against each other: MaxBytes is the total spend and
// MaxSymbolsPerFile decides whether that spend buys depth on a few files or
// breadth across many.
type RepoMapConfig struct {
	// MaxBytes caps the rendered map; 0 falls back to repomap.DefaultMaxBytes
	// (8000, ~2k tokens). Spelled as a plain int rather than resolved here so
	// the fallback stays in one place — internal/repomap owns what "unset"
	// means, and this package deliberately carries no dependency on it.
	MaxBytes int `koanf:"max_bytes"`
	// MaxSymbolsPerFile caps how many symbols any one file contributes; 0 falls
	// back to repomap.DefaultMaxSymbolsPerFile (3) and a *negative* value means
	// uncapped. The negative sentinel is why nothing clamps this to a
	// non-negative range on load: "render every symbol you found and let
	// MaxBytes do the truncating" is a legitimate setting for a large-context
	// model, and clamping it to 0 would silently reinstate the default instead.
	MaxSymbolsPerFile int `koanf:"max_symbols_per_file"`
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
	// Theme selects the TUI color scheme: "auto" (default — detect the
	// terminal's light/dark background at startup, P40.5), "dark", "light", an
	// embedded builtin (catppuccin, dracula, gruvbox, tokyonight), or a
	// custom name loaded from .aegis/themes/<name>.json (project) or
	// ~/.aegis/themes/<name>.json (user) — see internal/tui/theme_loader.go.
	Theme string `koanf:"theme"`
	// Notifications controls the P16.1 attention system fired on stream-end,
	// approval-pending, and error while the terminal isn't focused: "off",
	// "bell", "desktop", or "both" (default).
	Notifications string `koanf:"notifications"`
	// ImageRendering controls the P16.9 inline thumbnail shown in the
	// transcript when an image is attached: "auto" (default — the kitty
	// graphics protocol when the terminal answers P67.9's capability query
	// saying it speaks it, else a truecolor half-block thumbnail when the
	// detected color profile supports it), "off", "halfblock" (force the
	// half-block tier), or "kitty" (P40.4 — force the graphics protocol on a
	// terminal that supports it but answers no queries). The probe itself has
	// its own escape hatch, AEGIS_TERM_CAPS (see internal/termcaps).
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

// ToolsConfig tunes which optional built-in tool families are registered.
type ToolsConfig struct {
	// Families names deferred tool families to register that the active
	// prompt profile would otherwise omit (P62.6). Additive, and empty by
	// default, so an unset key is unambiguous — the same shape as
	// skills.builtin_enabled, and for the same reason.
	//
	// It exists because the local prompt profile drops the coordination,
	// scheduling and long-term-memory families ("team", "cron", "entity"):
	// thirteen of the twenty-six deferred tools, advertised on every turn of
	// every session, for capabilities a small-model file-scoped task does not
	// reach for. The default profile registers everything and ignores this
	// key. Naming a family here puts it back — a local model driving a swarm
	// is a real configuration, just not the one the profile is tuned for.
	//
	// See builtin.ToolFamilies for the recognized names; an unknown name is a
	// no-op rather than an error, since a family can be retired.
	Families []string `koanf:"families"`
}

// WorkspaceConfig widens what the session's workspace-confined tools can
// reach beyond the single directory Aegis was started in (P52.13).
type WorkspaceConfig struct {
	// AdditionalRoots names directories outside the session workdir that
	// workspace-confined tools may resolve paths into. This exists for the
	// cross-repo shape the single-root model makes inexpressible: read
	// research artifacts out of repo A, write the formal document into repo
	// B. The alternative — starting Aegis from a common parent — works but
	// widens confinement far past what the task needs and inflates the repo
	// map.
	//
	// Each root needs its own `aegis trust` decision; an additional root does
	// not inherit the primary workspace's trust (a trusted project must not
	// be able to nominate an untrusted directory and have it silently
	// honored). Untrusted entries are dropped with a warning rather than
	// failing the load.
	AdditionalRoots []AdditionalRoot `koanf:"additional_roots"`
}

// AdditionalRoot is one entry of workspace.additional_roots.
type AdditionalRoot struct {
	// Path is the directory to admit. A relative path resolves against the
	// session workdir, so `../research` in a project config means what it
	// looks like.
	Path string `koanf:"path"`
	// Writable admits writes as well as reads. Off by default — the common
	// case is reading source material out of the additional root, and
	// read-only is a cheap, meaningful restriction on a directory the model
	// was not started in.
	Writable bool `koanf:"writable"`
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

// CostConfig configures spend tracking.
type CostConfig struct {
	BudgetUSD float64 `koanf:"budget_usd"` // abort a run past this estimated cost; 0 = unlimited

	// MaxTokensPerRun aborts a run past this cumulative token count (input +
	// output + cache, across every turn); 0 = unlimited. The primary spend
	// guardrail (P10.5): unlike BudgetUSD, it is always enforceable because
	// token counts are present even for unpriced or local/Ollama models where
	// BudgetUSD silently never fires (estimated usage carries no dollar cost).
	// Note this is a *context* budget on any backend that reports the full
	// prompt each turn rather than a delta — see MaxGeneratedTokensPerRun.
	MaxTokensPerRun int `koanf:"max_tokens_per_run"`

	// MaxGeneratedTokensPerRun aborts a run past this cumulative *output* token
	// count; 0 = unlimited (P59.4). The work budget, where MaxTokensPerRun is
	// the context/billing budget.
	//
	// The two are separate keys on purpose. MaxTokensPerRun sums the whole
	// prompt every turn, which is precisely what a priced provider bills — but
	// on Ollama prompt_eval_count is the full prompt each turn rather than a
	// delta, so the sum grows ~O(N²) in conversation length and a cap set with
	// "how much work should this do" in mind aborts far earlier than intended.
	// Overloading one key to mean output tokens on unpriced backends and total
	// tokens elsewhere would make a single config value mean two things
	// depending on the provider; a second key says what it counts in its name
	// and leaves MaxTokensPerRun's meaning intact for everyone already using it.
	MaxGeneratedTokensPerRun int `koanf:"max_generated_tokens_per_run"`

	// MaxWallClockPerRunSec aborts a run that has been going longer than this
	// many seconds; 0 = unlimited (P52.15). The time dimension the other two
	// budgets miss entirely: BudgetUSD is a no-op for unpriced local usage and
	// MaxTokensPerRun defaults to 0, so on a slow local model a run's only real
	// bound is provider.max_iterations (40), which at ~7 tok/s can be hours.
	//
	// Off by default and deliberately so — a wall-clock cap cannot distinguish a
	// stalled run from a slow one making real progress, so any non-zero default
	// would eventually kill legitimate long work. Read via
	// MaxWallClockPerRun(), never this field directly.
	MaxWallClockPerRunSec int `koanf:"max_wall_clock_per_run"`

	// MaxTurnStallSec aborts a run whose current turn has produced no provider
	// stream event and no tool activity for this many seconds; 0 = disabled
	// (P39.17). Default DefaultMaxTurnStallSec. Read via MaxTurnStall(), never
	// this field directly.
	//
	// Unlike MaxWallClockPerRunSec above this one is ON by default, and the
	// difference is not a change of mind about time bounds — it is that the two
	// measure different things. A wall-clock cap cannot distinguish a stalled
	// run from a slow one making real progress, so it must stay opt-in. This
	// measures *silence*: no token streamed, no tool started or finished. There
	// is no legitimate long-running work that looks like that, which is why it
	// can carry a default at all.
	//
	// It is also the only bound covering the tool-execution half of a turn;
	// provider.stream_idle_timeout covers the model half at the transport, and
	// only for adapters that speak HTTP.
	MaxTurnStallSec int `koanf:"max_turn_stall"`

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

// MaxWallClockPerRun returns the configured cost.max_wall_clock_per_run as a
// time.Duration, or 0 when unset or non-positive — which the engine reads as
// "no wall-clock bound" (P52.15).
func (c CostConfig) MaxWallClockPerRun() time.Duration {
	if c.MaxWallClockPerRunSec <= 0 {
		return 0
	}
	return time.Duration(c.MaxWallClockPerRunSec) * time.Second
}

// DefaultMaxTurnStallSec is the shipped cost.max_turn_stall (P39.17): fifteen
// minutes of complete silence before a turn is called hung.
//
// The number is chosen to be a *backstop*, not a competitor. Layer-specific
// timeouts already bound pieces of a turn, and the stall detector must not fire
// before any of them has had its chance, or it would convert a precise,
// locally-reported failure into a vague one:
//
//   - provider.stream_idle_timeout — 10 minutes (sse.DefaultStreamIdleTimeout),
//     the gap between two streamed chunks.
//   - the shell tool's per-call ceiling — 600s = 10 minutes, the longest a
//     single tool call can legitimately block with no output.
//   - the cron job timeout — 10 minutes.
//   - every other per-call tool bound, enumerated by
//     builtin.TestToolTimeoutsStayUnderTheStallBound.
//
// Fifteen minutes clears them all with margin. It is also far below the hours a
// legitimate phased drive runs for, which is the gap P39.17 was filed about: the
// 2026-08-09 hang had been silent 14 minutes when it was noticed by hand, and
// nothing in the harness would ever have noticed it.
//
// **The relation is "above every *per-call* bound", not "above every timeout",
// and the distinction is load-bearing** (P66.8 / ARCH-04). Two bounds in
// internal/tool/builtin are deliberately larger — the agent tool's workflow
// batch (up to 9 teammates) and its debate (up to 2*rounds+2 roles) — and until
// P66.8 that made this comment false rather than nuanced: 40 and 80 minutes of
// silence against a 900s bound, aborting a healthy fan-out as a fatal
// ErrTurnStalled. What makes them admissible now is that each decomposes into
// per-teammate waits capped at 10 minutes with a heartbeat between them, so an
// aggregate bound can never be *reached* without observable activity. A future
// timeout above 900s needs the same treatment or it is a regression; the
// enumerating test above is what enforces the choice.
const DefaultMaxTurnStallSec = 900

// MaxTurnStall returns cost.max_turn_stall as a time.Duration, or 0 when set to
// zero or negative — which the engine reads as "no stall detection" (P39.17).
//
// Note the asymmetry with MaxWallClockPerRun: there, 0 is the shipped state; here
// 0 is an explicit opt-out, and the default is applied by the defaults layer
// rather than here, so a user who writes `max_turn_stall: 0` genuinely gets it
// off rather than silently getting the default back.
func (c CostConfig) MaxTurnStall() time.Duration {
	if c.MaxTurnStallSec <= 0 {
		return 0
	}
	return time.Duration(c.MaxTurnStallSec) * time.Second
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
	ZeroToolNudge int `koanf:"zero_tool_nudge"`
	// Temperature overrides the backend's sampling temperature. nil (unset)
	// leaves it to the backend, which is rarely what an agentic run wants:
	// Ollama's default is 0.8 unless a Modelfile says otherwise, so two runs
	// of the same prompt against the same model take visibly different paths.
	// Measured (P38.1 re-test, 2026-08-09): one run opened by writing a file,
	// the next opened with an unprompted web search, same prompt and model.
	// A tool-dispatching run is a decision process, not a creative one — set
	// this to 0 when reproducibility matters more than variety.
	Temperature *float64 `koanf:"temperature"`
	// Seed pins the backend's RNG so a run at a fixed temperature is
	// reproducible turn for turn. nil (unset) leaves the backend to pick.
	// Only meaningful alongside Temperature: a seed at temperature 0.8 fixes
	// which of many paths is taken, not that there is only one.
	Seed            *int              `koanf:"seed"`
	Headers         map[string]string `koanf:"headers"`          // extra HTTP headers sent with every request (e.g. gateway auth)
	Think           *bool             `koanf:"think"`            // controls extended thinking for Ollama reasoning models (nil/false = disable; true = enable)
	ReasoningEffort string            `koanf:"reasoning_effort"` // OpenAI o1/o3 reasoning_effort: "low", "medium", "high", or "" (omit)
	ContextWindow   int               `koanf:"context_window"`   // model context window in tokens; 0 = auto (skips compaction for local models)
	// VRAMBudgetGB is how much memory, in GiB, the operator is willing to let
	// the model server hold across *every* concurrently resident model (P69.6).
	// It is what makes a co-resident plan possible: since P69.1 each debate seat
	// resolves its own model, so a debate holds two or three models in VRAM at
	// once, and context_window alone can only size one of them at a time — each
	// as if it were alone.
	//
	// It is a figure the operator states, never one Aegis detects. No GPU/VRAM
	// introspection is attempted on any platform (P17.5, and P20.3 rejected it
	// again), and the number wanted here is not the card's capacity anyway: it is
	// capacity minus whatever the driver reserve and the desktop compositor
	// already hold, which only the operator can see. ~14.5 of a 16 GB card is the
	// measured figure on the machine P69.5/P69.6 were calibrated against.
	//
	//   0 (unset) — no resident-set planning at all. Every path keeps the
	//               behavior it had before P69.6; the feature exists only for an
	//               operator who opted in.
	//   n > 0     — plan every co-resident set against n GiB.
	//
	// Only meaningful for a local Ollama backend; a cloud provider has nothing
	// resident to budget. Read via VRAMBudgetBytes(), never this field directly.
	VRAMBudgetGB float64 `koanf:"vram_budget_gb"`
	// AutofitContext lets the daemon size the serving context window from
	// vram_budget_gb at startup and on first use of a newly-selected model,
	// rather than serving whatever context_window says (P72.1).
	//
	// The fit itself is gated on vram_budget_gb, not on this flag: with a budget
	// stated and *no* context_window configured, the daemon already has nothing
	// to contradict and fits. This flag answers the other case — a
	// context_window that IS set. That number is frequently load-bearing (the
	// debate topology's 16k pin is documented as such), and silently replacing a
	// figure an operator worked out is the one thing P72.1 was filed saying not
	// to do. So overriding a configured window is a separate, explicit "yes".
	//
	//   false (default) — fit only when context_window is unset.
	//   true            — fit always, and let the fitted window override a
	//                     configured one (announced in the log, never written
	//                     back to config.yaml).
	//
	// Like vram_budget_gb it is a property of the operator's machine, so it is
	// frozen from project config (see freeze.go).
	AutofitContext bool `koanf:"autofit_context"`
	// KVCacheType declares the element type Ollama stores K and V in — its
	// OLLAMA_KV_CACHE_TYPE, llama.cpp's --cache-type-k/v. "" (unset) means f16,
	// Ollama's default; "q8_0" roughly halves the cache and "q4_0" roughly
	// quarters it.
	//
	// This is a declaration, not a discovery: Ollama does not report the setting
	// over its API, so an operator running a quantized cache must say so, or every
	// window is planned against roughly twice the bytes it actually costs —
	// erring safe, but wasting most of the headroom the quantization bought. A
	// declaration in the wrong direction is caught empirically by Ollama's own
	// size/size_vram split (ollamainfo.Footprint.FullyOnGPU) rather than silently
	// believed. An unrecognized value is treated as f16; KVCacheTypeValid reports
	// whether it was recognized, so a typo can be named rather than left looking
	// like a working setting.
	KVCacheType string `koanf:"kv_cache_type"`
	// KeepAlive controls how long Ollama keeps the model resident after a
	// request, via the native adapter's keep_alive field (P33.10). Only the
	// native "ollama" adapter honors it; the OpenAI-compat path cannot send it.
	// Accepts a Go duration ("30m") or an integer number of seconds; "-1" pins
	// the model in memory forever, "0" unloads it immediately. "" (unset) is
	// NOT passed through as Ollama's 5m default: providerfactory substitutes
	// a bounded resident default (providerfactory.defaultOllamaKeepAlive, 30m)
	// so a multi-turn agentic run reuses its KV cache across turns instead of
	// reprocessing the whole conversation each turn (P35.4). It is still never
	// defaulted to "-1" — the model unloads once a run goes idle, so RAM is
	// held only during active work (the limited-RAM concern behind P33.10).
	KeepAlive string `koanf:"keep_alive"`
	// ResponseHeaderTimeoutSec bounds how long a streamed request waits for
	// the response headers, in seconds (P35.5). Shared by every adapter via
	// sse.NewStreamingClient. Ollama withholds the response header until
	// prompt-eval (prefill) finishes, so a legitimately slow prefill on a
	// large local context can trip the default and abort the whole turn as a
	// transport error before any content streams. 0 (unset) keeps the
	// default (sse.DefaultResponseHeaderTimeout, 30 minutes as of P38.1) so
	// behavior is unchanged unless a user opts in to raising it further. Read
	// via ResponseHeaderTimeout(), never this field directly.
	ResponseHeaderTimeoutSec int `koanf:"response_header_timeout"`
	// StreamIdleTimeoutSec bounds the gap between two streamed chunks, in
	// seconds (P59.2). ResponseHeaderTimeoutSec above stops applying the moment
	// the headers arrive, and the streaming client deliberately has no overall
	// timeout, so before this key a model runner that wedged mid-generation left
	// the turn blocked on a read indefinitely — and cost.max_wall_clock_per_run
	// could not help, because it is checked between turns, never inside one.
	// 0 (unset) keeps the default (sse.DefaultStreamIdleTimeout, 10 minutes);
	// a negative value disables the bound. Read via StreamIdleTimeout(), never
	// this field directly. Honored by every adapter: it reached only the native
	// ollama one until P61.1, which is worse than it sounds — the openai adapter
	// is a local path too (Ollama's /v1 compat endpoint), so the backend most
	// likely to wedge was half unprotected by a key users read as global.
	StreamIdleTimeoutSec int `koanf:"stream_idle_timeout"`
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
	// ToolCallProbeTrials is how many times the tool-calling smoke probe
	// (internal/toolcallprobe) is run when measuring a model's conformance
	// *rate* rather than a yes/no verdict (P53.4). Default
	// toolcallprobe.DefaultTrials (5) — the sample SmokeMaxTokens's own
	// calibration used. 1 reduces the probe to a single trial, exactly the
	// pre-P53.4 behavior, and disables the daemon's background refinement
	// entirely. Only ever applies to local Ollama-style providers, the same
	// scope the probe itself has. In the daemon the first trial is the only
	// one on the message path — the rest run in the background — so raising
	// this never adds first-message latency; in `aegis doctor` it is fully
	// blocking, which is why that command announces the trial count before
	// starting.
	ToolCallProbeTrials int `koanf:"tool_call_probe_trials"`
	// ModelCapabilities pre-declares per-model quirks, keyed by model name
	// (P53.5). Aegis normally discovers these by probing — sending `think` and
	// taking the 400, running the tool-calling smoke probe — and persists what
	// it learns under <data_dir>/model_caps.json. A declaration here outranks
	// anything discovered, which serves two purposes: telling Aegis about a
	// model it has never met so the failing request is never sent even once,
	// and overriding a persisted verdict that has gone stale without having to
	// delete the cache file.
	//
	//   provider:
	//     model_capabilities:
	//       "mythos-sec:24b":
	//         think: false          # never send the think parameter
	//       "some-model:latest":
	//         tool_calling: ok      # or "unsupported"
	//
	// Unset fields declare nothing and leave discovery in charge; `think:
	// false` is a declaration of non-support, not merely a default.
	ModelCapabilities map[string]modelcaps.Declared `koanf:"model_capabilities"`
	// ToolCallShim opts a session into the non-native tool-calling fallback
	// (P53.6, internal/toolshim): tool schemas are serialized into the system
	// prompt and the model's tagged JSON is parsed back into tool calls, for
	// models that cannot speak the provider's tool protocol at all — the
	// qwen2.5-coder:1.5b signature Aegis already detects and, without this,
	// only warns about.
	//
	//   provider:
	//     tool_call_shim: on     # "off" (default) | "on"
	//
	// Deliberately explicit-only. Auto-engaging it off a low measured
	// conformance rate (internal/modelcaps) is a follow-up that is only sound
	// once that rate is trustworthy, so "auto" is rejected rather than
	// silently accepted as a no-op. Off is also the safe direction: a shim
	// that quietly turns prose into executable tool calls is a security
	// surface, not a convenience. Parsed calls still pass through the same
	// permission gate, capability check, and workspace confinement as native
	// ones — the shim changes how a call arrives, never what it is allowed to
	// do. An unrecognized value is treated as off; ToolCallShimValid reports
	// whether it was recognized, so a caller can say so rather than leaving a
	// typo looking like a working setting.
	ToolCallShim string `koanf:"tool_call_shim"`
	// MaxConcurrentRequests bounds how many requests may be in flight against
	// the backend at once (P59.9); the rest queue in the adapter until a slot
	// frees. It is a policy the operator sets, not a capacity Aegis infers —
	// no VRAM detection is attempted (P20.3/P17.5 both rejected that).
	//
	//   0 (unset) — auto: local backends get MaxConcurrentRequestsDefaultLocal,
	//               cloud backends stay unbounded.
	//   n > 0     — at most n concurrent requests, whatever the backend.
	//   negative  — explicitly unbounded, including for a local backend.
	//
	// Auto bounds local backends because a single Ollama server is one GPU:
	// every concurrent request is built believing it owns the full detected
	// num_ctx, while Ollama splits its KV cache across OLLAMA_NUM_PARALLEL
	// slots and evicts models to fit. Queueing is honest there; oversubscribing
	// is not. Cloud endpoints fan out across a fleet, so bounding them by
	// default would only slow multi-agent work down.
	MaxConcurrentRequests int `koanf:"max_concurrent_requests"`
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

// VRAMBudgetBytes returns provider.vram_budget_gb in bytes, or 0 when no budget
// is stated (P69.6). Zero is the "plan nothing" value every caller checks, and a
// negative figure reads as zero rather than as an error: a budget is an opt-in
// hint about hardware, and refusing to start the daemon over a mistyped one
// would be a worse trade than ignoring it. The daemon warns instead.
func (p ProviderConfig) VRAMBudgetBytes() int64 {
	if p.VRAMBudgetGB <= 0 {
		return 0
	}
	return int64(p.VRAMBudgetGB * float64(int64(1)<<30))
}

// KVCacheTypeValid reports whether provider.kv_cache_type names a KV cache
// element type Aegis knows how to size. The names mirror
// ollamainfo.KVCacheType and are spelled as literals here for the same reason
// provider.tool_call_probe_trials' default is — the config package stays free of
// a dependency on the package that consumes the value.
func (p ProviderConfig) KVCacheTypeValid() bool {
	switch p.KVCacheType {
	case "", "f16", "q8_0", "q4_0":
		return true
	}
	return false
}

// ResponseHeaderTimeout returns the configured
// provider.response_header_timeout as a time.Duration, substituting
// sse.DefaultResponseHeaderTimeout (30 minutes as of P38.1) when unset or
// non-positive (P35.5).
func (p ProviderConfig) ResponseHeaderTimeout() time.Duration {
	if p.ResponseHeaderTimeoutSec <= 0 {
		return sse.DefaultResponseHeaderTimeout
	}
	return time.Duration(p.ResponseHeaderTimeoutSec) * time.Second
}

// StreamIdleTimeout returns the configured provider.stream_idle_timeout as a
// time.Duration (P59.2), substituting sse.DefaultStreamIdleTimeout when unset.
// A negative configured value is passed through as a negative duration, which
// the adapter reads as "disable the bound" — distinct from unset, so a user who
// wants a legitimately unbounded stream can say so.
func (p ProviderConfig) StreamIdleTimeout() time.Duration {
	if p.StreamIdleTimeoutSec == 0 {
		return sse.DefaultStreamIdleTimeout
	}
	return time.Duration(p.StreamIdleTimeoutSec) * time.Second
}

// MaxConcurrentRequestsDefaultLocal is the in-flight request bound applied to a
// local backend when provider.max_concurrent_requests is unset (P59.9). One,
// because a local server is a single-GPU resource: two concurrent requests do
// not get two GPUs, they get one GPU time-slicing between them.
//
// Measured (2026-08-05, Ollama 0.30.10 / qwen3:14b / 16GB card) rather than
// assumed — see the admissionAdapter doc comment in internal/provider for the
// full numbers and for what the measurement *corrected*. In short: concurrency
// here is a latency cost, not the correctness hazard this constant was
// originally justified by. Four concurrent ~12k-token requests were not
// truncated, so the bound is chosen for turn latency (K=4 costs ~70% worse p50
// for ~60% more aggregate throughput), which is the wrong trade for an
// interactive agent and a defensible one for a batch of sub-agents. An operator
// running swarm work on a host with room raises it explicitly, and gives up
// nothing but latency headroom by doing so.
const MaxConcurrentRequestsDefaultLocal = 1

// AdmissionLimit resolves provider.max_concurrent_requests for one concrete
// (provider name, base URL) pair, returning the number of requests allowed in
// flight against it — 0 meaning unbounded, the value
// provider.WithAdmissionControl reads as "add no layer at all".
//
// It takes the pair rather than reading p.Default/p.BaseURL because a fallback
// target is a different backend with the same policy: a local primary with a
// cloud fallback must not carry its queue depth over to the cloud, and a
// local-to-local fallback must.
func (p ProviderConfig) AdmissionLimit(name, baseURL string) int {
	if p.MaxConcurrentRequests > 0 {
		return p.MaxConcurrentRequests
	}
	if p.MaxConcurrentRequests < 0 {
		return 0 // explicitly unbounded, local included
	}
	if LocalBackend(name, baseURL) {
		return MaxConcurrentRequestsDefaultLocal
	}
	return 0
}

// LocalBackend reports whether a (provider, base URL) pair names a model server
// running on this machine — the native Ollama adapter, or any OpenAI-compatible
// adapter pointed at a loopback address (LM Studio, llama.cpp, a local proxy).
// This is the "one GPU, not a fleet" test AdmissionLimit gates on, and it is
// deliberately broader than providerfactory's local/cloud *data-residency*
// check: an LM Studio endpoint is not a cloud provider for failover purposes
// either, but the question there is where the data goes and the question here is
// how many requests the hardware can hold.
func LocalBackend(name, baseURL string) bool {
	if strings.EqualFold(strings.TrimSpace(name), "ollama") {
		return true
	}
	return isLoopbackBaseURL(baseURL)
}

// ToolCallShimEnabled reports whether provider.tool_call_shim turns the P53.6
// non-native tool-calling fallback on.
func (p ProviderConfig) ToolCallShimEnabled() bool {
	return toolshim.Enabled(p.ToolCallShim)
}

// ToolCallShimValid reports whether provider.tool_call_shim holds a value the
// shim recognizes ("", "off", "on"). Callers surface a false as a warning: an
// unrecognized value is treated as off, and a user who typed "auto" or "true"
// deserves to be told that rather than to discover it from a run that never
// shimmed anything.
func (p ProviderConfig) ToolCallShimValid() bool {
	return toolshim.ValidMode(p.ToolCallShim)
}

// CompactionConfig tunes context compaction. Everything here is an override of
// an auto-detected default, not a switch that has to be set.
type CompactionConfig struct {
	// PreservePrefixCache overrides the headroom gate on the deterministic prune
	// pre-pass. Unset (nil) auto-detects: on a local backend the gate is on,
	// because rewriting the middle of a conversation discards the
	// llama.cpp/Ollama prefix KV cache and costs a full prefill recompute;
	// against a cloud provider there is no such cache and the gate is off.
	//
	// It is settable at all because the gate is an *optimization*, and an
	// optimization that cannot be switched off cannot be A/B'd or reverted
	// without a rebuild — which is the position P62.2 was stuck in.
	//
	// # The measurement, and why it took two passes to read correctly
	//
	// Measured 2026-08-08, the gate lost badly: 3m19s against 1m32s on the same
	// fixture, with the deferred turns costing ~23.7s each. That reading stood
	// long enough to be acted on, and it was an artifact of a broken instrument.
	// The engine's compaction trigger ran on an estimate that undercounted the
	// real prompt by 20-33% (P62.4), so compaction fired so late that *both*
	// arms were already inside the regime where Ollama context-shifts — where
	// the prefix cache is gone regardless and the gate has nothing left to
	// protect.
	//
	// Re-measured after P62.4 corrected the estimate, on the same fixture and
	// model, twice:
	//
	//	gate on:  1m16s / 1m27s wall, ~54,287ms prefill
	//	gate off: 2m7s  / 2m7s  wall, ~98,481ms prefill
	//
	// The gate now wins ~1.7x on wall clock with no overflows in either arm, and
	// the per-turn trace shows why: once past the trigger, gate-off prunes on
	// *every* turn for a small yield (message counts unchanged, 11->11, 13->13)
	// and pays a full ~9s prefill each time, while gate-on stays append-only at
	// ~2.5s and takes that hit three times.
	//
	// The durable lesson is about method rather than about caching: a
	// measurement of an optimization is only as good as the instrument the
	// system was running on, and this one was measured in a regime that existed
	// only because a different component was broken.
	PreservePrefixCache *bool `koanf:"preserve_prefix_cache"`

	// ColdCacheAfter is the idle gap after which a resumed conversation has its
	// stale, re-fetchable tool results cleared before the next request (P67.6) —
	// a trigger on cache *temperature*, orthogonal to the context-pressure
	// trigger everything else here tunes. "0" or "off" disables it.
	//
	// The default is ColdCacheAfterDefault. It is a duration rather than a bool
	// because the right answer is a property of the backend's cache TTL, and
	// those differ: Ollama unloads an idle model after 5 minutes by default,
	// Anthropic's prompt cache expires after 5 minutes or an hour depending on
	// the tier. Past any of them the prefix this request re-sends has to be
	// recomputed anyway, which is exactly when clearing it becomes free.
	//
	// It is a string so "off" and "20m" are both sayable; use ColdCacheAfterOr.
	ColdCacheAfter string `koanf:"cold_cache_after"`

	// ColdCacheKeep is how many of the most recent clearable tool results the
	// cold-cache pass leaves verbatim. 0 takes the package default (3); the pass
	// itself floors it at 1, because clearing every result leaves the model with
	// no working context at all.
	ColdCacheKeep int `koanf:"cold_cache_keep"`
}

// ColdCacheAfterDefault is the idle gap at which the P67.6 cold-cache pass fires
// when compaction.cold_cache_after is unset.
//
// Twenty minutes, chosen to sit clear of every cache TTL this ships against
// rather than to split the difference between them: Ollama's default keep-alive
// is 5 minutes, Anthropic's default prompt-cache TTL is 5 minutes and its
// extended tier is 1 hour. Below ~5 minutes the pass would fire on a cache that
// is still warm and throw away context for nothing; at 20 minutes the two
// 5-minute TTLs have certainly expired, and the 1-hour tier is a cloud backend
// where the pass costs little either way. It is a default, not a finding — no
// live measurement has been taken at any value, and the config knob exists so
// one can be.
const ColdCacheAfterDefault = 20 * time.Minute

// ColdCacheAfterOr resolves compaction.cold_cache_after to a duration: unset
// means ColdCacheAfterDefault, "off"/"0"/"none" means disabled, anything else is
// parsed as a Go duration. An unparseable value returns the default and false,
// so the caller can warn rather than silently disabling a feature the user was
// trying to tune.
func (c CompactionConfig) ColdCacheAfterOr() (time.Duration, bool) {
	v := strings.TrimSpace(strings.ToLower(c.ColdCacheAfter))
	switch v {
	case "":
		return ColdCacheAfterDefault, true
	case "off", "none", "false", "0":
		return 0, true
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return ColdCacheAfterDefault, false
	}
	return d, true
}

// PreservePrefixCacheOr resolves the tri-state against an auto-detected
// default, which is what the caller would have used before the override
// existed.
func (c CompactionConfig) PreservePrefixCacheOr(auto bool) bool {
	if c.PreservePrefixCache == nil {
		return auto
	}
	return *c.PreservePrefixCache
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

	// TrustProxyHeaders must be explicitly set to make the daemon honor
	// X-Forwarded-Proto (and, for now, only that header — X-Forwarded-For and
	// other forwarded headers are out of scope) from any client able to reach
	// it (P32.10). This exists for operators who run the daemon behind a
	// reverse proxy that terminates TLS itself and forwards plaintext to the
	// loopback daemon: on that backend hop r.TLS is nil even though the
	// browser used HTTPS, so without this flag the web UI's CSRF cookie would
	// be minted without Secure. Only enable this when the daemon sits behind
	// a reverse proxy the operator controls that strips or overwrites any
	// client-supplied X-Forwarded-Proto before forwarding — otherwise a
	// caller with direct network access to the daemon could spoof the header
	// and the protection this flag is meant to restore becomes a lie. Off by
	// default so the existing loopback-plaintext and built-in-TLS (see
	// ServerTLSConfig below) deployment shapes are unaffected.
	TrustProxyHeaders bool `koanf:"trust_proxy_headers"`

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

// GitConfig configures the git-facing built-in tools (currently just the
// pre-commit test gate, P46.2).
type GitConfig struct {
	// PreCommitTestCommand, when set, is a shell command the git_commit tool
	// runs in the workspace before every commit; a non-zero exit aborts the
	// commit and the command's output is returned to the model instead. This
	// makes "tests pass before every commit" a mechanical gate rather than
	// unenforced persona/skill prose a model can drop under context pressure
	// (P46.2). Empty (the default) is a no-op — git_commit stays a straight
	// passthrough, so existing sessions with no test command declared are
	// unaffected. It executes an arbitrary host command, so it is treated as a
	// security-relevant setting frozen under the workspace-trust gate (P27.1):
	// an untrusted project's .aegis/config.yaml cannot introduce or change it.
	PreCommitTestCommand string `koanf:"pre_commit_test_command"`
	// PreCommitTestTimeoutSec bounds how long PreCommitTestCommand may run
	// before it is killed and the commit aborted. 0 falls back to
	// DefaultPreCommitTestTimeoutSec.
	PreCommitTestTimeoutSec int `koanf:"pre_commit_test_timeout_sec"`
}

// DefaultPreCommitTestTimeoutSec is the pre-commit test command's timeout when
// GitConfig.PreCommitTestTimeoutSec is unset.
const DefaultPreCommitTestTimeoutSec = 600

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
	// scanner name (opengrep, trivy, gitleaks, ...); a name with no entry uses
	// DefaultMethod and runs enabled with no image override.
	Tools map[string]SecurityToolConfig `koanf:"tools"`
	// DefaultMethod is the resolver method for any scanner with no entry in
	// Tools: "host" (never fall back to a container), "container" (always
	// prefer the container image), or "auto"/"" (host if present, else
	// container) — the default.
	DefaultMethod string `koanf:"default_method"`

	// Multiscanner configures the single locally-built image that carries
	// every bundled scanner, so an operator provisions once instead of
	// installing each tool (or configuring a container image per tool).
	// Written by `aegis security build-image`; see MultiscannerConfig.
	Multiscanner MultiscannerConfig `koanf:"multiscanner"`

	// Netscanner points at the second locally-built image, the one for tools
	// that scan a remote target rather than local source. Written by
	// `aegis security build-image --netscanner`; see NetscannerConfig.
	Netscanner NetscannerConfig `koanf:"netscanner"`

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

// MultiscannerConfig points at the locally-built image that bundles every
// scanner Aegis can drive against a directory. It exists because the
// alternative — ScannerDescriptor.DefaultImage — is deliberately empty for
// every tool (a scanner image is supply-chain attack surface, so Aegis never
// ships an unverified default), which left the container path resolving to
// MethodNone out of the box unless an operator pinned 16 separate images.
//
// The pin here is an image *ID*, not a registry digest, and that difference is
// the point. A locally-built image has no registry digest at all — RepoDigests
// stays empty until a push or pull — so the `image@sha256:...` reference the
// per-tool Image field requires cannot exist for one. Rather than weaken that
// rule, this path replaces it with a stronger check: `<runtime> image inspect`
// is asked for the image's real ID before the first container run of a scan
// and compared against ImageID. An image rebuilt or retagged behind Aegis's
// back fails closed with a specific reason, instead of a regex on a reference
// string vouching for content it never looked at.
type MultiscannerConfig struct {
	// Enabled turns the shared image on as the container-method image for
	// every bundled scanner. False (the default) leaves resolution exactly as
	// it was: per-tool images only.
	Enabled bool `koanf:"enabled"`
	// Image is the local reference `aegis security build-image` tagged, e.g.
	// "localhost/aegis-multiscanner:v1". Never pulled — it must already exist
	// in the runtime's local storage, and its ID must match ImageID.
	Image string `koanf:"image"`
	// Runtime is the container runtime that built the image ("podman",
	// "docker", ...), recorded because a locally-built image exists only in
	// the storage of the runtime that built it. Without it, resolution would
	// fall back to auto-detection and could pick a different runtime than the
	// build did — DetectBest returns the first available engine in priority
	// order, not the one that built anything, so on a machine with both
	// installed a docker-built image would be reported missing even though it
	// exists, because podman answered first. Empty falls back to
	// auto-detection (the pre-multiscanner behavior).
	Runtime string `koanf:"runtime"`
	// ImageID is the full "sha256:..." ID of the image as built, recorded by
	// `aegis security build-image` and re-verified before use. Empty means
	// "never built" — resolution reports that rather than running whatever
	// currently answers to Image.
	ImageID string `koanf:"image_id"`
	// SourceFingerprint is a hash of the Containerfile and scripts the image
	// was built from, recorded alongside ImageID. It closes the gap ImageID
	// cannot see: an image can match its pin perfectly and still predate the
	// source it claims to be built from, which is how a pinned image went two
	// commits stale and silently lacked a scanner entirely.
	//
	// Empty means "unknown", not "drift" — configs written before this field
	// existed have a perfectly good image, and flagging every one of them on
	// upgrade would be noise, not a finding.
	SourceFingerprint string `koanf:"source_fingerprint"`
	// Concurrency bounds how many scanners run at once during a scan. Each
	// container-method scanner is one container, so this is how many run in
	// parallel. 0 means the built-in default (multiscannerDefaultConcurrency);
	// 1 restores strictly sequential execution.
	Concurrency int `koanf:"concurrency"`
	// Tools optionally restricts which scanners resolve to the shared image.
	// Empty (the default) means every scanner the image is known to carry.
	// A per-tool security.tools.<name>.image always wins over this.
	Tools []string `koanf:"tools"`
}

// NetscannerConfig points at the second locally-built scanner image (P55.7),
// the one carrying tools that scan a *remote* target — nmap and nuclei against
// a host list, trivy/grype against a container image reference.
//
// It is a separate image from the multiscanner, and separate for exactly one
// reason: mount posture. Every tool here needs network egress and none of them
// needs the workspace, so this image runs with network ON and no workspace
// mounted, ever — while the multiscanner keeps --network none with the
// workspace mounted. The two runners are separate functions with separate
// signatures (the netscanner's has no directory parameter at all), so the
// invariant is structural rather than a convention someone has to remember.
//
// Pinning works exactly as MultiscannerConfig's does, for the same reason: a
// locally-built image has no registry digest, so its real image ID is recorded
// and re-verified before every run.
type NetscannerConfig struct {
	// Enabled turns the network-facing image on as the container path for the
	// tools it carries. False (the default) leaves image scanning and network
	// recon host-binary-only, exactly as they were before this image existed.
	Enabled bool `koanf:"enabled"`
	// Image is the local reference `aegis security build-image --netscanner`
	// tagged, e.g. "localhost/aegis-netscanner:v1". Never pulled.
	Image string `koanf:"image"`
	// Runtime is the container runtime that built the image, recorded because a
	// locally-built image exists only in that engine's storage.
	Runtime string `koanf:"runtime"`
	// ImageID is the full "sha256:..." ID as built, re-verified before use.
	// Empty means "never built".
	ImageID string `koanf:"image_id"`
	// SourceFingerprint is a hash of the build context the image came from.
	// Both images are built from one context, so this is the same hash the
	// multiscanner records. Empty means "unknown", not "drift".
	SourceFingerprint string `koanf:"source_fingerprint"`
	// Tools optionally restricts which scanners resolve to this image. Empty
	// (the default) means every scanner the image is known to carry.
	Tools []string `koanf:"tools"`
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

	// Model pins the model guard verdict calls run on, outranking every default
	// (P59.5). Empty means: provider.small_model on a cloud provider, the
	// session's own model on a local Ollama one — where naming a second model
	// evicts the resident one and pays a cold reload on the next turn, costing
	// far more than the cheaper verdict saves. Set this when you have the VRAM
	// to hold both and want the split anyway.
	Model string `koanf:"model"`
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
		"sandbox.backend":     "local",
		"sandbox.image":       "ubuntu:22.04",
		"sandbox.network":     false,
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
