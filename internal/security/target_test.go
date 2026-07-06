package security

import "testing"

// TestIsHostAllowedDefaultsToLoopbackAndPrivate is the P13.5.2 generalized
// gate's default policy — the same default isDASTTargetAllowed already
// tests at the URL level, exercised here directly against bare hosts/IPs
// (as recon_scan's targets are, with no http(s):// scheme).
func TestIsHostAllowedDefaultsToLoopbackAndPrivate(t *testing.T) {
	cases := []string{
		"localhost",
		"127.0.0.1",
		"10.20.30.40",
		"192.168.1.50",
		"172.16.5.5",
		"::1",
	}
	for _, host := range cases {
		allowed, reason := isHostAllowed(host, nil)
		if !allowed {
			t.Errorf("isHostAllowed(%q, nil) = false, %q; want allowed by default", host, reason)
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
