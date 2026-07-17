package security

import (
	"context"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// withTestDescriptor registers a scanner descriptor under a name guaranteed
// not to collide with a real scanner, restoring the map afterward. Using a
// synthetic name (never "opengrep"/"trivy"/"gitleaks") keeps these tests
// deterministic regardless of what happens to be installed on the machine
// running them.
func withTestDescriptor(t *testing.T, d ScannerDescriptor) {
	t.Helper()
	name := d.Name
	descriptors[name] = d
	t.Cleanup(func() { delete(descriptors, name) })
}

func withDetectRuntime(t *testing.T, fn func(ctx context.Context, priority []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool)) {
	t.Helper()
	orig := detectRuntime
	detectRuntime = fn
	t.Cleanup(func() { detectRuntime = orig })
}

func withWSLBinaryAvailable(t *testing.T, fn func(ctx context.Context, bin, distro string) bool) {
	t.Helper()
	orig := wslBinaryAvailable
	wslBinaryAvailable = fn
	t.Cleanup(func() { wslBinaryAvailable = orig })
}

// withHostGOOS pins the OS the platform-specific resolution rules see, so the
// HostBroken tests below assert the rule on every machine instead of only on
// the one platform that happens to trigger it (P34.7's lesson).
func withHostGOOS(t *testing.T, goos string) {
	t.Helper()
	orig := hostGOOS
	hostGOOS = goos
	t.Cleanup(func() { hostGOOS = orig })
}

// TestResolveOptInToolDisabledByDefault is the P11.3 regression: a tool
// descriptor with DefaultEnabled: false must resolve to MethodNone with a
// distinct "opt-in" reason (not "disabled by configuration") when the
// operator hasn't configured it at all — e.g. the language-targeted SAST
// engines, which are opt-in alongside the default opengrep.
func TestResolveOptInToolDisabledByDefault(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-optin", Binary: "go", DefaultEnabled: false})

	method, _, _, reason := Resolve(context.Background(), "test-optin", Options{})
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone (opt-in tool, no config at all)", method)
	}
	if !strings.Contains(reason, "opt-in tool, not enabled by default") {
		t.Errorf("reason = %q, want mention of opt-in default", reason)
	}
}

// TestResolveOptInToolEnabledExplicitly proves the operator can turn an
// opt-in tool on via security.tools.<name>.enabled without needing to touch
// anything else.
func TestResolveOptInToolEnabledExplicitly(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-optin-on", Binary: "go", DefaultEnabled: false})
	opts := Options{Tools: map[string]ToolPolicy{"test-optin-on": {Enabled: true}}}

	method, _, _, reason := Resolve(context.Background(), "test-optin-on", opts)
	if method != MethodHost {
		t.Fatalf("method = %v, reason = %q, want MethodHost once explicitly enabled", method, reason)
	}
}

// TestOptionsFromConfigDefaultsEnabledFromDescriptor is the OptionsFromConfig
// half of the same regression: configuring a tool for an unrelated reason
// (e.g. just to set `method`) without an explicit `enabled` must not
// silently opt a default-off tool back in.
func TestOptionsFromConfigDefaultsEnabledFromDescriptor(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-cfg-optin", Binary: "go", DefaultEnabled: false})

	cfg := config.SecurityConfig{Tools: map[string]config.SecurityToolConfig{
		"test-cfg-optin": {Method: "host"}, // no Enabled set
	}}
	opts := OptionsFromConfig(cfg)
	if opts.Tools["test-cfg-optin"].Enabled {
		t.Error("expected Enabled to stay false (descriptor default) when config doesn't set it explicitly")
	}

	trueVal := true
	cfg.Tools["test-cfg-optin"] = config.SecurityToolConfig{Method: "host", Enabled: &trueVal}
	opts = OptionsFromConfig(cfg)
	if !opts.Tools["test-cfg-optin"].Enabled {
		t.Error("expected explicit enabled: true to override the descriptor default")
	}
}

