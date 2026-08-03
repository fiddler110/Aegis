package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSecurityPatchRoundTripsEveryField is the guard for a failure that
// actually happened: SecurityPatch grew a Netscanner field and buildSecurityBlock
// was never taught to write it. `aegis security build-image --netscanner` then
// built the image, reported "pinned in: <file>", exited 0 — and pinned nothing.
// The next command simply behaved as though the image did not exist.
//
// patchSecurity replaces the whole security: block, so *any* field this struct
// carries but the writer forgets is not merely unsaved: it is deleted from the
// operator's config by an unrelated write. Asserting field-by-field would grow a
// hole the same way, so this fills every field with a non-zero value, writes,
// reloads, and compares the whole struct.
func TestSecurityPatchRoundTripsEveryField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	enabled := true
	want := SecurityPatch{
		EgressThenWrite:  true,
		NetworkAllowList: []string{"api.github.com"},
		DefaultMethod:    "container",
		Tools: map[string]SecurityToolConfig{
			"trivy": {Enabled: &enabled, Method: "container", Image: "img@sha256:abc",
				TemplatesVersion: "v1.2.3", Verify: true, Install: "brew install trivy"},
		},
		DAST:      DASTConfig{AllowedTargets: []string{"10.0.0.1"}, AllowActive: true},
		WSLDistro: "kali-linux",
		Debate:    DebateIntegrationConfig{ThreatModel: true, Triage: true},
		Multiscanner: MultiscannerConfig{
			Enabled: true, Image: "localhost/aegis-multiscanner:v1", ImageID: "sha256:aaa",
			SourceFingerprint: "fp-multi", Runtime: "podman", Concurrency: 5,
			Tools: []string{"gitleaks", "gosec"},
		},
		Netscanner: NetscannerConfig{
			Enabled: true, Image: "localhost/aegis-netscanner:v1", ImageID: "sha256:bbb",
			SourceFingerprint: "fp-net", Runtime: "podman",
			Tools: []string{"grype", "nmap", "nuclei", "trivy"},
		},
	}

	if err := patchSecurity(path, want); err != nil {
		t.Fatalf("patchSecurity: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FileSecurity(path)
	if err != nil {
		t.Fatalf("FileSecurity: %v", err)
	}

	if !reflect.DeepEqual(got.Multiscanner, want.Multiscanner) {
		t.Errorf("multiscanner did not round-trip:\n got %+v\nwant %+v\n\nfile:\n%s", got.Multiscanner, want.Multiscanner, raw)
	}
	if !reflect.DeepEqual(got.Netscanner, want.Netscanner) {
		t.Errorf("netscanner did not round-trip — a pin the operator was told was written:\n got %+v\nwant %+v\n\nfile:\n%s", got.Netscanner, want.Netscanner, raw)
	}
	if !reflect.DeepEqual(got.DAST, want.DAST) {
		t.Errorf("dast did not round-trip:\n got %+v\nwant %+v", got.DAST, want.DAST)
	}
	if !reflect.DeepEqual(got.Debate, want.Debate) {
		t.Errorf("debate did not round-trip:\n got %+v\nwant %+v", got.Debate, want.Debate)
	}
	if got.WSLDistro != want.WSLDistro || got.DefaultMethod != want.DefaultMethod ||
		got.EgressThenWrite != want.EgressThenWrite {
		t.Errorf("scalar security fields did not round-trip: %+v", got)
	}
	if !reflect.DeepEqual(got.NetworkAllowList, want.NetworkAllowList) {
		t.Errorf("network_allowlist did not round-trip: %v", got.NetworkAllowList)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tools did not round-trip: %+v", got.Tools)
	}
	gt, wt := got.Tools["trivy"], want.Tools["trivy"]
	if gt.Method != wt.Method || gt.Image != wt.Image || gt.TemplatesVersion != wt.TemplatesVersion ||
		gt.Verify != wt.Verify || gt.Install != wt.Install || !gt.ToolEnabled() {
		t.Errorf("security.tools.trivy did not round-trip:\n got %+v\nwant %+v", gt, wt)
	}
}

// TestSecurityPatchWriterCoversEveryField fails when a field is added to
// SecurityPatch without the round-trip test above being extended to cover it —
// the mechanism by which Netscanner slipped through in the first place.
func TestSecurityPatchWriterCoversEveryField(t *testing.T) {
	// Every field name this test knows is asserted by
	// TestSecurityPatchRoundTripsEveryField. Adding a field here without adding
	// it there is the mistake; the list is the checklist.
	covered := map[string]bool{
		"EgressThenWrite": true, "NetworkAllowList": true, "DefaultMethod": true,
		"Tools": true, "DAST": true, "WSLDistro": true, "Debate": true,
		"Multiscanner": true, "Netscanner": true,
	}
	tp := reflect.TypeOf(SecurityPatch{})
	for i := range tp.NumField() {
		name := tp.Field(i).Name
		if !covered[name] {
			t.Errorf("SecurityPatch.%s is not covered by TestSecurityPatchRoundTripsEveryField — "+
				"patchSecurity rewrites the whole security: block, so an unwritten field is silently "+
				"deleted from the operator's config on the next write. Add it to buildSecurityBlock, "+
				"to that test, and to this list.", name)
		}
	}
}
