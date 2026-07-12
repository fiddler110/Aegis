//go:build windows

package hwinfo

import (
	"testing"
	"unsafe"
)

// TestMemoryStatusExLayoutIsSane guards the hand-rolled MEMORYSTATUSEX
// mirror: the real Win32 struct is 64 bytes on 64-bit Windows (one uint32
// dwLength + one uint32 dwMemoryLoad + seven uint64 fields = 8 + 56 = 64,
// with no padding since every 64-bit field starts on an 8-byte boundary
// after the two uint32s pack together). If the struct layout ever drifts
// (a field added/removed/reordered), GlobalMemoryStatusEx would read/write
// past what Go allocated — this catches that at test time instead of via a
// corrupted read in the field.
func TestMemoryStatusExLayoutIsSane(t *testing.T) {
	var mem memoryStatusEx
	if got, want := unsafe.Sizeof(mem), uintptr(64); got != want {
		t.Errorf("unsafe.Sizeof(memoryStatusEx{}) = %d, want %d", got, want)
	}
}

// TestTotalRAMWindows exercises the real GlobalMemoryStatusEx call. Skips
// (rather than fails) on the fail-soft path, matching totalRAM's contract —
// though on any real Windows install this call should never fail.
func TestTotalRAMWindows(t *testing.T) {
	bytes, src := totalRAM()
	if src == SourceUnknown {
		t.Skip("GlobalMemoryStatusEx unexpectedly failed in this environment")
	}
	if src != SourceWinAPI {
		t.Errorf("source = %q, want %q", src, SourceWinAPI)
	}
	if bytes == 0 {
		t.Error("totalRAM() reported a non-unknown source but 0 bytes")
	}
}
