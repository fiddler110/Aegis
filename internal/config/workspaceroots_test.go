package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/workspacetrust"
)

// trustDir records a trust decision for dir in the store ResolveAdditionalRoots
// consults, the way `aegis trust --dir` does.
func trustDir(t *testing.T, dir string) {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	if err := workspacetrust.Open(WorkspaceTrustStorePath()).Trust(real); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// TestResolveAdditionalRootsAlwaysYieldsWritablePrimary pins the invariant the
// callers depend on: index 0 is the session workdir and is writable, whatever
// the config says. Without it, a session with a broken config would silently
// have no root at all.
func TestResolveAdditionalRootsAlwaysYieldsWritablePrimary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	primary := mkdir(t, base, "work")

	roots, rejected := ResolveAdditionalRoots(primary, WorkspaceConfig{})
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	if roots[0].Path != primary || !roots[0].Writable {
		t.Errorf("primary root = %+v, want {%s true}", roots[0], primary)
	}
	if len(rejected) != 0 {
		t.Errorf("unexpected rejections: %v", rejected)
	}
}

// TestResolveAdditionalRootsRequiresPerRootTrust is the item's stated design
// point: an additional root does not inherit the primary workspace's trust.
// Trusting the repo you are working in is not the same decision as granting
// that repo a window into another directory on the host.
func TestResolveAdditionalRootsRequiresPerRootTrust(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	primary := mkdir(t, base, "work")
	research := mkdir(t, base, "research")
	trustDir(t, primary) // the primary being trusted must not carry the root

	cfg := WorkspaceConfig{AdditionalRoots: []AdditionalRoot{{Path: research}}}

	roots, rejected := ResolveAdditionalRoots(primary, cfg)
	if len(roots) != 1 {
		t.Fatalf("untrusted root was admitted: %+v", roots)
	}
	if len(rejected) != 1 || !strings.Contains(rejected[0], "not trusted") {
		t.Fatalf("rejections = %v, want one 'not trusted'", rejected)
	}

	trustDir(t, research)
	roots, rejected = ResolveAdditionalRoots(primary, cfg)
	if len(roots) != 2 {
		t.Fatalf("trusted root not admitted: %+v (rejected %v)", roots, rejected)
	}
	if roots[1].Path != research {
		t.Errorf("additional root = %q, want %q", roots[1].Path, research)
	}
	if roots[1].Writable {
		t.Error("additional root defaulted to writable; read-only is the documented default")
	}
}

// TestResolveAdditionalRootsHonorsWritable checks the opt-in half of the
// read-only default.
func TestResolveAdditionalRootsHonorsWritable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	primary := mkdir(t, base, "work")
	out := mkdir(t, base, "out")
	trustDir(t, out)

	roots, _ := ResolveAdditionalRoots(primary, WorkspaceConfig{
		AdditionalRoots: []AdditionalRoot{{Path: out, Writable: true}},
	})
	if len(roots) != 2 || !roots[1].Writable {
		t.Fatalf("roots = %+v, want a writable additional root", roots)
	}
}

// TestResolveAdditionalRootsRejectsUnusableEntries covers the three ways an
// entry is dropped rather than silently doing nothing: it doesn't exist, it is
// already inside the workspace, or it is a duplicate. Each returns a reason so
// the daemon can log something an operator can act on.
func TestResolveAdditionalRootsRejectsUnusableEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	primary := mkdir(t, base, "work")
	nested := mkdir(t, primary, "sub")
	dup := mkdir(t, base, "dup")
	trustDir(t, nested)
	trustDir(t, dup)

	roots, rejected := ResolveAdditionalRoots(primary, WorkspaceConfig{
		AdditionalRoots: []AdditionalRoot{
			{Path: filepath.Join(base, "missing")},
			{Path: nested},
			{Path: dup},
			{Path: dup},
		},
	})
	if len(roots) != 2 {
		t.Fatalf("roots = %+v, want primary + dup only", roots)
	}
	if len(rejected) != 3 {
		t.Fatalf("rejected = %v, want 3 entries", rejected)
	}
	joined := strings.Join(rejected, "\n")
	for _, want := range []string{"not an existing directory", "already reachable", "already configured"} {
		if !strings.Contains(joined, want) {
			t.Errorf("rejections %v missing %q", rejected, want)
		}
	}
}

// TestResolveAdditionalRootsResolvesRelativeToPrimary makes the ergonomic case
// work: `../research` in a project config means what it looks like.
func TestResolveAdditionalRootsResolvesRelativeToPrimary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	primary := mkdir(t, base, "work")
	research := mkdir(t, base, "research")
	trustDir(t, research)

	roots, rejected := ResolveAdditionalRoots(primary, WorkspaceConfig{
		AdditionalRoots: []AdditionalRoot{{Path: "../research"}},
	})
	if len(roots) != 2 {
		t.Fatalf("roots = %+v (rejected %v)", roots, rejected)
	}
	if roots[1].Path != research {
		t.Errorf("relative root resolved to %q, want %q", roots[1].Path, research)
	}
}
