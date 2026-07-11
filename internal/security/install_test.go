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

func withWSLDistroAvailable(t *testing.T, fn func(ctx context.Context, distro string) bool) {
	t.Helper()
	orig := wslDistroAvailable
	wslDistroAvailable = fn
	t.Cleanup(func() { wslDistroAvailable = orig })
}

// TestInstallCommandWSLFallback is the P14.x regression: a tool with no
// Windows install entry (opengrep/kubescape's actual shape) but a Linux one
// falls back to a `wsl -- bash -lc '<linux cmd>'` invocation when a WSL
// distro is present — only on Windows, and only when WSL is actually there.
func TestInstallCommandWSLFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("WSL fallback only applies on windows")
	}
	withTestDescriptor(t, ScannerDescriptor{
		Name:   "test-wsl-install",
		Binary: "test-wsl-install",
		Install: map[string]string{
			"linux": "curl -fsSL https://example.com/install.sh | bash",
		},
	})

	withWSLDistroAvailable(t, func(context.Context, string) bool { return false })
	if _, ok := InstallCommand("test-wsl-install"); ok {
		t.Error("expected no install command when WSL isn't available")
	}

	withWSLDistroAvailable(t, func(context.Context, string) bool { return true })
	cmd, ok := InstallCommand("test-wsl-install")
	if !ok {
		t.Fatal("expected a WSL-fallback install command")
	}
	want := "wsl -- bash -lc 'curl -fsSL https://example.com/install.sh | bash'"
	if cmd != want {
		t.Errorf("cmd = %q, want %q", cmd, want)
	}
}

// TestInstallCommandNativeWindowsNeverFallsBackToWSL proves a tool that
// already has a native Windows install entry never gets routed through WSL,
// even if a distro is available — WSL is strictly a fallback for tools with
// no native build at all.
func TestInstallCommandNativeWindowsNeverFallsBackToWSL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("this proves windows-native takes priority over the windows-only WSL fallback")
	}
	withTestDescriptor(t, ScannerDescriptor{
		Name:   "test-native-win",
		Binary: "test-native-win",
		Install: map[string]string{
			"windows": "scoop install test-native-win",
			"linux":   "curl -fsSL https://example.com/install.sh | bash",
		},
	})
	withWSLDistroAvailable(t, func(context.Context, string) bool { return true })

	cmd, ok := InstallCommand("test-native-win")
	if !ok || cmd != "scoop install test-native-win" {
		t.Errorf("cmd = %q, ok = %v, want the native windows command unchanged", cmd, ok)
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

// ---------------------------------------------------------------------------
// FIND-31 / P24.8 — verification-only threat-model item: audit install.go's
// installer-script argument construction and confirm no unsanitized string
// concatenation feeds the shell invocation. Audit conclusion (see
// RunGuidedInstall's doc comment): every ScannerDescriptor.Install entry in
// method.go's descriptors map is a fixed compile-time string literal; name
// only ever selects a map key (validated via DescriptorFor, never itself
// spliced into a command string); and shellInvocation hands the resulting
// command to exec.CommandContext as a single, unmodified argv element, never
// re-split or re-parsed by Go. No fix was needed — the two tests below lock
// in the properties the audit relied on, so a future change that breaks
// either (e.g. someone adding fmt.Sprintf-based interpolation to an Install
// entry, or "helpfully" tokenizing command before exec) fails loudly here.
// ---------------------------------------------------------------------------

// TestInstallCommandsHaveNoInterpolationMarkers asserts every descriptor's
// Install command (for every OS) is a plain literal with no leftover
// interpolation markers — Go format verbs or template placeholders — that
// would signal a future edit accidentally splicing a runtime/config/attacker
// value into a string that flows unmodified into a real shell.
func TestInstallCommandsHaveNoInterpolationMarkers(t *testing.T) {
	suspicious := []string{"%s", "%d", "%v", "%q", "%x", "${", "{{", "}}"}
	for _, d := range Descriptors() {
		for osName, cmd := range d.Install {
			for _, marker := range suspicious {
				if strings.Contains(cmd, marker) {
					t.Errorf("descriptor %q install command for %q contains suspicious interpolation marker %q: %q", d.Name, osName, marker, cmd)
				}
			}
		}
	}
}

// TestShellInvocationKeepsCommandAsSingleArgvElement proves shellInvocation
// never tokenizes/re-splits command — it always appears as exactly one,
// byte-for-byte unmodified element of the returned argv slice, regardless of
// embedded shell metacharacters. This is the property that makes "command is
// always a hardcoded literal" sufficient to rule out injection: nothing
// downstream of InstallCommand re-parses the string against attacker/config
// input, so there is no second place a concatenation bug could sneak in.
func TestShellInvocationKeepsCommandAsSingleArgvElement(t *testing.T) {
	const command = `echo hello; echo "word1 word2" && echo $(danger) | echo`
	shell, args := shellInvocation(command)
	if strings.TrimSpace(shell) == "" {
		t.Fatal("shellInvocation returned an empty shell binary")
	}
	if len(args) == 0 {
		t.Fatal("shellInvocation returned no args")
	}
	last := args[len(args)-1]
	if last != command {
		t.Errorf("args[last] = %q, want the command unmodified: %q", last, command)
	}
	count := 0
	for _, a := range args {
		if a == command {
			count++
		}
	}
	if count != 1 {
		t.Errorf("command appeared as %d distinct argv elements, want exactly 1 (proves Go isn't word-splitting it)", count)
	}
}

// TestRunGuidedInstallPassesCommandIntactThroughShell proves a `;`-chained
// command string reaches a real shell whole (both halves run), matching the
// shape every real Install entry uses (curl ... | bash, brew install X &&
// Y). That intact pass-through is safe here only because every Install value
// is a fixed compile-time literal (TestInstallCommandsHaveNoInterpolationMarkers)
// with nothing runtime/attacker-controlled spliced in — if it were, this same
// behavior is exactly what would make injection possible.
func TestRunGuidedInstallPassesCommandIntactThroughShell(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{
		Name:   "test-chained-install",
		Binary: "test-chained-install",
		Install: map[string]string{
			"windows": "Write-Output part1; Write-Output part2",
			"linux":   "echo part1; echo part2",
			"darwin":  "echo part1; echo part2",
		},
	})

	var buf bytes.Buffer
	if err := RunGuidedInstall(context.Background(), "test-chained-install", &buf); err != nil {
		t.Fatalf("RunGuidedInstall: %v (output: %s)", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "part1") || !strings.Contains(out, "part2") {
		t.Errorf("output = %q, want both chained command parts to have run", out)
	}
}
