package builtin

import (
	"github.com/fiddler110/aegis/internal/trust"
)

// wrapNetworkScanReport marks a network scanner's report as untrusted external
// content before it re-enters the model's context (P66.15).
//
// A finding's title, location, description and remediation are assembled from
// what the *scanned host* said: nmap's service/product/version fields are the
// banner a remote service chose to print, and ZAP's findings quote the target
// application's own responses. That is the same provenance web_fetch and an
// MCP server's output already carry — content that reaches the model without
// ever passing through the user — and this is the last place in the pipeline
// that still knows where the bytes came from.
//
// Only the two network-facing tools wrap. security_scan reads the workspace,
// whose files the model can already read unwrapped, so wrapping it would be a
// posture change for a different threat rather than closing this one.
//
// The heuristic injection scan is deliberately off: everywhere it is used
// (web, MCP) it is a config-gated opt-in, and a security report is exactly the
// document where a keyword scan would fire on legitimate text. The envelope —
// "this is data, not instructions" — is what the provenance gap actually asked
// for.
func wrapNetworkScanReport(toolName, target, report string) string {
	return trust.Wrap(
		toolName+"_untrusted_output",
		[][2]string{{"target", target}},
		"a security scanner probing a network target (the findings quote what that host returned)",
		report,
		false,
	)
}
