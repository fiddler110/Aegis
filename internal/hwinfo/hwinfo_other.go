//go:build !linux && !darwin && !windows

package hwinfo

// totalRAM is unimplemented on platforms other than Linux/macOS/Windows
// (e.g. the BSDs). Fails soft: callers degrade to hardware-agnostic
// guidance rather than erroring.
func totalRAM() (uint64, Source) {
	return 0, SourceUnknown
}
