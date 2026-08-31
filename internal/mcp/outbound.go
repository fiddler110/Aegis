package mcp

import (
	"encoding/json"
	"fmt"
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
// before the call is forwarded. A hit both logs a Warn identifying the
// server, tool, and matched pattern class — never the matched text itself,
// which would copy the suspected secret into the log — and, since
// P81.5/FIND-05, refuses to forward the call: the whole point of a server an
// operator opted into scanning is that it is untrusted enough to want the
// scan, and forwarding a flagged argument to it anyway made scan_arguments
// an audit trail for exfiltration rather than a control against it.
//
// The pattern set itself lives in internal/redact (P66.11), which is where it
// moved when the exported transcript became its second consumer: internal/share
// has to filter the same shapes, and two copies of a credential list is how the
// artifact a user hands to someone else comes to be filtered by the older one.

// warnOutboundSecrets scans args and, on any hit, logs a Warn identifying the
// target server, the tool being called, and the matched pattern classes,
// returning those classes so the caller can refuse the call (P81.5) — never
// logging the matched text itself. A nil/empty return means nothing matched
// and the call should proceed.
func warnOutboundSecrets(logger *slog.Logger, server, toolName string, args json.RawMessage) []string {
	classes := redact.Classes(string(args))
	if len(classes) == 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("mcp outbound argument scan flagged possible secret in tool-call arguments; refusing to forward the call",
		"server", server, "tool", toolName, "patterns", strings.Join(classes, ", "))
	return classes
}

// outboundSecretRefusal is the tool.Result content for a call withheld by
// warnOutboundSecrets — named once so all three call sites (mcpTool,
// mcpResourceReadTool, mcpPromptGetTool) say the same thing.
func outboundSecretRefusal(classes []string) string {
	return fmt.Sprintf(
		"refusing to forward this call: the arguments appear to contain %s — this looks like an attempt to send a credential to an external MCP server rather than a legitimate call. If this is a false positive, rephrase the call without the flagged value.",
		strings.Join(classes, ", "))
}
