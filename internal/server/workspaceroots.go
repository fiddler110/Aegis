package server

import (
	"sync"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// workspaceRootCache memoizes the resolved additional-root set per session
// workdir (P52.13).
//
// Resolution reads the workspace-trust store off disk and stats every
// configured root, so doing it inside newEngine — which runs once per turn,
// per sub-agent spawn, and per debate round — would put a handful of syscalls
// on every model turn to answer a question whose inputs (config plus the
// trust store) do not change while the daemon is up. Restarting the daemon is
// already how a trust decision is applied, which `aegis trust` says outright.
type workspaceRootCache struct {
	mu sync.Mutex
	by map[string][]sandbox.Root
}

// workspaceRootsFor returns the confinement roots for a session rooted at
// workdir: the workdir itself as the primary writable root, followed by each
// trusted entry of workspace.additional_roots.
//
// An empty workdir means "the tool's own construction-time root" (the daemon
// workspace), which is exactly the case effectiveRoots already handles from
// the tool side — so return nil rather than inventing a root here, leaving
// confinement untouched.
func (s *Server) workspaceRootsFor(workdir string) []sandbox.Root {
	if workdir == "" || len(s.cfg.Workspace.AdditionalRoots) == 0 {
		return nil
	}
	s.wsRoots.mu.Lock()
	defer s.wsRoots.mu.Unlock()
	if roots, ok := s.wsRoots.by[workdir]; ok {
		return roots
	}

	roots, rejected := config.ResolveAdditionalRoots(workdir, s.cfg.Workspace)
	// Drop the primary — the engine carries it separately as Workdir, and
	// duplicating it would let a stale entry contradict its writability.
	extra := roots[1:]
	for _, r := range rejected {
		s.logger.Warn("workspace.additional_roots entry ignored", "workdir", workdir, "reason", r)
	}
	if len(extra) > 0 {
		paths := make([]string, 0, len(extra))
		for _, r := range extra {
			mode := "ro"
			if r.Writable {
				mode = "rw"
			}
			paths = append(paths, r.Path+" ("+mode+")")
		}
		s.logger.Info("workspace additional roots active", "workdir", workdir, "roots", paths)
	}

	if s.wsRoots.by == nil {
		s.wsRoots.by = make(map[string][]sandbox.Root)
	}
	s.wsRoots.by[workdir] = extra
	return extra
}
