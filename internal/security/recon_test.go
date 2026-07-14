package security

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// TestRunReconRejectsNoTargets and TestRunReconRejectsTooManyTargets are the
// P13.5.3 boundary checks: no silent truncation, a clear error instead.
func TestRunReconRejectsNoTargets(t *testing.T) {
	_, err := RunRecon(context.Background(), ReconOptions{}, Options{})
	if err == nil {
		t.Fatal("expected an error for zero targets")
	}
}

func TestRunReconRejectsTooManyTargets(t *testing.T) {
	targets := make([]string, maxReconTargets+1)
	for i := range targets {
		targets[i] = "127.0.0.1"
	}
	_, err := RunRecon(context.Background(), ReconOptions{Targets: targets}, Options{})
	if err == nil {
		t.Fatal("expected an error for exceeding the target cap")
	}
	if !strings.Contains(err.Error(), "at most") {
		t.Errorf("err = %q, want mention of the cap", err)
	}
}

// TestRunReconRejectsOneDisallowedTargetInList proves P13.5.3's per-host
// enforcement: one bad host in an otherwise-authorized list fails the whole
// call, not a partial silent skip — checked before any scanner runs.
func TestRunReconRejectsOneDisallowedTargetInList(t *testing.T) {
	_, err := RunRecon(context.Background(), ReconOptions{
		Targets:        []string{"127.0.0.1", "evil.example.com"},
		AllowedTargets: nil,
	}, Options{})
	if err == nil {
		t.Fatal("expected an error when any target in the list is disallowed")
	}
	if !strings.Contains(err.Error(), "evil.example.com") {
		t.Errorf("err = %q, want it to name the disallowed target", err)
	}
}

func TestRunReconAcceptsPrivateCIDR(t *testing.T) {
	// A private CIDR's network address should gate-check as allowed without
	// any config — the common "scan my home lab subnet" case.
	host, err := targetHost("192.168.1.0/24")
	if err != nil {
		t.Fatalf("targetHost: %v", err)
	}
	if allowed, reason := isHostAllowed(host, nil); !allowed {
		t.Errorf("expected private CIDR network address to be allowed by default, got reason %q", reason)
	}
}

func TestRunReconRejectsInvalidCIDR(t *testing.T) {
	_, err := RunRecon(context.Background(), ReconOptions{Targets: []string{"10.0.0.0/999"}}, Options{})
	if err == nil {
		t.Fatal("expected an error for an invalid CIDR target")
	}
}

func TestTargetHostParsesHostPortAndBareHost(t *testing.T) {
	cases := map[string]string{
		"db.lan":         "db.lan",
		"db.lan:5432":    "db.lan",
		"192.168.1.5":    "192.168.1.5",
		"192.168.1.5:22": "192.168.1.5",
		"10.0.0.0/24":    "10.0.0.0",
	}
	for target, want := range cases {
		got, err := targetHost(target)
		if err != nil {
			t.Errorf("targetHost(%q) error: %v", target, err)
			continue
		}
		if got != want {
			t.Errorf("targetHost(%q) = %q, want %q", target, got, want)
		}
	}
}

// TestNmapArgsBaselineVsActive proves the P13.5.4 safe-default split: no OS
// detection/full-port-range/scripts unless active is true.
func TestNmapArgsBaselineVsActive(t *testing.T) {
	baseline := nmapArgs([]string{"192.168.1.1"}, false, "/tmp/out.xml")
	for _, forbidden := range []string{"-sC", "-O", "-p-"} {
		if containsArg(baseline, forbidden) {
			t.Errorf("baseline args should not include %q: %v", forbidden, baseline)
		}
	}
	if !containsArg(baseline, "192.168.1.1") {
		t.Errorf("expected target in args: %v", baseline)
	}

	active := nmapArgs([]string{"192.168.1.1"}, true, "/tmp/out.xml")
	for _, want := range []string{"-sC", "-O", "-p-"} {
		if !containsArg(active, want) {
			t.Errorf("active args should include %q: %v", want, active)
		}
	}
}

