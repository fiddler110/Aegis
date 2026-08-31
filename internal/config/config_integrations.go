package config

// MCPServerModeConfig controls `aegis mcp-serve` (P6.3): exposing this
// daemon's sessions as MCP tools to other MCP-speaking harnesses. This is the
// reverse direction of MCP (below) — Aegis acting as the server rather than
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
	// AutoApproveTools narrows AutoApprove to these tool names. Empty leaves
	// AutoApprove blanket (its historical meaning); a non-empty list grants
	// only the tools named and denies every other approval request, which is
	// the middle setting the on/off switch never had (C1/F2).
	AutoApproveTools []string `koanf:"auto_approve_tools"`
	// AllowCallerModeEscalation lets an MCP client's `mode` tool argument
	// exceed DefaultMode. Off by default: an MCP client is a program holding a
	// token from a file, not the local user, so it may pick any mode at or
	// below what the operator configured and no more. Turning this on restores
	// the pre-C1/F1 behavior, where a caller asking for `auto` got it whatever
	// this config said.
	AllowCallerModeEscalation bool `koanf:"allow_caller_mode_escalation"`
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