func TestResolveDisabledByConfig(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-disabled", Binary: "go"})
	opts := Options{Tools: map[string]ToolPolicy{"test-disabled": {Enabled: false}}}

	method, _, _, reason := Resolve(context.Background(), "test-disabled", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone", method)
	}
	if !strings.Contains(reason, "disabled") {
		t.Errorf("reason = %q, want mention of disabled", reason)
	}
}

func TestResolveAutoPrefersHostBinary(t *testing.T) {
	// "go" is guaranteed to be on PATH — this test runs via `go test`.
	withTestDescriptor(t, ScannerDescriptor{Name: "test-host", Binary: "go"})
	opts := Options{Tools: map[string]ToolPolicy{"test-host": {Enabled: true}}}

	method, _, _, reason := Resolve(context.Background(), "test-host", opts)
	if method != MethodHost {
		t.Fatalf("method = %v, reason = %q, want MethodHost", method, reason)
	}
}

func TestResolveHostMethodMissingBinaryHasNoContainerFallback(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-missing", Binary: "aegis-does-not-exist-xyz", DefaultImage: "example/image@sha256:deadbeef"})
	opts := Options{Tools: map[string]ToolPolicy{"test-missing": {Enabled: true, Method: "host"}}}

	method, _, _, reason := Resolve(context.Background(), "test-missing", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone (method:host must never fall back to container)", method)
	}
	if !strings.Contains(reason, "not installed on PATH") {
		t.Errorf("reason = %q, want mention of PATH", reason)
	}
}

func TestResolveAutoWithNoImageAndNoHostBinary(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-noimage", Binary: "aegis-does-not-exist-xyz"})
	opts := Options{Tools: map[string]ToolPolicy{"test-noimage": {Enabled: true}}}

	method, _, _, reason := Resolve(context.Background(), "test-noimage", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone", method)
	}
	if !strings.Contains(reason, "no container image configured") {
		t.Errorf("reason = %q, want mention of no container image configured", reason)
	}
}

// TestResolveContainerFallbackWithConfiguredImage is the P11.1 regression: a
// missing host binary with a configured image and an available container
// runtime must resolve to MethodContainer, not a silent skip.
func TestResolveContainerFallbackWithConfiguredImage(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-container", Binary: "aegis-does-not-exist-xyz"})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-container": {Enabled: true, Image: "example/image@sha256:deadbeef"}}}

	method, rt, image, reason := Resolve(context.Background(), "test-container", opts)
	if method != MethodContainer {
		t.Fatalf("method = %v, reason = %q, want MethodContainer", method, reason)
	}
	if rt != sandbox.RuntimeDocker {
		t.Errorf("runtime = %v, want docker", rt)
	}
	if image != "example/image@sha256:deadbeef" {
		t.Errorf("image = %q, want the configured override", image)
	}
}

// TestResolveRejectsFloatingTagImage is the P11.9 provenance-hardening
// regression: a configured image with no digest pin (a floating tag, or a
// bare image name) must resolve to MethodNone with a clear reason, not
// silently run — closing the gap where "must be digest-pinned" was only
// ever a doc comment, never enforced.
func TestResolveRejectsFloatingTagImage(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-floating", Binary: "aegis-does-not-exist-xyz"})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-floating": {Enabled: true, Image: "example/image:latest"}}}

	method, _, _, reason := Resolve(context.Background(), "test-floating", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone for a floating-tag image", method)
	}
	if !strings.Contains(reason, "not digest-pinned") {
		t.Errorf("reason = %q, want mention of digest pinning", reason)
	}
}

// TestResolveAcceptsDigestPinnedImage confirms a properly pinned image still
// resolves to MethodContainer — the enforcement in
// TestResolveRejectsFloatingTagImage shouldn't have collateral damage on
// the documented-correct form.
func TestResolveAcceptsDigestPinnedImage(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-pinned", Binary: "aegis-does-not-exist-xyz"})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-pinned": {Enabled: true, Image: "example/image@sha256:deadbeef"}}}

	method, _, _, reason := Resolve(context.Background(), "test-pinned", opts)
	if method != MethodContainer {
		t.Fatalf("method = %v, reason = %q, want MethodContainer for a digest-pinned image", method, reason)
	}
}

