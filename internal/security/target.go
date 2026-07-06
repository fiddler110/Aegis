package security

import (
	"fmt"
	"net"
	"strings"
)

// isHostAllowed is the shared network-target authorization policy (P13.5.2):
// loopback and RFC-1918 private addresses are always allowed (the common
// "scan my locally running app/home lab" case needs no config); any other
// host must be explicitly declared in allowedTargets. Hostnames are matched
// as literal strings — never DNS-resolved — so a declared target's identity
// can't be silently changed by whatever it happens to resolve to at scan
// time (the scanner itself resolves DNS, outside this check's visibility).
//
// Originally DAST-only (isDASTTargetAllowed operated on a parsed URL's
// hostname); generalized so the recon scanners (nmap/nuclei, which take bare
// hosts/IPs/CIDRs, not URLs) share the exact same policy rather than a
// second, potentially-drifting implementation.
func isHostAllowed(host string, allowedTargets []string) (allowed bool, reason string) {
	if strings.TrimSpace(host) == "" {
		return false, "target host is empty"
	}
	if isLoopbackOrPrivateHost(host) {
		return true, ""
	}
	for _, entry := range allowedTargets {
		if hostMatchesAllowEntry(host, entry) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("target host %q is not loopback/private and is not declared in security.dast.allowed_targets", host)
}

func isLoopbackOrPrivateHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A non-IP, non-"localhost" hostname is never auto-allowed: DNS
		// resolution is deliberately not performed here (see doc comment).
		return false
	}
	for _, r := range networkPrivateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast()
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

// networkPrivateRanges is the loopback/RFC-1918/link-local table used by
// every network-target-reaching tool's default-allow policy (DAST, recon) —
// a small, deliberate duplicate of the equivalent table in
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
