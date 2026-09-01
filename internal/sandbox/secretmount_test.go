package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestOCIRunArgsShadowsAegisEnv is the P81.10/FIND-10 regression: a
// container run must shadow .aegis/.env out of the mounted workspace with an
// empty read-only file, rather than leaving it reachable through the bind
// mount.
func TestOCIRunArgsShadowsAegisEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aegis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aegis", ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &ContainerBackend{runtime: RuntimeDocker, image: "img", secretExcludes: defaultSecretExcludes}
	defer c.closeShadowMounts()
	args := c.ociRunArgs("ls", ExecOpts{Dir: dir})

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, ":/workspace/.aegis/.env:ro") {
		t.Fatalf("expected a shadow mount over /workspace/.aegis/.env, got %v", args)
	}
	// The shadow source (the backend's own lazily-created empty file, not the
	// hostPathForMount-converted form that ends up in argv) must not be the
	// real secret file, and must be empty.
	if c.shadowFilePath == "" {
		t.Fatal("expected shadowFilePath to be populated after ociRunArgs")
	}
	if c.shadowFilePath == filepath.Join(dir, ".aegis", ".env") {
		t.Fatalf("shadow source must not be the real secret file, got %q", c.shadowFilePath)
	}
	data, err := os.ReadFile(c.shadowFilePath)
	if err != nil {
		t.Fatalf("shadow source unreadable: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("shadow source must be empty, got %q", data)
	}
}

// TestOCIRunArgsSkipsShadowWhenSecretAbsent confirms no shadow mount is added
// when the excluded path doesn't exist under the workspace — nothing to hide.
func TestOCIRunArgsSkipsShadowWhenSecretAbsent(t *testing.T) {
	dir := t.TempDir()
	c := &ContainerBackend{runtime: RuntimeDocker, image: "img", secretExcludes: defaultSecretExcludes}
	defer c.closeShadowMounts()
	args := c.ociRunArgs("ls", ExecOpts{Dir: dir})
	if strings.Contains(strings.Join(args, " "), ".aegis/.env") {
		t.Errorf("expected no shadow mount when .aegis/.env does not exist, got %v", args)
	}
}

// TestOCIRunArgsReadOnlyMount is the P81.10/FIND-10 regression for the other
// half: a command the capability memo classified as non-mutating gets a
// read-only workspace mount in the one-shot container path.
func TestOCIRunArgsReadOnlyMount(t *testing.T) {
	c := &ContainerBackend{runtime: RuntimeDocker, image: "img"}
	args := c.ociRunArgs("cat foo", ExecOpts{Dir: "/work", ReadOnly: true})
	if !slices.Contains(args, "/work:/workspace:ro") && !slices.Contains(args, hostPathForMount("/work")+":/workspace:ro") {
		t.Errorf("expected a read-only workspace mount, got %v", args)
	}

	rw := c.ociRunArgs("rm foo", ExecOpts{Dir: "/work"})
	joined := strings.Join(rw, " ")
	if strings.Contains(joined, ":/workspace:ro") {
		t.Errorf("expected a read-write mount by default, got %v", rw)
	}
}

// TestStartPersistentArgsStaysReadWrite confirms the persistent-container
// mount never varies per call — its mount is fixed at container start and
// reused across `exec` regardless of the capability verdict of whatever
// command happens to run next.
func TestStartPersistentArgsStaysReadWrite(t *testing.T) {
	c := &ContainerBackend{runtime: RuntimeDocker, image: "img"}
	args := c.startPersistentArgs("/work")
	if strings.Contains(strings.Join(args, " "), ":/workspace:ro") {
		t.Errorf("persistent container mount must stay read-write, got %v", args)
	}
}
