//go:build darwin

package hwinfo

import "testing"

// TestTotalRAMDarwin exercises the real `sysctl -n hw.memsize` call. Skips
// (rather than fails) if sysctl isn't on PATH or errors, matching totalRAM's
// own fail-soft contract — this should be rare on a real macOS install but
// must never fail the suite.
func TestTotalRAMDarwin(t *testing.T) {
	bytes, src := totalRAM()
	if src == SourceUnknown {
		t.Skip("sysctl unavailable in this environment; fail-soft path already covered by TestParseSysctlMemsize")
	}
	if src != SourceSysctl {
		t.Errorf("source = %q, want %q", src, SourceSysctl)
	}
	if bytes == 0 {
		t.Error("totalRAM() reported a non-unknown source but 0 bytes")
	}
}
