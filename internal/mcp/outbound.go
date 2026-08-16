package mcp

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/fiddler110/aegis/internal/redact"
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
// The pattern set itself lives in internal/redact (P66.11), which is where it
// moved when the exported transcript became its second consumer: internal/share
// has to filter the same shapes, and two copies of a credential list is how the
// artifact a user hands to someone else comes to be filtered by the older one.
// The scan here is unchanged — flag-only, never blocking, never logging the
// matched text.

// warnOutboundSecrets scans args and, on any hit, logs a Warn identifying
// the target server, the tool being called, and the matched pattern classes.
// It never blocks, drops, or rewrites the call — flag-only, symmetric with
// the inbound scan_output behavior — and never logs the matched text itself.
func warnOutboundSecrets(logger *slog.Logger, server, toolName string, args json.RawMessage) {
	classes := redact.Classes(string(args))
	if len(classes) == 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("mcp outbound argument scan flagged possible secret in tool-call arguments",
		"server", server, "tool", toolName, "patterns", strings.Join(classes, ", "))
}
