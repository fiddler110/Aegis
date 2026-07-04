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
// synthetic name (never "semgrep"/"trivy"/"gitleaks") keeps these tests
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

// TestResolveOptInToolDisabledByDefault is the P11.3 regression: a tool
// descriptor with DefaultEnabled: false must resolve to MethodNone with a
// distinct "opt-in" reason (not "disabled by configuration") when the
// operator hasn't configured it at all — e.g. semgrep and the language-
// targeted SAST engines, which are opt-in now that opengrep is the default.
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
