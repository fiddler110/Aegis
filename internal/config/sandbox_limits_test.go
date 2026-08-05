package config

import "testing"

// TestLoadDefaults_SandboxLimits is the P60.1 end-to-end default check: the
// caps have to survive the config layers and arrive in the sandbox package's
// own shape, or the container backend runs uncapped exactly as it did before —
// silently, since nothing else in the system would notice.
func TestLoadDefaults_SandboxLimits(t *testing.T) {
	redirectConfigDir(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	lim := cfg.Sandbox.Limits
	if lim.Memory == "" || lim.CPUs == "" || lim.PIDs <= 0 {
		t.Fatalf("default sandbox.limits = %+v, want all three capped", lim)
	}
	sb := cfg.Sandbox.Limits.Sandbox()
	if sb.Memory != lim.Memory || sb.CPUs != lim.CPUs || sb.PIDs != lim.PIDs {
		t.Errorf("Sandbox() = %+v, want the configured values %+v", sb, lim)
	}
	if sb.Empty() {
		t.Error("defaulted limits must not report Empty — SelectSandbox skips its enforcement notice on Empty")
	}
}

// TestSandboxLimits_ExplicitlyUncapped: emptying the keys is the documented
// escape hatch for a heavy toolchain, and it has to reach the sandbox as "no
// flags" rather than as an empty-string flag value the runtime would reject.
func TestSandboxLimits_ExplicitlyUncapped(t *testing.T) {
	var lim SandboxLimits
	if !lim.Sandbox().Empty() {
		t.Error("zeroed SandboxLimits must convert to empty sandbox.ResourceLimits")
	}
}
