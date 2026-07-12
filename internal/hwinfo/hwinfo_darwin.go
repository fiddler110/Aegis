//go:build darwin

package hwinfo

import (
	"context"
	"os/exec"
	"time"
)

// totalRAM shells out to `sysctl -n hw.memsize`, which prints total physical
// RAM in bytes. macOS has no /proc filesystem and no portable Go stdlib call
// for this, so this is the same best-effort, subprocess-based approach
// internal/ollamainfo and internal/discover use elsewhere in this codebase
// for host/service introspection. Fails soft (0, SourceUnknown) if sysctl is
// missing, times out, or returns something unparsable.
func totalRAM() (uint64, Source) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, SourceUnknown
	}
	n, ok := parseSysctlMemsize(out)
	if !ok {
		return 0, SourceUnknown
	}
	return n, SourceSysctl
}
