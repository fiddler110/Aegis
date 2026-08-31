package security

import (
	"strings"
	"testing"
)

// TestIsHostAllowedDefaultsToLoopbackOnly is the P13.5.2 generalized gate's
// default policy as narrowed by P81.29 — the same default isDASTTargetAllowed
// tests at the URL level, exercised here directly against bare hosts/IPs (as
// recon_scan's targets are, with no http(s):// scheme). The zero-config path
// covers this machine and stops there.
func TestIsHostAllowedDefaultsToLoopbackOnly(t *testing.T) {
	cases := []string{
		"localhost",
		"LocalHost",
		"127.0.0.1",
		"127.0.0.53",
		"::1",
	}
	for _, host := range cases {
		allowed, reason := isHostAllowed(host, nil)
		if !allowed {
			t.Errorf("isHostAllowed(%q, nil) = false, %q; want allowed by default", host, reason)
		}
	}
}

// TestIsHostAllowedRefusesUndeclaredPrivateHosts is the P81.29 (FIND-29)
// regression. `if isLoopbackOrPrivateHost(host) { return true, "" }` used to
// run above the allowlist loop, so every RFC-1918 and link-local address was
// authorized with no configuration at all — a model-issued recon_scan could
// sweep a corporate LAN. Ordering is the control; this test is what holds it.
func TestIsHostAllowedRefusesUndeclaredPrivateHosts(t *testing.T) {
	cases := []string{
		"10.20.30.40",
		"192.168.1.50",
		"172.16.5.5",
		"169.254.169.254", // link-local, incl. the cloud metadata address
		"fc00::1",
		"fe80::1",
	}
	for _, host := range cases {
		allowed, reason := isHostAllowed(host, nil)
		if allowed {
			t.Errorf("isHostAllowed(%q, nil) = true; want refused without an allowlist entry", host)
		}
		if !strings.Contains(reason, "not declared") {
			t.Errorf("reason for %q = %q, want it to point at the allowlist", host, reason)
		}
	}
}

// TestIsHostAllowedAllowsDeclaredPrivateHosts is the other half: the home-lab
// case P81.29 narrowed still works, it just has to be written down. Both entry
// shapes an operator would reach for — a bare address and a CIDR — count.
func TestIsHostAllowedAllowsDeclaredPrivateHosts(t *testing.T) {
	cases := map[string][]string{
		"10.20.30.40":  {"10.20.30.40"},
		"192.168.1.50": {"192.168.0.0/16"},
		"172.16.5.5":   {"172.16.0.0/12"},
	}
	for host, allowlist := range cases {
		if allowed, reason := isHostAllowed(host, allowlist); !allowed {
			t.Errorf("isHostAllowed(%q, %v) = false, %q; want allowed once declared", host, allowlist, reason)
		}
	}
}

// TestIsHostAllowedNeverResolvesDNS pins the property P81.29 had to preserve
// while reordering the checks: a hostname is matched as a literal string and
// never resolved, so a declared target's identity cannot change under the
// check. "localhost.example.com" resolving to 127.0.0.1 must not make it the
// local machine, and a name that resolves into a declared CIDR is still not
// that CIDR.
func TestIsHostAllowedNeverResolvesDNS(t *testing.T) {
	cases := []struct {
		host      string
		allowlist []string
	}{
		{"localhost.example.com", nil},
		{"127.0.0.1.nip.io", nil},
		{"router.lan", []string{"192.168.0.0/16"}},
	}
	for _, tc := range cases {
		if allowed, _ := isHostAllowed(tc.host, tc.allowlist); allowed {
			t.Errorf("isHostAllowed(%q, %v) = true; a hostname must match literally, never by resolution", tc.host, tc.allowlist)
		}
	}
}

func TestIsHostAllowedRejectsUndeclaredPublicHost(t *testing.T) {
	allowed, reason := isHostAllowed("example.com", nil)
	if allowed {
		t.Fatal("expected an undeclared public host to be rejected")
	}
	if reason == "" {
		t.Error("expected a non-empty rejection reason")
	}
}

func TestIsHostAllowedHonorsExplicitDeclarations(t *testing.T) {
	allowlist := []string{"example.com", ".staging.internal", "203.0.113.0/24"}

	cases := map[string]bool{
		"example.com":               true,
		"api.staging.internal":      true,
		"deep.api.staging.internal": true,
		"203.0.113.42":              true,
		"evil.com":                  false,
		"notstaging.internal":       false,
	}
	for host, want := range cases {
		allowed, reason := isHostAllowed(host, allowlist)
		if allowed != want {
			t.Errorf("isHostAllowed(%q) = %v (%q), want %v", host, allowed, reason, want)
		}
	}
}

func TestIsHostAllowedRejectsEmptyHost(t *testing.T) {
	if allowed, _ := isHostAllowed("", []string{"example.com"}); allowed {
		t.Error("expected an empty host to be rejected")
	}
}
