//go:build windows

package swarm

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sysProcAttr puts a spawned worker in its own process group (CTRL_BREAK can
// then be delivered to the whole group if ever needed). The real abnormal-death
// cleanup below happens via a Job Object, assigned in afterStart once the
// process exists.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
	jobErr    error
)

// workerJobObject returns a process-lifetime Job Object configured to kill
// every process assigned to it as soon as the job's last handle closes —
// which happens automatically when this daemon process itself exits for any
// reason, including a crash or an external TerminateProcess. Without this, an
// abnormal daemon death orphans worker processes that keep running,
// including making further model calls, with nothing left to reap them (P9).
func workerJobObject() (windows.Handle, error) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = err
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			windows.CloseHandle(h)
			jobErr = err
			return
		}
		jobHandle = h
	})
	return jobHandle, jobErr
}

// afterStart assigns the just-started worker process to the shared job
// object, if one could be created. Failure is non-fatal — the worker still
// runs normally, just without the extra OS-level lifecycle guarantee.
func afterStart(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	job, err := workerJobObject()
	if err != nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(job, h)
}
