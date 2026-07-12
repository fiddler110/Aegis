//go:build linux

package hwinfo

import "testing"

// TestTotalRAMLinux exercises the real /proc/meminfo read. Skips (rather
// than fails) if the file is missing or unparsable — some minimal/container
// environments don't expose a full /proc, and this must degrade gracefully
// there too, matching totalRAM's own fail-soft contract.
func TestTotalRAMLinux(t *testing.T) {
	bytes, src := totalRAM()
	if src == SourceUnknown {
		t.Skip("/proc/meminfo not readable/parsable in this environment; fail-soft path already covered by TestParseMemTotalKB")
	}
	if src != SourceProcMeminfo {
		t.Errorf("source = %q, want %q", src, SourceProcMeminfo)
	}
	if bytes == 0 {
		t.Error("totalRAM() reported a non-unknown source but 0 bytes")
	}
}
