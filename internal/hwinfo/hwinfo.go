// Package hwinfo does small, best-effort, purely local hardware detection —
// CPU core count and total system RAM — so internal/modelcatalog and the
// model pickers (CLI `aegis models --recommend`, TUI `/models`) have enough
// signal to narrow local-model recommendations to what a machine can
// actually run.
//
// This package deliberately stops at CPU cores and RAM. P17.5 (adaptive
// sub-agent concurrency, see internal/swarm/adaptive_test.go and its release
// notes) already weighed and rejected GPU/VRAM introspection for this
// codebase: Ollama doesn't expose its own computed concurrency/VRAM budget
// over its API, so shelling out to nvidia-smi or a platform-specific GPU
// driver query would mean Aegis reimplementing that heuristic blind from a
// fragile, platform-specific proxy signal. The same reasoning applies here —
// no GPU/VRAM detection is attempted, on any platform, ever.
//
// runtime.NumCPU() covers CPU cores portably. Total RAM has no equivalent
// single Go stdlib call, so each OS gets a narrow, best-effort helper (see
// hwinfo_linux.go, hwinfo_darwin.go, hwinfo_windows.go, hwinfo_other.go).
// Every path fails soft to Source "unknown" with TotalRAMBytes 0 rather than
// erroring or panicking — an undetectable host degrades the recommendation
// to hardware-agnostic guidance instead of blocking anything, matching this
// package's sibling internal/ollamainfo's posture toward introspection it
// can't fully trust.
package hwinfo

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// Source identifies how TotalRAMBytes was determined, or that it wasn't.
type Source string

const (
	// SourceProcMeminfo means Linux's /proc/meminfo was read directly.
	SourceProcMeminfo Source = "proc_meminfo"
	// SourceSysctl means macOS's `sysctl hw.memsize` was shelled out to.
	SourceSysctl Source = "sysctl"
	// SourceWinAPI means the Win32 GlobalMemoryStatusEx API was called.
	SourceWinAPI Source = "windows_api"
	// SourceUnknown means detection failed or the platform is unsupported.
	SourceUnknown Source = "unknown"
)

// Info is the detected hardware snapshot.
type Info struct {
	CPUCores      int
	TotalRAMBytes uint64 // 0 when undetectable
	RAMSource     Source
}

// Detect probes CPU core count (always reliable) and total system RAM
// (best-effort, platform-specific, fails soft — see package doc). Cheap and
// local: no network calls, safe to call on every /models or `aegis models
// --recommend` invocation.
func Detect() Info {
	ramBytes, src := totalRAM()
	return Info{
		CPUCores:      runtime.NumCPU(),
		TotalRAMBytes: ramBytes,
		RAMSource:     src,
	}
}

// RAMKnown reports whether TotalRAMBytes reflects a real detection rather
// than the fail-soft "unknown" default.
func (i Info) RAMKnown() bool {
	return i.RAMSource != SourceUnknown && i.RAMSource != "" && i.TotalRAMBytes > 0
}

// TotalRAMGB returns total RAM in gibibytes, 0 when unknown.
func (i Info) TotalRAMGB() float64 {
	if !i.RAMKnown() {
		return 0
	}
	return float64(i.TotalRAMBytes) / (1 << 30)
}

// Describe renders a human-readable summary for CLI/TUI display.
func (i Info) Describe() string {
	if !i.RAMKnown() {
		return fmt.Sprintf("%d CPU cores, RAM unknown (detection unavailable on this platform/environment)", i.CPUCores)
	}
	return fmt.Sprintf("%d CPU cores, ~%.0fGB RAM (source: %s)", i.CPUCores, i.TotalRAMGB(), i.RAMSource)
}

// parseMemTotalKB extracts the value (in kB) of the "MemTotal:" line from
// /proc/meminfo content. Pure and portable so it can be unit-tested on any
// platform without needing a real /proc filesystem; only the file read
// happens in the Linux-only totalRAM() in hwinfo_linux.go.
func parseMemTotalKB(data []byte) (uint64, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb, true
	}
	return 0, false
}

// parseSysctlMemsize parses the byte count printed by `sysctl -n hw.memsize`
// on macOS. Pure and portable so it can be unit-tested on any platform;
// only the subprocess call happens in the Darwin-only totalRAM() in
// hwinfo_darwin.go.
func parseSysctlMemsize(out []byte) (uint64, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
