package security

import (
	"context"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// TestIsDASTTargetAllowedDefaultsToLoopbackAndPrivate is the P11.7 default
// policy: the common "scan my locally running app" case needs no config.
func TestIsDASTTargetAllowedDefaultsToLoopbackAndPrivate(t *testing.T) {
	cases := []string{
		"http://localhost:8080",
		"http://127.0.0.1:3000",
		"https://10.20.30.40",
		"http://192.168.1.50",
		"http://172.16.5.5",
		"http://[::1]:9000",
	}
	for _, target := range cases {
		allowed, reason := isDASTTargetAllowed(target, nil)
		if !allowed {
			t.Errorf("isDASTTargetAllowed(%q, nil) = false, %q; want allowed by default", target, reason)
		}
	}
}

// TestIsDASTTargetAllowedRejectsUndeclaredPublicHost is the hard-gate
// regression: a public/arbitrary host must be rejected unless explicitly
// declared, regardless of permission mode (this check runs unconditionally
// upstream of any mode/approval gate).
func TestIsDASTTargetAllowedRejectsUndeclaredPublicHost(t *testing.T) {
	allowed, reason := isDASTTargetAllowed("https://example.com", nil)
	if allowed {
		t.Fatal("expected an undeclared public host to be rejected")
	}
	if !strings.Contains(reason, "not declared") {
		t.Errorf("reason = %q, want mention of not declared", reason)
	}
}

func TestIsDASTTargetAllowedHonorsExplicitDeclarations(t *testing.T) {
	allowlist := []string{"example.com", ".staging.internal", "203.0.113.0/24"}

	cases := map[string]bool{
		"https://example.com":               true,
		"https://api.staging.internal":      true,
		"https://deep.api.staging.internal": true,
		"https://203.0.113.42":              true,
		"https://evil.com":                  false,
		"https://notstaging.internal":       false,
	}
	for target, want := range cases {
		allowed, reason := isDASTTargetAllowed(target, allowlist)
		if allowed != want {
			t.Errorf("isDASTTargetAllowed(%q) = %v (%q), want %v", target, allowed, reason, want)
		}
	}
}

func TestIsDASTTargetAllowedRejectsInvalidURL(t *testing.T) {
	for _, target := range []string{"", "not-a-url", "ftp://example.com", "javascript:alert(1)"} {
		if allowed, _ := isDASTTargetAllowed(target, []string{"example.com"}); allowed {
			t.Errorf("expected %q to be rejected as an invalid target", target)
		}
	}
}

// TestBuildZAPPlanModes proves each mode produces the right job sequence and
// that every plan routes through the shared SARIF report job.
func TestBuildZAPPlanModes(t *testing.T) {
	baseline, err := buildZAPPlan(DASTModeBaseline, "http://localhost:8080", "")
	if err != nil {
		t.Fatal(err)
	}
	var parsed zapPlan
	if err := yaml.Unmarshal(baseline, &parsed); err != nil {
		t.Fatalf("baseline plan is not valid YAML: %v", err)
	}
	assertJobTypes(t, parsed, []string{"spider", "passiveScan-wait", "report"})
	if parsed.Env.Contexts[0].URLs[0] != "http://localhost:8080" {
		t.Errorf("target not set in context: %+v", parsed.Env.Contexts[0])
	}
	assertReportIsSARIF(t, parsed)

	active, err := buildZAPPlan(DASTModeActive, "http://localhost:8080", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(active, &parsed); err != nil {
		t.Fatalf("active plan is not valid YAML: %v", err)
	}
	assertJobTypes(t, parsed, []string{"spider", "passiveScan-wait", "activeScan", "report"})

	api, err := buildZAPPlan(DASTModeAPI, "http://localhost:8080", "http://localhost:8080/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(api, &parsed); err != nil {
		t.Fatalf("api plan is not valid YAML: %v", err)
	}
	assertJobTypes(t, parsed, []string{"openapi", "activeScan", "report"})
}

func TestBuildZAPPlanAPIModeRequiresDefinition(t *testing.T) {
	if _, err := buildZAPPlan(DASTModeAPI, "http://localhost:8080", ""); err == nil {
		t.Fatal("expected an error when api mode has no api_definition")
	}
}

func TestBuildZAPPlanRejectsUnknownMode(t *testing.T) {
	if _, err := buildZAPPlan(DASTMode("bogus"), "http://localhost:8080", ""); err == nil {
		t.Fatal("expected an error for an unrecognized mode")
	}
}

func assertJobTypes(t *testing.T, plan zapPlan, want []string) {
	t.Helper()
	if len(plan.Jobs) != len(want) {
		t.Fatalf("got %d jobs, want %d (%v)", len(plan.Jobs), len(want), want)
	}
	for i, w := range want {
		if plan.Jobs[i].Type != w {
			t.Errorf("job[%d].Type = %q, want %q", i, plan.Jobs[i].Type, w)
		}
	}
}

func assertReportIsSARIF(t *testing.T, plan zapPlan) {
	t.Helper()
	last := plan.Jobs[len(plan.Jobs)-1]
	if last.Type != "report" {
		t.Fatalf("last job = %q, want report", last.Type)
	}
	if last.Parameters["template"] != "sarif-json" {
		t.Errorf("report template = %v, want sarif-json", last.Parameters["template"])
	}
}

// TestRunDASTRejectsDisallowedTargetBeforeTouchingZAP proves the target
// check runs before any scanner resolution/container work — no docker/zap
// binary needs to exist for this rejection to happen deterministically.
func TestRunDASTRejectsDisallowedTargetBeforeTouchingZAP(t *testing.T) {
	_, err := RunDAST(context.Background(), DASTOptions{Target: "https://evil.example.com"}, Options{})
	if err == nil {
		t.Fatal("expected an error for a disallowed target")
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Errorf("err = %q, want mention of not declared", err)
	}
}

// TestRunDASTRejectsActiveModeWithoutAllowActive is the P11.7 double-gate
// regression: active/api modes need security.dast.allow_active: true even
// against an otherwise-allowed (loopback) target.
func TestRunDASTRejectsActiveModeWithoutAllowActive(t *testing.T) {
	_, err := RunDAST(context.Background(), DASTOptions{
		Target: "http://localhost:8080",
		Mode:   DASTModeActive,
	}, Options{})
	if err == nil {
		t.Fatal("expected an error when active mode is used without allow_active")
	}
	if !strings.Contains(err.Error(), "allow_active") {
		t.Errorf("err = %q, want mention of allow_active", err)
	}
}
