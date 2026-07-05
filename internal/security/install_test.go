package security

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestInstallCommandKnownTool(t *testing.T) {
	// semgrep ships an install command for every supported OS (unlike
	// opengrep, whose curl|bash installer has no Windows equivalent).
	cmd, ok := InstallCommand("semgrep")
	if !ok || strings.TrimSpace(cmd) == "" {
		t.Fatalf("InstallCommand(semgrep) = (%q, %v), want a non-empty command", cmd, ok)
	}
}

func TestInstallCommandUnknownTool(t *testing.T) {
	if _, ok := InstallCommand("not-a-real-scanner"); ok {
		t.Error("InstallCommand for an unknown scanner should return ok=false")
	}
}

func TestRunGuidedInstallUnknownToolErrors(t *testing.T) {
	var buf bytes.Buffer
	err := RunGuidedInstall(context.Background(), "not-a-real-scanner", &buf)
	if err == nil {
		t.Fatal("expected an error for an unknown scanner")
	}
	if !strings.Contains(err.Error(), "no guided install available") {
		t.Errorf("err = %q, want it to explain no install is available", err.Error())
	}
}

// otherGOOS returns the two supported OS names that are not the one running
// this test, so descriptor fixtures stay valid regardless of which platform
// CI happens to run on.
func otherGOOS() []string {
	out := make([]string, 0, 2)
	for _, os := range []string{"windows", "darwin", "linux"} {
		if os != runtime.GOOS {
			out = append(out, os)
		}
	}
	return out
}

// TestInstallAvailabilityCurrentOSSupported proves no note-worthy gap is
// reported when the current OS has its own guided install command.
func TestInstallAvailabilityCurrentOSSupported(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{
		Name:    "test-avail-current",
		Binary:  "test-avail-current",
		Install: map[string]string{runtime.GOOS: "echo hello"},
	})

	av := InstallAvailability("test-avail-current")
	if !av.CurrentOSSupported {
		t.Error("CurrentOSSupported = false, want true")
	}
	if got := AvailabilityNote("test-avail-current", "test-avail-current not installed on PATH"); got != "" {
		t.Errorf("AvailabilityNote = %q, want \"\" (current OS already supported)", got)
	}
}

// TestInstallAvailabilityOtherOSOnly is the P13.1 case: a tool with a guided
// install for other OSes but not this one should surface which OSes do have
// one, and point at the container-image config as the actionable next step.
func TestInstallAvailabilityOtherOSOnly(t *testing.T) {
	others := otherGOOS()
	install := map[string]string{}
	for _, os := range others {
		install[os] = "echo hello"
	}
	withTestDescriptor(t, ScannerDescriptor{
		Name:    "test-avail-other",
		Binary:  "test-avail-other",
		Install: install,
	})

	av := InstallAvailability("test-avail-other")
	if av.CurrentOSSupported {
		t.Error("CurrentOSSupported = true, want false")
	}
	if !av.HasAnyHostInstall {
		t.Error("HasAnyHostInstall = false, want true")
	}
	if strings.Join(av.OtherOSes, ",") != strings.Join(others, ",") {
		t.Errorf("OtherOSes = %v, want %v", av.OtherOSes, others)
	}

	note := AvailabilityNote("test-avail-other", "test-avail-other not installed on PATH (some detail)")
	if note == "" {
		t.Fatal("AvailabilityNote = \"\", want a non-empty note")
	}
	for _, os := range others {
		if !strings.Contains(note, os) {
			t.Errorf("note = %q, want it to mention %q", note, os)
		}
	}
	if !strings.Contains(note, "security.tools.test-avail-other.image") {
		t.Errorf("note = %q, want it to point at the container-image config", note)
	}
}

// TestAvailabilityNoteIgnoresUnrelatedReasons proves the note only fires for
// a "not installed" host-binary reason, not for disabled/opt-in/container
// reasons where cross-platform install info would be misleading noise.
func TestAvailabilityNoteIgnoresUnrelatedReasons(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{
		Name:    "test-avail-disabled",
		Binary:  "test-avail-disabled",
		Install: map[string]string{"linux": "echo hello"}, // no entry for runtime.GOOS (unless GOOS==linux)
	})

	for _, reason := range []string{
		"disabled by configuration (security.tools.test-avail-disabled.enabled: false)",
		"opt-in tool, not enabled by default — set security.tools.test-avail-disabled.enabled: true",
		"no container image configured for test-avail-disabled; set security.tools.test-avail-disabled.image",
	} {
		if got := AvailabilityNote("test-avail-disabled", reason); got != "" {
			t.Errorf("AvailabilityNote(%q) = %q, want \"\" (not a missing-host-binary reason)", reason, got)
		}
	}
}

// TestInstallAvailabilityNoHostInstallAtAll proves a container-only tool
// (like zap, with an empty Install map) never gets a note — there's no
// "other OS" to point to.
func TestInstallAvailabilityNoHostInstallAtAll(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-avail-none", Binary: ""})

	av := InstallAvailability("test-avail-none")
	if av.HasAnyHostInstall {
		t.Error("HasAnyHostInstall = true, want false for an empty Install map")
	}
	if got := AvailabilityNote("test-avail-none", "not installed on PATH"); got != "" {
		t.Errorf("AvailabilityNote = %q, want \"\" (no host install exists on any OS)", got)
	}
}

func TestRunGuidedInstallRunsCommand(t *testing.T) {
	// A fake descriptor whose "install" command is a trivial, portable
	// shell/PowerShell one-liner so this test never touches the network or a
	// real package manager.
	withTestDescriptor(t, ScannerDescriptor{
		Name:   "test-echo-install",
		Binary: "test-echo-install",
		Install: map[string]string{
			"windows": "Write-Output hello",
			"linux":   "echo hello",
			"darwin":  "echo hello",
		},
	})

	var buf bytes.Buffer
	if err := RunGuidedInstall(context.Background(), "test-echo-install", &buf); err != nil {
		t.Fatalf("RunGuidedInstall: %v (output: %s)", err, buf.String())
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("output = %q, want it to contain the command's output", buf.String())
	}
}
