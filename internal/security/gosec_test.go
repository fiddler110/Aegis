package security

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// stubGoModuleWarm swaps the phase-1 runner and records every invocation.
func stubGoModuleWarm(t *testing.T, out []byte, err error) *[]string {
	t.Helper()
	var calls []string
	prev := runGoModuleWarm
	runGoModuleWarm = func(_ context.Context, rt sandbox.ContainerRuntime, image, dir string) ([]byte, error) {
		calls = append(calls, string(rt)+"|"+image+"|"+dir)
		return out, err
	}
	t.Cleanup(func() { runGoModuleWarm = prev })
	return &calls
}

func goModuleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestGoModuleWarmArgsKeepTheWorkspaceReadOnly is the invariant that makes the
// two-phase split safe rather than merely convenient: phase 1 is the one run
// that has both network and a view of the workspace, so the workspace must be
// read-only and the command must be a fetch, never an analysis.
func TestGoModuleWarmArgsKeepTheWorkspaceReadOnly(t *testing.T) {
	args := goModuleWarmArgs(sandbox.RuntimePodman, "localhost/img:v1", "/work/repo")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--network bridge") {
		t.Errorf("phase 1 must have network — that is its whole job: %v", args)
	}
	sawRO := false
	for i, a := range args {
		if a != "-v" || i+1 >= len(args) {
			continue
		}
		spec := args[i+1]
		if strings.HasSuffix(spec, ":/src") {
			t.Errorf("workspace mounted read-write into a networked container: %q", spec)
		}
		if strings.HasSuffix(spec, ":/src:ro") {
			sawRO = true
		}
	}
	if !sawRO {
		t.Errorf("phase 1 must mount the workspace read-only, got %v", args)
	}
	if !strings.Contains(joined, MultiscannerGoCacheVolume+":"+multiscannerGoCacheMount) {
		t.Errorf("phase 1 must mount the module cache volume it exists to fill: %v", args)
	}
	// The command itself: `go mod download` fetches modules and does not build
	// or run them. Anything that compiles or analyzes belongs in phase 2.
	if got := args[len(args)-3:]; got[0] != "go" || got[1] != "mod" || got[2] != "download" {
		t.Errorf("phase 1 command = %v, want `go mod download`", got)
	}
	if strings.Contains(joined, "gosec") {
		t.Errorf("phase 1 must not run the analyzer: %v", args)
	}
	if !strings.Contains(joined, "--cap-drop=ALL") || !strings.Contains(joined, "--security-opt=no-new-privileges") {
		t.Errorf("phase 1 drops the same privileges every other scanner run does: %v", args)
	}
}

// TestWarmGoModuleCacheFailureIsFatal is the single most important assertion in
// this file. A warm phase that fails and is swallowed hands gosec a cold cache,
// and gosec on a cold cache does not fail — it drops every type-aware rule and
// still exits clean (measured on this repo: 258 findings instead of 283, with
// G115/G118/G124/G702 gone entirely). That is indistinguishable downstream from
// a good run, so the failure must propagate.
func TestWarmGoModuleCacheFailureIsFatal(t *testing.T) {
	dir := goModuleDir(t)
	stubGoModuleWarm(t, []byte("go: missing go.sum entry for module example.com/x"), errors.New("exit status 1"))

	ok, err := warmGoModuleCache(context.Background(), sandbox.RuntimePodman, "img", dir)
	if err == nil {
		t.Fatal("a failed warm phase must be fatal to the gosec run, not a warning")
	}
	if ok {
		t.Error("warm reported success alongside an error")
	}
	if !strings.Contains(err.Error(), "exit clean") {
		t.Errorf("the error must say why running anyway is worse than failing, got %q", err)
	}
	// The tool's own diagnosis is what an operator can act on; losing it would
	// leave "warm phase failed" and nothing else.
	if !strings.Contains(err.Error(), "missing go.sum entry") {
		t.Errorf("warm-phase output should reach the error, got %q", err)
	}
}

// TestWarmGoModuleCacheSkipsNonModules: gosec can legitimately be pointed at a
// tree with no module in it. That is not a warm-phase failure, and reporting it
// as one would replace gosec's own accurate complaint with a worse guess.
func TestWarmGoModuleCacheSkipsNonModules(t *testing.T) {
	calls := stubGoModuleWarm(t, nil, errors.New("should never run"))

	ran, err := warmGoModuleCache(context.Background(), sandbox.RuntimePodman, "img", t.TempDir())
	if err != nil {
		t.Fatalf("a directory with no go.mod is not an error: %v", err)
	}
	if ran {
		t.Error("nothing to warm, so nothing should have been reported as warmed")
	}
	if len(*calls) != 0 {
		t.Errorf("no container should have been started: %v", *calls)
	}
}

