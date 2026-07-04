package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runSecurity(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newSecurityCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if stdin != "" {
		cmd.SetIn(strings.NewReader(stdin))
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// TestSecurityStatusListsBuiltinScanners is a smoke test for the P11.1
// status surface: every built-in scanner descriptor should appear, and none
// should be silently missing from the report the way an unresolved binary
// used to just vanish from Available()-gated output.
func TestSecurityStatusListsBuiltinScanners(t *testing.T) {
	chdirTemp(t) // isolate from any real .aegis/config.yaml
	out, err := runSecurity(t, "", "status")
	if err != nil {
		t.Fatalf("security status: %v", err)
	}
	for _, name := range []string{"semgrep", "trivy", "gitleaks"} {
		if !strings.Contains(out, name) {
			t.Errorf("status output missing %q: %s", name, out)
		}
	}
}

func TestSecurityConfigShowsDefaultMethod(t *testing.T) {
	chdirTemp(t)
	out, err := runSecurity(t, "", "config")
	if err != nil {
		t.Fatalf("security config: %v", err)
	}
	if !strings.Contains(out, "default_method: auto") {
		t.Errorf("expected default_method: auto, got %q", out)
	}
}

func TestSecurityInstallUnknownTool(t *testing.T) {
	chdirTemp(t)
	_, err := runSecurity(t, "", "install", "not-a-real-scanner")
	if err == nil {
		t.Fatal("expected an error for an unknown scanner name")
	}
	if !strings.Contains(err.Error(), "unknown scanner") {
		t.Errorf("error = %v, want mention of unknown scanner", err)
	}
}

// TestSecurityInstallAbortsWithoutConfirmation is the P11.10 approval-gate
// regression: declining the prompt must not run the install command.
func TestSecurityInstallAbortsWithoutConfirmation(t *testing.T) {
	chdirTemp(t)
	out, err := runSecurity(t, "n\n", "install", "gitleaks")
	if err != nil {
		t.Fatalf("security install: %v", err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Errorf("expected an abort message when declining, got %q", out)
	}
	if !strings.Contains(out, "This will run the following command") {
		t.Errorf("expected the exact command to be shown before the prompt, got %q", out)
	}
}
