package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
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
	// redirectConfigDir as well as chdirTemp: without it this reads the
	// developer's *user* config, and a machine with `aegis security
	// build-image --global` run against it would send this test through a real
	// image inspect, per-tool cache probes, and (since P55.6) a
	// database-age probe — real podman work inside `go test ./...`.
	redirectConfigDir(t)
	chdirTemp(t) // isolate from any real .aegis/config.yaml
	out, err := runSecurity(t, "", "status")
	if err != nil {
		t.Fatalf("security status: %v", err)
	}
	for _, name := range []string{"opengrep", "trivy", "gitleaks"} {
		if !strings.Contains(out, name) {
			t.Errorf("status output missing %q: %s", name, out)
		}
	}
	// With no multiscanner configured there is no shared cache to report on,
	// and nothing about the container path to advise: P55.4's fallback
	// advisory and P55.6's database table must both stay silent rather than
	// nagging a host-only install on every invocation.
	for _, unwanted := range []string{"Vulnerability databases", "running from host binaries"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("status output contains %q with no multiscanner configured:\n%s", unwanted, out)
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

// TestSecurityBaselineNoFile is the P11.8 no-baseline case: most projects
// have no accepted-risk allowlist, and the command should say so plainly
// rather than error.
func TestSecurityBaselineNoFile(t *testing.T) {
	chdirTemp(t)
	out, err := runSecurity(t, "", "baseline")
	if err != nil {
		t.Fatalf("security baseline: %v", err)
	}
	if !strings.Contains(out, "no baseline entries") {
		t.Errorf("expected a no-entries message, got %q", out)
	}
}

// TestSecurityBaselineShowsEntryStatus is the P11.8 status-display
// regression: active/expired/invalid entries should each render with the
// right label.
func TestSecurityBaselineShowsEntryStatus(t *testing.T) {
	dir := chdirTemp(t)
	baselineDir := filepath.Join(dir, ".aegis")
	if err := os.MkdirAll(baselineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
suppressions:
  - rule_id: "CVE-2099-0001"
    reason: "active accepted risk"
    expires: "2099-01-01"
  - rule_id: "CVE-2000-0001"
    reason: "long past its review date"
    expires: "2000-01-01"
  - rule_id: "CVE-2024-0001"
    reason: "missing expires"
`
	if err := os.WriteFile(filepath.Join(baselineDir, "security-baseline.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSecurity(t, "", "baseline")
	if err != nil {
		t.Fatalf("security baseline: %v", err)
	}
	for _, want := range []string{"active", "CVE-2099-0001", "expired", "CVE-2000-0001", "invalid", "CVE-2024-0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

// fakeBuildResult stands in for a real `build-image` run: everything the pin
// records, with none of the multi-gigabyte container build needed to produce
// it.
func fakeBuildResult(imageID string) security.MultiscannerBuildResult {
	return security.MultiscannerBuildResult{
		Runtime:           sandbox.RuntimePodman,
		Image:             "localhost/aegis-multiscanner:v1",
		ImageID:           imageID,
		Profile:           security.MultiscannerProfileCore,
		Tools:             []string{"trivy", "gitleaks"},
		SourceFingerprint: "sha256:fingerprint-abc",
	}
}

// TestBuildImagePinTarget is the P55.5 default flip: the pin belongs in the
// user config, because the image and the shared database volume it points at
// are machine-wide. --project is the opt-in for a repo that wants its own
// image. Whichever file is chosen, the file *not* chosen must be left alone —
// the old behavior's real damage was a pin nobody could find.
func TestBuildImagePinTarget(t *testing.T) {
	cases := []struct {
		name    string
		project bool
	}{
		{name: "default targets the user config"},
		{name: "--project targets the project config", project: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redirectConfigDir(t)
			chdirTemp(t)

			ms, target, err := recordMultiscannerPin(fakeBuildResult("sha256:deadbeef"), tc.project)
			if err != nil {
				t.Fatalf("recordMultiscannerPin: %v", err)
			}
			wantTarget, other := config.GlobalConfigPath(), config.ProjectConfigPath()
			if tc.project {
				wantTarget, other = other, wantTarget
			}
			if target != wantTarget {
				t.Errorf("wrote to %s, want %s", target, wantTarget)
			}
			if _, err := os.Stat(other); !os.IsNotExist(err) {
				t.Errorf("%s should not have been touched (stat err = %v)", other, err)
			}

			// Round-trip through the file that was actually written, not
			// through the merged config: the merge would pass even if the pin
			// landed in the wrong layer, which is the bug being fixed.
			sec, err := config.FileSecurity(target)
			if err != nil {
				t.Fatalf("FileSecurity(%s): %v", target, err)
			}
			got := sec.Multiscanner
			if !got.Enabled || got.ImageID != "sha256:deadbeef" || got.Image != ms.Image {
				t.Errorf("multiscanner block round-tripped as %+v", got)
			}
			// P55.1's fingerprint and P55.3's runtime have to survive the flip:
			// drift detection and verification both read them back from
			// whichever file the pin was written to.
			if got.SourceFingerprint != "sha256:fingerprint-abc" {
				t.Errorf("source_fingerprint = %q, want it recorded alongside the image ID", got.SourceFingerprint)
			}
			if got.Runtime != string(sandbox.RuntimePodman) {
				t.Errorf("runtime = %q, want podman", got.Runtime)
			}
		})
	}
}

// TestBuildImageRejectsBothTargets: --global is a deprecated no-op for the new
// default, so asking for both it and --project is a contradiction, not a
// precedence question. It must fail before the build, since failing after one
// would cost an operator the whole multi-gigabyte download.
func TestBuildImageRejectsBothTargets(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	_, err := runSecurity(t, "", "build-image", "--project", "--global")
	if err == nil {
		t.Fatal("expected --project --global to be rejected")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want a mutually-exclusive message", err)
	}
	// The check must short-circuit the build, so neither config file exists.
	for _, path := range []string{config.GlobalConfigPath(), config.ProjectConfigPath()} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("%s was written despite the flag conflict (stat err = %v)", path, statErr)
		}
	}
}

// TestBuildImageWarnsAboutProjectShadow is the upgrade case that matters most:
// an operator who ran the pre-P55.5 command in a repo has a project-level pin
// that overrides the machine-wide one written today. Silence there reads as a
// working global pin while every scan in that repo uses the old image.
func TestBuildImageWarnsAboutProjectShadow(t *testing.T) {
	cases := []struct {
		name        string
		projectPin  string
		justBuilt   string
		wantWarning bool
		wantPhrase  string
	}{
		{
			name:      "no project config at all",
			justBuilt: "sha256:new",
		},
		{
			name:        "stale project pin shadows the global one",
			projectPin:  "sha256:old",
			justBuilt:   "sha256:new",
			wantWarning: true,
			wantPhrase:  "not the one just written",
		},
		{
			name:        "project pin names the same image built today",
			projectPin:  "sha256:new",
			justBuilt:   "sha256:new",
			wantWarning: true,
			wantPhrase:  "the same image just built",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redirectConfigDir(t)
			chdirTemp(t)
			if tc.projectPin != "" {
				if err := config.PatchProjectSecurity(config.SecurityPatch{
					Multiscanner: config.MultiscannerConfig{
						Enabled: true,
						Image:   "localhost/aegis-multiscanner:v1",
						ImageID: tc.projectPin,
					},
				}); err != nil {
					t.Fatal(err)
				}
			}
			warn, err := multiscannerShadowWarning(tc.justBuilt)
			if err != nil {
				t.Fatalf("multiscannerShadowWarning: %v", err)
			}
			if !tc.wantWarning {
				if warn != "" {
					t.Fatalf("unexpected warning: %s", warn)
				}
				return
			}
			if warn == "" {
				t.Fatal("expected a shadowing warning")
			}
			for _, want := range []string{config.ProjectConfigPath(), tc.projectPin, tc.wantPhrase, "--project"} {
				if !strings.Contains(warn, want) {
					t.Errorf("warning missing %q: %s", want, warn)
				}
			}
		})
	}
}

// TestBuildImagePinDoesNotLeakProjectPolicyGlobally guards the side effect the
// default flip creates: the pin rewrites the target file's whole security:
// block, so building from inside a repo with project-scoped scanner policy
// must not promote that policy to every project on the machine.
func TestBuildImagePinDoesNotLeakProjectPolicyGlobally(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	if err := config.PatchProjectSecurity(config.SecurityPatch{
		WSLDistro: "kali-linux",
		Tools:     map[string]config.SecurityToolConfig{"nmap": {Method: "wsl"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := recordMultiscannerPin(fakeBuildResult("sha256:deadbeef"), false); err != nil {
		t.Fatalf("recordMultiscannerPin: %v", err)
	}
	sec, err := config.FileSecurity(config.GlobalConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if sec.WSLDistro != "" || len(sec.Tools) != 0 {
		t.Errorf("project-scoped policy leaked into the user config: wsl_distro=%q tools=%v", sec.WSLDistro, sec.Tools)
	}
	// ...and the project's own settings are still where the operator put them.
	proj, err := config.FileSecurity(config.ProjectConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if proj.WSLDistro != "kali-linux" {
		t.Errorf("project wsl_distro = %q, want it untouched", proj.WSLDistro)
	}
}

// TestSecurityVerifyImageWithoutAnImage checks the preflight message rather
// than the scan: there is nothing to verify, which must read differently from
// "your scanners are broken".
//
// Deliberately calls runVerifyImage with an explicit config value instead of
// driving the cobra command, which would config.Load() the developer's real
// user config — and on a machine that has actually built the image, that would
// turn a unit test into a dozen container runs.
func TestSecurityVerifyImageWithoutAnImage(t *testing.T) {
	cmd := newSecurityVerifyImageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runVerifyImage(cmd, config.MultiscannerConfig{}, nil)
	if err == nil {
		t.Fatal("verify-image with no configured image should fail, not report an empty all-clear")
	}
	if !strings.Contains(err.Error(), "build-image") {
		t.Errorf("error should point at `aegis security build-image`, got %q", err)
	}
}

// TestSecurityVerifyImageCommandIsRegistered guards the wiring: the whole
// point is a command an operator (or a provisioning script) can run.
func TestSecurityVerifyImageCommandIsRegistered(t *testing.T) {
	var found bool
	for _, sub := range newSecurityCmd().Commands() {
		if sub.Name() == "verify-image" {
			found = true
		}
	}
	if !found {
		t.Error("`aegis security verify-image` is not registered")
	}
}
