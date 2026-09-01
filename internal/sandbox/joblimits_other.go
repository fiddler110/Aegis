//go:build !windows

package sandbox

import "os"

// resourceLimiter is the non-Windows stub: the local backend's resource caps
// (P81.22/FIND-22) are implemented via Windows job objects only in this pass.
// A POSIX equivalent (rlimits for memory/PID count, or a cgroup for a real
// resource-controller-backed cap) is a documented follow-up, not a silent
// gap — see resourceLimiterSupported and the P81.22 report: sandbox selection
// warns when limits are configured but this platform cannot enforce them,
// the same way it already does for a container runtime whose CLI doesn't
// accept a resource flag.
type resourceLimiter struct{}

func newResourceLimiter(ResourceLimits) (*resourceLimiter, error) { return nil, nil }

func (*resourceLimiter) assign(*os.Process) error { return nil }

func (*resourceLimiter) Close() error { return nil }

// resourceLimiterSupported reports whether this platform's newResourceLimiter
// can enforce ResourceLimits at all — used by sandbox selection to warn
// instead of letting an operator assume a configured cap is in force.
func ResourceLimiterSupported() bool { return false }
