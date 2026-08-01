package cli

import (
	"log/slog"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// driveExtraRoots resolves workspace.additional_roots for a CLI run rooted at
// cwd (P52.13), returning only the *additional* roots — cwd itself is already
// the registry's construction-time root, so re-adding it here would let a
// stale entry contradict its writability.
//
// Rejections are logged rather than returned: a root that has gone missing or
// lost its trust decision should degrade the run to ordinary single-root
// confinement, not refuse to start it. The daemon's equivalent
// (server.workspaceRootsFor) memoizes because it is asked once per turn; a CLI
// run resolves once at startup, so this one does not.
func driveExtraRoots(cwd string, cfg *config.Config, logger *slog.Logger) []sandbox.Root {
	if cfg == nil || len(cfg.Workspace.AdditionalRoots) == 0 {
		return nil
	}
	roots, rejected := config.ResolveAdditionalRoots(cwd, cfg.Workspace)
	for _, r := range rejected {
		logger.Warn("workspace.additional_roots entry ignored", "reason", r)
	}
	return roots[1:]
}