// TestNucleiArgsBaselineExcludesIntrusiveTags is the P13.5.4 safe-default
// check on the nuclei side.
func TestNucleiArgsBaselineExcludesIntrusiveTags(t *testing.T) {
	baseline := nucleiArgs([]string{"192.168.1.1"}, false, "/templates", "/tmp/out.sarif")
	if !containsArg(baseline, "-etags") {
		t.Errorf("baseline args should exclude dos/fuzz/intrusive tags: %v", baseline)
	}

	active := nucleiArgs([]string{"192.168.1.1"}, true, "/templates", "/tmp/out.sarif")
	if containsArg(active, "-etags") {
		t.Errorf("active args should not exclude any tags: %v", active)
	}
	if !containsArg(active, "-target") || !containsArg(active, "192.168.1.1") {
		t.Errorf("expected target in args: %v", active)
	}
}

func TestResolveNucleiTemplatesRequiresPinnedVersion(t *testing.T) {
	_, err := resolveNucleiTemplates(context.Background(), Options{})
	if err == nil {
		t.Fatal("expected an error when templates_version is unset")
	}
	if !strings.Contains(err.Error(), "templates_version") {
		t.Errorf("err = %q, want mention of templates_version", err)
	}
}

// TestResolveNucleiTemplatesRejectsUnsafeVersion covers P31.1: templates_version
// is used unsanitized in a filepath.Join and as a `git clone --branch` arg, so
// path-traversal and git-flag-injection shaped values must be rejected before
// either use.
func TestResolveNucleiTemplatesRejectsUnsafeVersion(t *testing.T) {
	for _, version := range []string{
		"../../../etc/passwd",
		"..",
		"v1.0.0/../../escape",
		"-oProxyCommand=evil",
		"--upload-pack=evil",
		"v1.0.0 && rm -rf /",
	} {
		_, err := resolveNucleiTemplates(context.Background(), Options{
			Tools: map[string]ToolPolicy{"nuclei": {TemplatesVersion: version}},
		})
		if err == nil {
			t.Errorf("templates_version = %q: expected an error, got nil", version)
		}
	}
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}

// TestParseNmapXMLBuildsFindingsAndAppliesRiskyPortSeverity proves the nmap
// XML ingester turns open ports into Findings, ignores closed/filtered
// ports, and bumps a curated risky-service port above plain INFO.
func TestParseNmapXMLBuildsFindingsAndAppliesRiskyPortSeverity(t *testing.T) {
	xmlData := `<?xml version="1.0"?>
<nmaprun>
  <host>
    <address addr="192.168.1.10" addrtype="ipv4"/>
    <ports>
      <port protocol="tcp" portid="80">
        <state state="open"/>
        <service name="http" product="nginx" version="1.25"/>
      </port>
      <port protocol="tcp" portid="6379">
        <state state="open"/>
        <service name="redis"/>
      </port>
      <port protocol="tcp" portid="9999">
        <state state="closed"/>
        <service name="unknown"/>
      </port>
    </ports>
  </host>
</nmaprun>`
	findings, err := parseNmapXML([]byte(xmlData))
	if err != nil {
		t.Fatalf("parseNmapXML: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2 (closed port must be excluded): %+v", len(findings), findings)
	}

	var httpFinding, redisFinding *Finding
	for i := range findings {
		switch findings[i].Location {
		case "192.168.1.10:80/tcp":
			httpFinding = &findings[i]
		case "192.168.1.10:6379/tcp":
			redisFinding = &findings[i]
		}
	}
	if httpFinding == nil || httpFinding.Severity != SevInfo {
		t.Errorf("http finding = %+v, want SevInfo", httpFinding)
	}
	if redisFinding == nil || redisFinding.Severity != SevHigh {
		t.Errorf("redis finding = %+v, want SevHigh (risky-port bump)", redisFinding)
	}
}
