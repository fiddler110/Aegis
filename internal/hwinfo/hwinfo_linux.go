//go:build linux

package hwinfo

import "os"

// totalRAM reads MemTotal from /proc/meminfo — the same source `free`/`top`
// use — and needs no subprocess. Fails soft (0, SourceUnknown) if the file
// is missing or unparsable; some minimal/container Linux environments don't
// expose a full /proc.
func totalRAM() (uint64, Source) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, SourceUnknown
	}
	kb, ok := parseMemTotalKB(data)
	if !ok {
		return 0, SourceUnknown
	}
	return kb * 1024, SourceProcMeminfo
}
