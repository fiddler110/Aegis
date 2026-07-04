//go:build !windows && !linux

package swarm

import (
	"os/exec"
	"syscall"
)

// sysProcAttr gives a spawned worker its own process group, so Shutdown's
// context cancellation can reliably signal its whole process tree rather
// than just the directly-tracked child.
//
// Unlike Linux, this platform has no direct equivalent to Pdeathsig: there is
// no simple, portable primitive to have the kernel kill a child automatically
// when its parent dies (crash, an external SIGKILL). An abnormal daemon
// death can still orphan worker processes here (P9) — closing that gap would
// require a supervisory mechanism (e.g. a kqueue EVFILT_PROC watch on the
// parent PID from within the worker), which is a larger change than this
// process-group baseline.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// afterStart has nothing further to do on this platform.
func afterStart(*exec.Cmd) {}
