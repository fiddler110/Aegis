package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/workspacetrust"
)

// ResolveAdditionalRoots turns the configured workspace.additional_roots into
// the root set workspace-confined tools validate against (P52.13), with
// primary as the session workdir.
//
// The returned slice is always usable as-is: index 0 is primary, writable —
// so a caller with no additional roots configured gets exactly the
// single-root behavior that predates this feature, and every path validation
// can go through the same code path.
//
// Rejected entries are returned as human-readable reasons rather than as an
// error: a stale or untrusted root should degrade the session to plain
// single-root confinement with a warning, not refuse to start it. Four things
// get an entry rejected:
//
//   - it does not exist, or is not a directory — a typo'd root that silently
//     validated nothing would be worse than a visible warning;
//   - it is the primary root, or nested inside it — already reachable, and
//     admitting it a second time would let a read-only entry contradict the
//     primary root's writability;
//   - it has no *current* `aegis trust` decision of its own. An additional root
//     does not inherit the primary workspace's trust: trusting the repo you are
//     working in is not the same decision as granting it a window into
//     another directory on the host. Since P66.25/SEC-07 "current" also means
//     the root's own security-relevant config has not moved since the grant —
//     a stale grant is rejected like a missing one, on the same reasoning that
//     a directory which has acquired a `hooks:` block since you approved it is
//     a directory you have not approved.
func ResolveAdditionalRoots(primary string, cfg WorkspaceConfig) (roots []sandbox.Root, rejected []string) {
	primaryAbs, err := filepath.Abs(primary)
	if err != nil {
		primaryAbs = primary
	}
	roots = []sandbox.Root{{Path: primaryAbs, Writable: true}}
	if len(cfg.AdditionalRoots) == 0 {
		return roots, nil
	}

	store := workspacetrust.Open(WorkspaceTrustStorePath())
	seen := map[string]bool{primaryAbs: true}
	for _, ar := range cfg.AdditionalRoots {
		if ar.Path == "" {
			continue
		}
		abs := os.ExpandEnv(ar.Path)
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(primaryAbs, abs)
		}
		abs = filepath.Clean(abs)
		// Resolve symlinks so the root recorded here is the same identity
		// ValidatePathIn will compare a resolved candidate against, and so a
		// symlinked alias of the primary root is caught by the nesting check
		// below rather than slipping through as a distinct root.
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}

		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			rejected = append(rejected, fmt.Sprintf("%s: not an existing directory", ar.Path))
			continue
		}
		if seen[abs] {
			rejected = append(rejected, fmt.Sprintf("%s: already configured", ar.Path))
			continue
		}
		if _, err := sandbox.ValidatePath(primaryAbs, abs); err == nil {
			rejected = append(rejected, fmt.Sprintf("%s: inside the workspace root %s (already reachable)", ar.Path, primaryAbs))
			continue
		}
		if !store.IsTrusted(abs, SecurityFingerprint(abs)) {
			rejected = append(rejected, fmt.Sprintf("%s: not trusted (run `aegis trust` in that directory)", ar.Path))
			continue
		}
		seen[abs] = true
		roots = append(roots, sandbox.Root{Path: abs, Writable: ar.Writable})
	}
	return roots, rejected
}
