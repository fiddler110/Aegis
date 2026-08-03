package security

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// P55.8: running gosec without a Go toolchain on the host.
//
// gosec is the one scanner container-only could not simply absorb, and the
// reason is worth restating because it decides the whole shape of this file.
// gosec is compile-assisted: it resolves packages through `go list`, so it
// needs a Go toolchain *and* a populated module cache. Denied either it does not
// fail, and the two denials degrade it differently — which is worth stating
// precisely, because the second one is the nastier of the pair. Measured on this
// repo (2026-08-03, gosec 2.28.0):
//
//	host binary                     244 findings
//	no Go toolchain at all            0 findings, exit 0
//	toolchain, cold module cache    258 findings, exit 0
//	toolchain, warm module cache    283 findings, exit 0
//
// The zero is the shape this package already documents. The 258 is worse. With
// no modules to resolve, `go list` leaves packages with type errors, gosec logs
// "skipping SSA analysis" to stderr and carries on with the rules that need only
// an AST — so every type-aware rule silently stops firing (G115 3→0, G118 5→0,
// G124 1→0, G702 1→0, G122 5→1, G703 13→2) while the total still looks perfectly
// healthy. A zero at least draws the eye; a plausible 258 does not.
//
// So "make gosec run in the container" is only half the requirement. The other
// half is that it can never *quietly* run degraded.
//
// The module cache is the hard part, because filling it needs the network and
// gosec needs the workspace, and "network plus workspace in one container" is
// precisely the combination the scanner hardening forbids — it is an
// exfiltration path out of a hostile repo.
//
// The split used here is the one this codebase already uses for `update-db`:
// two phases, where the phase with network does no analysis and the phase that
// analyzes has no network.
//
//	phase 1 (warm)     --network bridge, workspace mounted READ-ONLY,
//	                   `go mod download` into a persistent cache volume.
//	                   Downloads modules; does not execute them, does not
//	                   analyze, and cannot write to the workspace.
//	phase 2 (analyze)  --network none, workspace mounted as every other scanner
//	                   mounts it, gosec reading the cache phase 1 left behind.
//
// The invariant that keeps this honest is that **phase 1 failing is fatal**. A
// warm phase that quietly gives up hands phase 2 a cold cache, and the table
// above is what a cold cache produces: not an obvious zero but a confident,
// wrong-by-25 report with a whole class of rules missing. Nothing downstream can
// tell that apart from a good run. So warmGoModuleCache returns an error and
// gosecScanner.Scan propagates it rather than scanning anyway — a loud "could
// not fetch modules" beats a clean report nobody can trust.

// goModuleWarmMarker is the file whose presence means "this is a Go module, so
// there is something to download". Its absence is not an error: gosec can be
// pointed at a tree that has no module at all, and it will say so itself far
// more accurately than a preflight guess would.
const goModuleWarmMarker = "go.mod"

// runGoModuleWarm executes phase 1. A var so tests can exercise the two-phase
// sequencing — which run gets network, which gets the workspace read-only, and
// what happens when the warm phase fails — without a container runtime.
var runGoModuleWarm = func(ctx context.Context, rt sandbox.ContainerRuntime, image, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, string(rt), goModuleWarmArgs(rt, image, dir)...)
	return cmd.CombinedOutput()
}

// goModuleWarmArgs builds phase 1's command line, split out from the run so the
// two properties that make it safe — network on, workspace read-only, and no
// analysis command — are unit-testable without invoking a container runtime.
//
// The `:ro` on the workspace mount is the load-bearing part. `go mod download`
// reads go.mod/go.sum and writes only into GOMODCACHE, which lives in the
// mounted volume, so read-only costs nothing and removes the ability of a
// networked container to modify the source tree it can see.
func goModuleWarmArgs(rt sandbox.ContainerRuntime, image, dir string) []string {
	args := []string{"run", "--rm",
		// Network ON. The entire job of this phase, and the reason it does
		// nothing else: it runs one command that fetches modules and does not
		// execute them, then exits.
		"--network", "bridge",
	}
	args = append(args, sandbox.OCIHardeningFlags(rt)...)
	args = append(args,
		"-v", sandbox.HostMountPath(rt, dir)+":/src:ro",
		"-v", MultiscannerGoCacheVolume+":"+multiscannerGoCacheMount,
		"-w", "/src",
		image,
		// `go mod download` with no arguments fetches the modules needed to
		// build the packages in the main module — what `go list ./...` (and so
		// gosec) needs, without `all`'s much larger transitive closure. With
		// GOTOOLCHAIN=auto set in the image, this also downloads a newer
		// toolchain if go.mod asks for one, into the same volume, which is the
		// other thing phase 2 cannot fetch for itself.
		"go", "mod", "download",
	)
	return args
}

// warmGoModuleCache runs phase 1 for dir, or reports why gosec must not be run.
//
// Returns (false, nil) when there is nothing to warm — dir is not a Go module —
// which is not a failure: gosec will produce its own, more accurate complaint
// about a tree with no buildable Go in it.
func warmGoModuleCache(ctx context.Context, rt sandbox.ContainerRuntime, image, dir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(dir, goModuleWarmMarker)); err != nil {
		return false, nil
	}
	out, err := runGoModuleWarm(ctx, rt, image, dir)
	if err != nil {
		// Deliberately fatal to the gosec run rather than a warning. See this
		// file's header: continuing here does not produce an obvious failure, it
		// produces a confident report with every type-aware rule silently
		// missing, which nothing downstream can distinguish from a good one.
		return false, fmt.Errorf("gosec needs the Go module cache warmed before it can load packages, and that step failed — refusing to run gosec against a cold cache, because it would not fail either: it would drop every type-aware rule (measured on this repo: 258 findings instead of 283, with G115/G118/G124/G702 gone entirely) and still exit clean (%w)%s",
			err, formatGoWarmOutput(out))
	}
	return true, nil
}

// formatGoWarmOutput appends the warm phase's own output to an error, trimmed
// to the tail. `go mod download` is verbose on success and its actual
// diagnosis ("missing go.sum entry", a proxy that can't be reached) is the last
// thing it prints, so the tail is the part worth carrying.
func formatGoWarmOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	const keep = 10
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	return ":\n" + strings.Join(lines, "\n")
}
