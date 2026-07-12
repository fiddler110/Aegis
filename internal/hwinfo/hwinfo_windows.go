//go:build windows

package hwinfo

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx mirrors just enough of the Win32 MEMORYSTATUSEX struct
// layout for GlobalMemoryStatusEx to fill in ullTotalPhys. golang.org/x/sys
// (already a project dependency — see internal/fsguard/fsguard_windows.go
// and internal/swarm/procattr_windows.go for the same idiom) exposes the
// DLL-loading primitives (NewLazySystemDLL/Proc.Call) but doesn't wrap this
// particular API, so the struct is redeclared here rather than pulling in
// an extra dependency for one call.
type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// totalRAM calls the Win32 GlobalMemoryStatusEx API directly. Fails soft (0,
// SourceUnknown) if the call errors — shouldn't happen on any real Windows
// install, but detection must never crash the CLI/daemon.
func totalRAM() (uint64, Source) {
	var mem memoryStatusEx
	mem.dwLength = uint32(unsafe.Sizeof(mem))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if r == 0 {
		return 0, SourceUnknown
	}
	return mem.ullTotalPhys, SourceWinAPI
}