func TestWarmGoModuleCacheRunsForAModule(t *testing.T) {
	dir := goModuleDir(t)
	calls := stubGoModuleWarm(t, []byte("ok"), nil)

	ran, err := warmGoModuleCache(context.Background(), sandbox.RuntimePodman, "localhost/img:v1", dir)
	if err != nil || !ran {
		t.Fatalf("warm = %v, %v; want true, nil", ran, err)
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "localhost/img:v1") {
		t.Errorf("warm phase should have run once against the pinned image, got %v", *calls)
	}
}

// TestGosecContainerScanRefusesAfterAFailedWarm covers the wiring, not just the
// helper: the Scanner must not reach its analysis run when phase 1 failed.
func TestGosecContainerScanRefusesAfterAFailedWarm(t *testing.T) {
	dir := goModuleDir(t)
	stubGoModuleWarm(t, nil, errors.New("network unreachable"))

	findings, err := gosecScanner{}.Scan(context.Background(), dir, MethodContainer, sandbox.RuntimePodman, "img", Options{})
	if err == nil {
		t.Fatal("gosec must refuse to scan against a cold module cache")
	}
	if findings != nil {
		t.Errorf("no findings may be reported from a refused run, got %v", findings)
	}
}

// TestGosecAnalysisRunStaysOffline: phase 2 is an ordinary scanner run and must
// keep every property one has — no network, and the module cache from phase 1.
func TestGosecAnalysisRunStaysOffline(t *testing.T) {
	image := "localhost/aegis-multiscanner:v1"
	cliArgs := containerRunArgs(sandbox.RuntimePodman, image, "/work/repo", "gosec", "-fmt=sarif")
	cliArgs = withCacheVolume(cliArgs, image)
	cliArgs = withVolume(cliArgs, image, MultiscannerGoCacheVolume+":"+multiscannerGoCacheMount)
	joined := strings.Join(cliArgs, " ")

	if !strings.Contains(joined, "--network none") {
		t.Errorf("the analysis phase must have no network: %v", cliArgs)
	}
	if !strings.Contains(joined, MultiscannerGoCacheVolume) {
		t.Errorf("the analysis phase must see the cache phase 1 filled: %v", cliArgs)
	}
	// Both mounts have to land before the image reference, or they would be
	// passed to gosec as arguments instead of to the runtime as flags.
	imageAt := -1
	for i, a := range cliArgs {
		if a == image {
			imageAt = i
			break
		}
	}
	if imageAt < 0 {
		t.Fatalf("image reference missing from %v", cliArgs)
	}
	for i, a := range cliArgs[imageAt:] {
		if a == "-v" {
			t.Errorf("mount flag at position %d is after the image reference: %v", imageAt+i, cliArgs)
		}
	}
}

// TestGosecCanaryFixtureIsAModule: gosec's canary only means anything if the
// materialized fixture is a real Go module. Both files are stored under
// .canary in the repo (a nested go.mod would break the embed, and the .go file
// would be compiled into Aegis), so the rename is what makes it work — and a
// silently-missed rename would show up as "gosec: 0 findings", which reads as a
// broken image rather than a broken fixture.
func TestGosecCanaryFixtureIsAModule(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeCanaryFixture(dir); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	for _, name := range []string{"go.mod", "vuln.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("canary fixture is missing %s (the .canary rename did not apply): %v", name, err)
		}
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), canarySuffix) {
				t.Errorf("%s was materialized under its repo name; gosec would not see a module", e.Name())
			}
		}
	}
	// go.mod must be at the fixture root: gosec is invoked with -w /src and
	// ./..., so a module nested one directory down would not be loaded at all.
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || !strings.Contains(string(data), "module ") {
		t.Errorf("canary go.mod is not a module file: %v / %q", err, data)
	}
}

// TestGosecCanaryIsNotCompiledIntoAegis guards the reason for the .canary
// suffix from the other direction: if these ever land as real .go/go.mod files
// they would be built (and scanned) as part of this repository.
func TestGosecCanaryIsNotCompiledIntoAegis(t *testing.T) {
	for _, bad := range []string{"vuln.go", "go.mod"} {
		if _, err := os.Stat(filepath.Join("canary", bad)); err == nil {
			t.Errorf("canary/%s exists under its real name — it would be compiled into Aegis; store it as %s%s", bad, bad, canarySuffix)
		}
	}
}