func TestResolveContainerMethodNoRuntimeAvailable(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-nort", Binary: "aegis-does-not-exist-xyz"})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return "", false
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-nort": {Enabled: true, Method: "container", Image: "example/image@sha256:deadbeef"}}}

	method, _, _, reason := Resolve(context.Background(), "test-nort", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone", method)
	}
	if !strings.Contains(reason, "no container runtime is available") {
		t.Errorf("reason = %q, want mention of no runtime available", reason)
	}
}

// TestResolveWSLCapableFallsBackToWSL is the P14.x regression: a
// WSLCapable tool (opengrep, kubescape — no native Windows build) with no
// host binary and no container image must fall back to MethodWSL when a
// distro has the binary, instead of a bare MethodNone.
func TestResolveWSLCapableFallsBackToWSL(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-wsl", Binary: "aegis-does-not-exist-xyz", WSLCapable: true})
	withWSLBinaryAvailable(t, func(context.Context, string, string) bool { return true })
	opts := Options{Tools: map[string]ToolPolicy{"test-wsl": {Enabled: true}}}

	method, _, _, reason := Resolve(context.Background(), "test-wsl", opts)
	if method != MethodWSL {
		t.Fatalf("method = %v, reason = %q, want MethodWSL", method, reason)
	}
}

// TestResolveWSLCapableTriesWSLAfterContainerRuntimeUnavailable proves the
// auto path still reaches WSL when a container image is configured but no
// runtime is running — WSL is a fallback of last resort, not a substitute
// for a properly configured container.
func TestResolveWSLCapableTriesWSLAfterContainerRuntimeUnavailable(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-wsl-cfallback", Binary: "aegis-does-not-exist-xyz", WSLCapable: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return "", false
	})
	withWSLBinaryAvailable(t, func(context.Context, string, string) bool { return true })
	opts := Options{Tools: map[string]ToolPolicy{"test-wsl-cfallback": {Enabled: true, Image: "example/image@sha256:deadbeef"}}}

	method, _, _, reason := Resolve(context.Background(), "test-wsl-cfallback", opts)
	if method != MethodWSL {
		t.Fatalf("method = %v, reason = %q, want MethodWSL", method, reason)
	}
}

// TestResolveWSLCapableNoneWhenWSLAlsoUnavailable proves a WSLCapable tool
// still reports MethodNone (with a reason mentioning WSL) when WSL doesn't
// have it either — no silent success.
func TestResolveWSLCapableNoneWhenWSLAlsoUnavailable(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-wsl-none", Binary: "aegis-does-not-exist-xyz", WSLCapable: true})
	withWSLBinaryAvailable(t, func(context.Context, string, string) bool { return false })
	opts := Options{Tools: map[string]ToolPolicy{"test-wsl-none": {Enabled: true}}}

	method, _, _, reason := Resolve(context.Background(), "test-wsl-none", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone", method)
	}
	if !strings.Contains(reason, "WSL") {
		t.Errorf("reason = %q, want mention of WSL", reason)
	}
}

// TestResolveNonWSLCapableNeverConsultsWSL proves a tool without
// WSLCapable never triggers the WSL check at all (even if it happened to be
// available) — every scanner except opengrep/kubescape has no Scan-side
// WSL branch, so offering the method for them would misroute execution.
func TestResolveNonWSLCapableNeverConsultsWSL(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-not-wsl-capable", Binary: "aegis-does-not-exist-xyz"})
	called := false
	withWSLBinaryAvailable(t, func(context.Context, string, string) bool { called = true; return true })
	opts := Options{Tools: map[string]ToolPolicy{"test-not-wsl-capable": {Enabled: true}}}

	method, _, _, _ := Resolve(context.Background(), "test-not-wsl-capable", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone (no image, no container runtime, WSL not consulted)", method)
	}
	if called {
		t.Error("wslBinaryAvailable was called for a non-WSLCapable descriptor")
	}
}

