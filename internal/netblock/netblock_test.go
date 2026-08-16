package netblock

import (
	"net"
	"testing"
)

// TestIsPrivateBlocksNewlyCoveredRanges is the VULN-03 regression. Every
// address here was reachable through web_fetch and the HTTP/SSE MCP client
// before P66.10: 0.0.0.0 is not inside 127.0.0.0/8 and net.IP.IsLoopback is
// false for it, yet a connection to it lands on the local host — so
// http://0.0.0.0:11434 reached the operator's Ollama and http://0.0.0.0:4127
// reached the Aegis daemon itself.
func TestIsPrivateBlocksNewlyCoveredRanges(t *testing.T) {
	for _, tc := range []struct{ ip, why string }{
		{"0.0.0.0", "IPv4 unspecified — routes to the local host"},
		{"0.0.0.1", "0.0.0.0/8 'this network'"},
		{"0.255.255.255", "0.0.0.0/8 upper edge"},
		{"::", "IPv6 unspecified"},
		{"100.64.0.1", "CGNAT / tailnet"},
		{"100.127.255.255", "CGNAT upper edge"},
		{"192.0.0.1", "IETF protocol assignments"},
		{"198.18.0.1", "benchmarking range"},
		{"ff01::1", "interface-local multicast"},
	} {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("net.ParseIP(%s) failed", tc.ip)
		}
		if !IsPrivate(ip) {
			t.Errorf("IsPrivate(%s) = false, want true (%s)", tc.ip, tc.why)
		}
	}
}

// TestIsPrivateStillBlocksTheOriginalRanges keeps the pre-P66.10 blocklist
// pinned after the move out of web.go/http.go, including the IPv4-mapped form
// the resolver can hand back.
func TestIsPrivateStillBlocksTheOriginalRanges(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1",
	} {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("net.ParseIP(%s) failed", s)
		}
		if !IsPrivate(ip) {
			t.Errorf("IsPrivate(%s) = false, want true", s)
		}
	}
	if !IsPrivate(nil) {
		t.Error("IsPrivate(nil) = false, want true — an unparseable address is not dialable")
	}
}

// TestIsPrivateLeavesPublicAddressesAlone is the guard against over-blocking,
// and specifically against re-adding ::ffff:0:0/96 to the table: Contains
// reduces that CIDR to 0.0.0.0/0.0.0.0, which would match every IPv4 address
// and silently take web_fetch offline for the whole internet.
func TestIsPrivateLeavesPublicAddressesAlone(t *testing.T) {
	for _, s := range []string{
		"8.8.8.8", "1.1.1.1", "99.255.255.255", "100.128.0.1",
		"192.0.1.1", "198.20.0.1", "2606:4700:4700::1111",
	} {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("net.ParseIP(%s) failed", s)
		}
		if IsPrivate(ip) {
			t.Errorf("IsPrivate(%s) = true, want false", s)
		}
	}
}
