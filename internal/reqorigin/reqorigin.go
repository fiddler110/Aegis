// Package reqorigin names which surface originated a session or an audit
// record: the interactive TUI, the web UI, an ACP client, an MCP client, or a
// scripted CLI invocation (P81.14/P80.1).
//
// The value is self-declared by the daemon's own Go integration for each
// surface (internal/tui, internal/server's web handlers, internal/acp,
// internal/mcpserver, internal/cli) — never taken from an argument a remote
// protocol caller controls. An MCP tool call, for instance, only ever
// supplies mode/persona/session_id; mcpserver.Server is the one that decides
// the origin is MCP. That is what makes the stamp meaningful for FIND-21's
// enumerate-then-post attack even though every surface still authenticates
// with the same daemon bearer token (P81.3) — the token proves "a process the
// operator trusts", the origin records "which narrow protocol surface that
// process is speaking", and only the latter bounds what an MCP/ACP *tool
// call* itself can reach.
package reqorigin

import "context"

const (
	// TUI is the interactive terminal UI (`aegis`, bare or --resume).
	TUI = "tui"
	// Web is the browser-served UI. Also the default for a raw HTTP caller
	// that leaves the field unset, since the web frontend has no Go call site
	// to stamp it itself — it POSTs JSON directly from the browser.
	Web = "web"
	// ACP is an editor/harness speaking the Agent Client Protocol
	// (`aegis acp`).
	ACP = "acp"
	// MCP is an external client speaking the Model Context Protocol
	// (`aegis mcp-serve`).
	MCP = "mcp"
	// CLI is a scripted, operator-invoked subcommand that isn't the main TUI
	// (`aegis compare`, `aegis parallel`, ...) — same trust level as TUI (an
	// interactive local shell), tagged separately for legibility in the audit
	// trail.
	CLI = "cli"
)

// Valid reports whether s is one of the known origin values.
func Valid(s string) bool {
	switch s {
	case TUI, Web, ACP, MCP, CLI:
		return true
	default:
		return false
	}
}

// Normalize returns s when it's a known origin, else Web — the safe default
// for a caller that didn't declare one (the browser UI has no Go call site to
// stamp it, and an unrecognised value is treated the same way rather than
// trusted verbatim).
func Normalize(s string) string {
	if Valid(s) {
		return s
	}
	return Web
}

type ctxKey struct{}

// WithOrigin attaches an origin to ctx for the duration of a run, so a hook
// invoked deep in the engine (internal/hooks.Audit) can stamp its record
// without every intermediate signature carrying an extra parameter.
func WithOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, ctxKey{}, origin)
}

// FromContext returns the origin WithOrigin attached, or "" if none was.
func FromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}
