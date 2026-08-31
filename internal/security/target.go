package security

import (
	"fmt"
	"net"
	"strings"
)

// isHostAllowed is the shared network-target authorization policy (P13.5.2):
// the local machine — `localhost`, `127.0.0.0/8`, `::1` — is always allowed
// (the "scan my locally running app" case needs no config); *every* other
// host, private-range addresses included, must be explicitly declared in
// allowedTargets. Hostnames are matched as literal strings — never
// DNS-resolved — so a declared target's identity can't be silently changed by
// whatever it happens to resolve to at scan time (the scanner itself resolves
// DNS, outside this check's visibility).
//
// The private-range half of that used to be a default-allow, on the reasoning
// that a home lab needs no configuration. P81.29 (FIND-29) removed it: on a
// developer laptop attached to a corporate network, 10.0.0.0/8 is not the
// operator's home lab, and a model-issued recon_scan could sweep the whole
// LAN with no configuration and no per-target consent
// (security.dast.allow_active gates the aggressive checks, not the passive
// nmap/nuclei sweep). Reaching anything off-box is now a decision an operator
// has to have written down. Link-local (169.254.0.0/16, fe80::/10) is in the
// same position and for the same reason — it is another machine's address,
// not this one's.
//
// Ordering is the whole control: the loopback shortcut must stay *above* the
// allowlist loop (it is the zero-config path) and the private-range check must
// stay *below* it (it only shapes the rejection message, never the verdict).
//
// Originally DAST-only (isDASTTargetAllowed operated on a parsed URL's
// hostname); generalized so the recon scanners (nmap/nuclei, which take bare
// hosts/IPs/CIDRs, not URLs) share the exact same policy rather than a
// second, potentially-drifting implementation.
func isHostAllowed(host string, allowedTargets []string) (allowed bool, reason string) {
	if strings.TrimSpace(host) == "" {
		return false, "target host is empty"
	}
	if isLoopbackHost(host) {
		return true, ""
	}
	for _, entry := range allowedTargets {
		if hostMatchesAllowEntry(host, entry) {
			return true, ""
		}
	}
	if isPrivateNetworkHost(host) {
		// Worth its own sentence: a private address reads as "mine" to an
		// operator, and the refusal is otherwise indistinguishable from a
		// typo'd public hostname.
		return false, fmt.Sprintf("target host %q is a private-network address and is not declared in security.dast.allowed_targets — only the local machine (localhost/127.0.0.0/8/::1) is scannable without configuration", host)
	}
	return false, fmt.Sprintf("target host %q is not the local machine and is not declared in security.dast.allowed_targets", host)
}

// isLoopbackHost reports whether host names this machine's own loopback
// interface — the one target class scannable with no configuration at all.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A non-IP, non-"localhost" hostname is never auto-allowed: DNS
		// resolution is deliberately not performed here (see doc comment).
		return false
	}
	return ip.IsLoopback()
}

// isPrivateNetworkHost reports whether host is a non-public address (RFC-1918,
// unique-local, link-local). It no longer authorizes anything — isHostAllowed
// consults it only to explain a refusal in terms the operator will recognize.
func isPrivateNetworkHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, r := range networkPrivateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return ip.IsLinkLocalUnicast()
}

// hostMatchesAllowEntry checks host against one allowlist entry: an exact
// hostname/IP match, a ".suffix" subdomain wildcard, or (if the entry parses
// as one) a CIDR range containing host's IP.
func hostMatchesAllowEntry(host, entry string) bool {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return false
	}
	if _, cidr, err := net.ParseCIDR(entry); err == nil {
		ip := net.ParseIP(host)
		return ip != nil && cidr.Contains(ip)
	}
	if strings.HasPrefix(entry, ".") {
		return strings.HasSuffix(strings.ToLower(host), strings.ToLower(entry))
	}
	return strings.EqualFold(host, entry)
}

// networkPrivateRanges is the loopback/RFC-1918/link-local table naming the
// address space that is not publicly routable. Since P81.29 it authorizes
// nothing — isHostAllowed uses it only to word a refusal — but it is kept
// whole rather than trimmed, because "is this address public?" is the question
// it answers and the next reader will ask it again. It is a small, deliberate
// duplicate of the equivalent table in
// internal/tool/builtin's SSRF dialer (which uses the opposite polarity: it
// *blocks* these ranges for outbound web_fetch calls) rather than a
// cross-package dependency, matching how internal/sandbox is already kept
// decoupled from internal/config elsewhere in this package.
var networkPrivateRanges = mustParseCIDRs(
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16",
	"::1/128", "fc00::/7", "fe80::/10",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("security: invalid built-in CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}
