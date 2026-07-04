//go:build linux

package swarm

import (
	"os/exec"
	"syscall"
)

// sysProcAttr gives a spawned worker its own process group (so Shutdown's
// context cancellation can reliably signal its whole process tree, not just
// the directly-tracked child) and asks the kernel to SIGKILL it if this
// daemon process itself dies — crash, an external SIGKILL, anything short of
// SIGSTOP — before it gets a chance to reap children the normal way (P9).
// Without Pdeathsig, an abnormal daemon death orphans worker processes that
// keep running, including making further model calls, with nothing left to
// reap them.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}

// afterStart has nothing further to do on Linux: Pdeathsig above is already
// applied at process-creation time via SysProcAttr.
func afterStart(*exec.Cmd) {}