// TestResolveExplicitWSLMethod covers the explicit
// security.tools.<name>.method: "wsl" knob, both for a capable tool and one
// with no Scan-side WSL branch wired.
func TestResolveExplicitWSLMethod(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-explicit-wsl", Binary: "aegis-does-not-exist-xyz", WSLCapable: true})
	withWSLBinaryAvailable(t, func(context.Context, string, string) bool { return true })
	opts := Options{Tools: map[string]ToolPolicy{"test-explicit-wsl": {Enabled: true, Method: "wsl"}}}

	method, _, _, reason := Resolve(context.Background(), "test-explicit-wsl", opts)
	if method != MethodWSL {
		t.Fatalf("method = %v, reason = %q, want MethodWSL", method, reason)
	}

	withTestDescriptor(t, ScannerDescriptor{Name: "test-explicit-wsl-incapable", Binary: "aegis-does-not-exist-xyz"})
	opts = Options{Tools: map[string]ToolPolicy{"test-explicit-wsl-incapable": {Enabled: true, Method: "wsl"}}}
	method, _, _, reason = Resolve(context.Background(), "test-explicit-wsl-incapable", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone for a non-WSLCapable descriptor", method)
	}
	if !strings.Contains(reason, "no WSL execution path wired") {
		t.Errorf("reason = %q, want mention of no WSL execution path wired", reason)
	}
}

func TestDescriptorFor(t *testing.T) {
	d, ok := DescriptorFor("trivy")
	if !ok {
		t.Fatal("expected a built-in descriptor for trivy")
	}
	if d.Binary != "trivy" {
		t.Errorf("Binary = %q, want trivy", d.Binary)
	}
	if _, ok := DescriptorFor("not-a-real-scanner"); ok {
		t.Error("expected no descriptor for an unknown name")
	}
}

func TestContainerRunArgsHardening(t *testing.T) {
	args := containerRunArgs(sandbox.RuntimeDocker, "example/image@sha256:deadbeef", "/host/project", "detect", "--json")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--rm", "--network none", "--cap-drop=ALL", "--security-opt=no-new-privileges", "-v /host/project:/src", "-w /src", "example/image@sha256:deadbeef", "detect --json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestContainerRunArgsAppleContainersSkipsUnsupportedFlags(t *testing.T) {
	args := containerRunArgs(sandbox.RuntimeAppleContainers, "example/image@sha256:deadbeef", "/host/project", "detect")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--cap-drop") || strings.Contains(joined, "--security-opt") {
		t.Errorf("Apple Containers does not support these OCI flags, got %q", joined)
	}
}

