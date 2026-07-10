package mcp

import "github.com/fiddler110/aegis/internal/trust"

// wrapUntrusted tags content returned by an MCP server with a provenance
// marker before it reaches model context (P21.6). MCP tool output is
// external input the harness cannot vouch for — a compromised or malicious
// server is a prompt-injection vector — so every result is always framed
// this way, regardless of scan settings. When scan is true, the content is
// additionally run through the shared heuristic injection scan (also used by
// the web_fetch/web_search tools, FIND-04) and any hits are surfaced as a
// visible warning inside the marker rather than dropped, so a legitimate
// tool result is never silently discarded.
func wrapUntrusted(server, source, content string, scan bool) string {
	attrs := [][2]string{{"server", server}, {"source", source}}
	return trust.Wrap("mcp_untrusted_output", attrs, "an external MCP server", content, scan)
}
