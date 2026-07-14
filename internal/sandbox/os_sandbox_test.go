package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSeatbeltProfile(t *testing.T) {
	p := seatbeltProfile("/Users/x/proj", "", true, []string{"/usr", "/opt"})
	if !strings.Contains(p, `(subpath "/Users/x/proj")`) {
		t.Errorf("profile missing workspace subpath:\n%s", p)
	}
	if !strings.Contains(p, "(deny network*)") {
		t.Error("expected network deny when denyNet=true")
	}
	if strings.Contains(seatbeltProfile("/x", "", false, nil), "(deny network*)") {
		t.Error("did not expect network deny when denyNet=false")
	}
	if !strings.Contains(p, "(deny file-read*)") {
		t.Error("expected file-read* to be denied by default")
	}
	if !strings.Contains(p, `(subpath "/usr")`) || !strings.Contains(p, `(subpath "/opt")`) {
		t.Errorf("profile missing read-path allowlist entries:\n%s", p)
	}
}

// TestSeatbeltProfileExtraRoot is the P25.9 regression: a session Workdir
// outside the daemon's own workspace must be allow-listed for both read and
// write, and the default-session case (no extra root) must be byte-for-byte
// unaffected.
func TestSeatbeltProfileExtraRoot(t *testing.T) {
	withExtra := seatbeltProfile("/Users/x/proj", "/Users/x/other", false, nil)
	if !strings.Contains(withExtra, `(subpath "/Users/x/other")`) {
		t.Errorf("profile missing extra root subpath:\n%s", withExtra)
	}

	withoutExtra := seatbeltProfile("/Users/x/proj", "", false, nil)
	if strings.Contains(withoutExtra, "/Users/x/other") {
		t.Errorf("empty extraRoot leaked into profile:\n%s", withoutExtra)
	}
	// The default (no extraRoot) case must render identically to before
	// P25.9 — same profile a default session got previously.
	if got, want := withoutExtra, seatbeltProfile("/Users/x/proj", "", false, nil); got != want {
		t.Errorf("default-session profile changed:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestBwrapArgs(t *testing.T) {
	args := bwrapArgs("/home/x/proj", "", "/home/x/proj/sub", true, []string{"/usr", "/lib"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--bind /home/x/proj /home/x/proj") {
		t.Errorf("missing rw bind: %v", args)
	}
	if !strings.Contains(joined, "--unshare-net") {
		t.Errorf("missing net unshare: %v", args)
	}
	if !strings.Contains(joined, "--chdir /home/x/proj/sub") {
		t.Errorf("missing chdir: %v", args)
	}
	if !strings.Contains(joined, "--ro-bind /usr /usr") {
		t.Errorf("missing read-only toolchain bind: %v", args)
	}
	if strings.Contains(joined, "--ro-bind / /") {
		t.Errorf("whole host root should no longer be bound read-only: %v", args)
	}
}

// TestBwrapArgsExtraRoot is the P25.9 regression: a session Workdir outside
// the daemon's own workspace must get its own read-write bind, and omitting
// extraRoot must render identically to before P25.9.
func TestBwrapArgsExtraRoot(t *testing.T) {
	withExtra := bwrapArgs("/home/x/proj", "/home/x/other", "/home/x/other", false, nil)
	if !strings.Contains(strings.Join(withExtra, " "), "--bind /home/x/other /home/x/other") {
		t.Errorf("missing extra-root rw bind: %v", withExtra)
	}

	without := bwrapArgs("/home/x/proj", "", "/home/x/proj", false, nil)
	for _, a := range without {
		if strings.Contains(a, "other") {
			t.Errorf("empty extraRoot leaked into args: %v", without)
		}
	}
}

func TestMergeReadPaths(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "does-not-exist")
	got := mergeReadPaths([]string{tmp}, []string{tmp, missing})
	if len(got) != 1 || got[0] != filepath.Clean(tmp) {
		t.Errorf("expected deduped single existing path, got %v", got)
	}
}

// TestOSBackendConfinesWrites is an integration check that only runs when an OS
// sandbox mechanism is actually present on the host.
func TestOSBackendConfinesWrites(t *testing.T) {
	if _, _, ok := detectOSSandbox(); !ok {
		t.Skip("no OS sandbox mechanism on this host")
	}
	ws := t.TempDir()
	be, err := NewOSBackend(ws, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A write inside the workspace should succeed.
	if _, err := be.Exec(ctx, "echo hello > inside.txt", ExecOpts{Dir: ws}); err != nil {
		t.Fatalf("write inside workspace failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "inside.txt")); err != nil {
		t.Errorf("expected inside.txt to exist: %v", err)
	}

	// A write outside the workspace should be blocked. Use the home directory:
	// it is outside the workspace and (unlike temp dirs) not in the seatbelt
	// allow list, and bwrap makes / read-only on Linux.
	home, err := os.UserHomeDir()
	if err == nil && home != "" && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
		outside := filepath.Join(home, ".aegis-sandbox-should-not-exist-xyz")
		os.Remove(outside)
		be.Exec(ctx, "echo nope > "+outside, ExecOpts{Dir: ws})
		if _, err := os.Stat(outside); err == nil {
			os.Remove(outside)
			t.Errorf("write outside workspace was NOT blocked: %s", outside)
		}
	}
}

// TestOSBackendConfinesWritesToSessionWorkdir is the P25.9 regression: a
// write to ExecOpts.Dir must succeed even when that directory is outside
// the backend's own workspace (mirroring a session whose Workdir differs
// from the daemon's), and a write to some other, still-unlisted directory
// must remain blocked exactly as before.
func TestOSBackendConfinesWritesToSessionWorkdir(t *testing.T) {
	if _, _, ok := detectOSSandbox(); !ok {
		t.Skip("no OS sandbox mechanism on this host")
	}
	ws := t.TempDir()
	sessionDir := t.TempDir() // a separate root, standing in for a session Workdir
	be, err := NewOSBackend(ws, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := be.Exec(ctx, "echo hello > session.txt", ExecOpts{Dir: sessionDir}); err != nil {
		t.Fatalf("write inside session workdir failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "session.txt")); err != nil {
		t.Errorf("expected session.txt to exist in session workdir: %v", err)
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
		outside := filepath.Join(home, ".aegis-sandbox-should-not-exist-session-xyz")
		os.Remove(outside)
		be.Exec(ctx, "echo nope > "+outside, ExecOpts{Dir: sessionDir})
		if _, err := os.Stat(outside); err == nil {
			os.Remove(outside)
			t.Errorf("write to a third, unrelated directory was NOT blocked: %s", outside)
		}
	}
}