func TestDescriptorsSortedByName(t *testing.T) {
	all := Descriptors()
	if len(all) < 3 {
		t.Fatalf("expected at least 3 built-in descriptors, got %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Name < all[i-1].Name {
			t.Errorf("Descriptors() not sorted: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
}

// --- P34.9: HostBroken (a host binary that's present but cannot work here) ---

// TestResolveHostBrokenFallsBackToContainer is P34.9's core regression: on a
// platform where the host binary is installed but structurally broken,
// "auto" must route to the container rather than running it. Binary: "go" is
// deliberate — it's guaranteed on PATH while these tests run, so this proves
// the HostBroken rule beats a real lookPath hit rather than passing because
// no binary was found.
func TestResolveHostBrokenFallsBackToContainer(t *testing.T) {
	withHostGOOS(t, "windows")
	withTestDescriptor(t, ScannerDescriptor{
		Name:       "test-hostbroken",
		Binary:     "go",
		HostBroken: map[string]string{"windows": "test-hostbroken has no working Windows host build"},
	})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-hostbroken": {Enabled: true, Image: "example/image@sha256:deadbeef"}}}

	method, _, _, reason := Resolve(context.Background(), "test-hostbroken", opts)
	if method != MethodContainer {
		t.Fatalf("method = %v, reason = %q, want MethodContainer (host binary present but broken on this platform)", method, reason)
	}
}

// TestResolveHostBrokenOnlyAppliesToNamedPlatform guards the obvious
// over-reach: the rule must not disable the host binary everywhere.
func TestResolveHostBrokenOnlyAppliesToNamedPlatform(t *testing.T) {
	withHostGOOS(t, "linux")
	withTestDescriptor(t, ScannerDescriptor{
		Name:       "test-hostbroken-elsewhere",
		Binary:     "go",
		HostBroken: map[string]string{"windows": "no working Windows host build"},
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-hostbroken-elsewhere": {Enabled: true}}}

	method, _, _, reason := Resolve(context.Background(), "test-hostbroken-elsewhere", opts)
	if method != MethodHost {
		t.Fatalf("method = %v, reason = %q, want MethodHost (HostBroken names windows, not linux)", method, reason)
	}
}

// TestResolveHostBrokenRefusesExplicitHostMethod proves an operator who
// pinned method: host gets the real reason and the way out, rather than
// either a traceback (the pre-P34.9 behavior) or a silent downgrade to a
// method they explicitly didn't ask for.
func TestResolveHostBrokenRefusesExplicitHostMethod(t *testing.T) {
	withHostGOOS(t, "windows")
	withTestDescriptor(t, ScannerDescriptor{
		Name:       "test-hostbroken-pinned",
		Binary:     "go",
		HostBroken: map[string]string{"windows": "no working Windows host build: its engine crashes on the stub"},
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-hostbroken-pinned": {Enabled: true, Method: "host"}}}

	method, _, _, reason := Resolve(context.Background(), "test-hostbroken-pinned", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone (method: host pinned on a platform where host can't work)", method)
	}
	if !strings.Contains(reason, "no working Windows host build") {
		t.Errorf("reason = %q, want the descriptor's own explanation", reason)
	}
	if !strings.Contains(reason, "container") {
		t.Errorf("reason = %q, want the container method named as the way out", reason)
	}
	// The binary *is* installed; saying otherwise sends the operator to
	// reinstall it, and would also trip AvailabilityNote's "not installed"
	// heuristic into offering an install that cannot help.
	if strings.Contains(reason, "not installed") {
		t.Errorf("reason = %q, must not claim the binary is not installed", reason)
	}
}

// TestResolveHostBrokenWithNoContainerFallbackExplainsItself covers the
// dead-end: no image, nothing runnable. The reason must still be about the
// platform, not a bogus "not installed".
func TestResolveHostBrokenWithNoContainerFallbackExplainsItself(t *testing.T) {
	withHostGOOS(t, "windows")
	withTestDescriptor(t, ScannerDescriptor{
		Name:       "test-hostbroken-noimage",
		Binary:     "go",
		HostBroken: map[string]string{"windows": "no working Windows host build"},
	})
	opts := Options{Tools: map[string]ToolPolicy{"test-hostbroken-noimage": {Enabled: true}}}

	method, _, _, reason := Resolve(context.Background(), "test-hostbroken-noimage", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone (no image, host unusable)", method)
	}
	if !strings.Contains(reason, "no working Windows host build") {
		t.Errorf("reason = %q, want the platform explanation", reason)
	}
	if strings.Contains(reason, "not installed") {
		t.Errorf("reason = %q, must not claim the binary is not installed", reason)
	}
}

// TestNjsscanDeclaresWindowsHostBroken pins the real descriptor: njsscan on a
// Windows host dies in libsast's own engine stub (P34.9), and it must never be
// routed there. Asserted on the descriptor rather than by running njsscan, so
// it holds on CI machines without it installed.
func TestNjsscanDeclaresWindowsHostBroken(t *testing.T) {
	d, ok := DescriptorFor("njsscan")
	if !ok {
		t.Fatal("njsscan descriptor missing")
	}
	if d.HostBroken["windows"] == "" {
		t.Error("njsscan must declare HostBroken[windows]: its libsast engine stubs out its own analysis call on Windows and crashes on the stub")
	}
	// A guided `pipx install njsscan` on Windows installs exactly the binary
	// HostBroken then refuses to run.
	if _, has := d.Install["windows"]; has {
		t.Error("njsscan must not offer a Windows host install: the installed binary can only produce a traceback")
	}
	for _, osName := range []string{"darwin", "linux"} {
		if d.Install[osName] == "" {
			t.Errorf("njsscan lost its %s install command", osName)
		}
	}
}
