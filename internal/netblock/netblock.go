// Package netblock is the single home for the SSRF private-address blocklist
// and the dialer that enforces it.
//
// It exists because the list used to be kept twice — once in
// internal/tool/builtin/web.go for web_fetch, once in internal/mcp/http.go for
// the HTTP/SSE MCP client, each with its own copy of the CIDR table and its own
// mustParseCIDR. The duplication was deliberate (a comment in mcp/http.go
// argued against a cross-package import of internal/tool/builtin) but it is
// what let the table drift: VULN-03 found *both* copies missing 0.0.0.0/8, the
// IPv6 unspecified address and CGNAT. A leaf package with no dependencies of
// its own gives both callers one table without either importing the other.
//
// Note that internal/security/target.go keeps a separate range table on
// purpose: it answers a different question (is this *scan target* on a private
// network?), not "may this process connect here?".
package netblock

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// privateRanges are the CIDRs a fetch must never reach. Beyond the obvious
// RFC1918 set:
//
//   - 0.0.0.0/8 — "this network". A *connection* to 0.0.0.0 reaches the local
//     host on Linux and Windows alike, and net.IP.IsLoopback is false for it,
//     so it was a straight loopback bypass (http://0.0.0.0:11434 reached the
//     operator's Ollama; :4127 reached the Aegis daemon itself).
//   - 100.64.0.0/10 — CGNAT, which is where tailnet and several cloud fabrics
//     put internal hosts.
//   - 192.0.0.0/24 (IETF protocol assignments) and 198.18.0.0/15 (benchmarking)
//     — non-routable ranges with no legitimate fetch target in them.
//
// Deliberately absent: ::ffff:0:0/96. It looks like belt-and-braces for
// IPv4-mapped addresses, but net.IPNet.Contains reduces it via To4() to
// 0.0.0.0/0.0.0.0, which matches *every* IPv4 address — adding it would block
// the entire public internet. IsPrivate handles mapped addresses correctly
// already, because Contains does the To4() conversion on the candidate.
var privateRanges = []*net.IPNet{
	mustParseCIDR("0.0.0.0/8"),
	mustParseCIDR("10.0.0.0/8"),
	mustParseCIDR("100.64.0.0/10"),
	mustParseCIDR("172.16.0.0/12"),
	mustParseCIDR("192.0.0.0/24"),
	mustParseCIDR("192.168.0.0/16"),
	mustParseCIDR("198.18.0.0/15"),
	mustParseCIDR("127.0.0.0/8"),
	mustParseCIDR("169.254.0.0/16"),
	mustParseCIDR("::1/128"),
	mustParseCIDR("fc00::/7"),
	mustParseCIDR("fe80::/10"),
}

// IsPrivate reports whether ip is on a private, loopback, link-local or
// otherwise internal network and must therefore not be dialed on behalf of a
// model-supplied or project-config-supplied URL.
func IsPrivate(ip net.IP) bool {
	if ip == nil {
		return true // an address we cannot parse is not an address we will dial
	}
	if ip.IsUnspecified() { // 0.0.0.0 and ::
		return true
	}
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast()
}

// SafeDialer is a net.Dialer.DialContext replacement for an http.Transport.
// It resolves the address once and dials the resolved literal IP, which is
// what defeats DNS rebinding between the check and the connect — a re-resolve
// inside the dial would let a hostname answer publicly for the check and
// privately for the connection.
func SafeDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("blocked: %s resolves to no addresses", host)
	}
	for _, ip := range ips {
		if IsPrivate(ip.IP) {
			return nil, fmt.Errorf("blocked: %s resolves to private/internal address %s", host, ip.IP)
		}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

// ValidateNotPrivate checks a URL's hostname against the blocklist. It is used
// as an http.Client CheckRedirect hook, so a 3xx from a server that passed the
// initial check cannot then steer the client at an internal address.
func ValidateNotPrivate(ctx context.Context, u *url.URL) error {
	host := u.Hostname()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if IsPrivate(ip.IP) {
			return fmt.Errorf("blocked: redirect to private/internal address %s (%s)", host, ip.IP)
		}
	}
	return nil
}

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}
