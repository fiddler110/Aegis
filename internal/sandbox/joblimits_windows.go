//go:build windows

package sandbox

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// resourceLimiter owns a Windows job object that bounds the memory and
// process count of everything run under it (P81.22/FIND-22): the container
// backend has had per-run resource caps since P60.1 (see ResourceFlags in
// docker.go), while the unsandboxed local backend — the one every current
// Windows dev box actually falls back to (P77.6) — had none at all.
//
// CPU-rate limiting is deliberately not implemented: the job-object API for
// it (JOBOBJECT_CPU_RATE_CONTROL_INFORMATION, JobObjectCpuRateControlInformation)
// is not exposed by golang.org/x/sys/windows, and hand-rolling the raw
// struct/uintptr layout for it was judged not worth shipping untested in this
// pass. Memory and process-count are the two limits enforced; see the P81.22
// report for the POSIX (rlimit/cgroup) follow-up this leaves open too.
type resourceLimiter struct {
	handle windows.Handle
}

// newResourceLimiter builds a job object configured for lim, or returns
// (nil, nil) when lim has nothing this platform can enforce (empty, or only
// a CPU cap — CPUs is silently not applied here, same "only where verified"
// stance ResourceFlags takes per container runtime).
//
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE means closing the handle (Close, or this
// process exiting) terminates any process still assigned to the job — so a
// runaway command does not outlive the daemon even if the daemon itself is
// killed before the command's own timeout fires.
func newResourceLimiter(lim ResourceLimits) (*resourceLimiter, error) {
	mem, hasMem := parseMemoryLimit(lim.Memory)
	hasPIDs := lim.PIDs > 0
	if !hasMem && !hasPIDs {
		return nil, nil
	}
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("sandbox: CreateJobObject: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if hasMem {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
		info.ProcessMemoryLimit = uintptr(mem)
	}
	if hasPIDs {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
		info.BasicLimitInformation.ActiveProcessLimit = uint32(lim.PIDs)
	}
	if _, err := windows.SetInformationJobObject(
		h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("sandbox: SetInformationJobObject: %w", err)
	}
	return &resourceLimiter{handle: h}, nil
}

// assign places proc under the job's limits. Must be called after
// cmd.Start() (a job assignment needs a real process handle) — a process
// freshly returned by Start has not yet run any of its own code, so this
// still catches everything the process (and any child it spawns while still
// a job member) does from here on.
func (j *resourceLimiter) assign(proc *os.Process) error {
	if j == nil || proc == nil {
		return nil
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(proc.Pid))
	if err != nil {
		return fmt.Errorf("sandbox: OpenProcess for job assignment: %w", err)
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(j.handle, ph)
}

// Close releases the job object. Combined with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, this also terminates any process still
// assigned to it — harmless in the normal case (the command has already
// exited by the time this runs) and a safety net in the timeout/cancellation
// case (a child the shell spawned and left running past its parent's exit).
func (j *resourceLimiter) Close() error {
	if j == nil {
		return nil
	}
	return windows.CloseHandle(j.handle)
}

// parseMemoryLimit parses a docker-style memory string ("4G", "512M", "1024K",
// or a bare byte count) into a byte count, mirroring the vocabulary
// ResourceLimits.Memory already documents for the container backend so the
// same config value means the same thing on both. Empty or unparseable
// returns ok=false, which callers treat as "no memory cap" rather than an
// error — the same posture ResourceLimits.Empty() takes.
func parseMemoryLimit(s string) (bytes int64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'g', 'G':
		mult, s = 1<<30, s[:len(s)-1]
	case 'm', 'M':
		mult, s = 1<<20, s[:len(s)-1]
	case 'k', 'K':
		mult, s = 1<<10, s[:len(s)-1]
	case 'b', 'B':
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return int64(n * float64(mult)), true
}

// resourceLimiterSupported reports whether this platform's newResourceLimiter
// can enforce ResourceLimits at all — used by sandbox selection to warn
// (mirroring SupportsResourceLimits for the container backend) rather than
// let an operator assume a configured cap is in force.
func ResourceLimiterSupported() bool { return true }
