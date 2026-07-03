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
	p := seatbeltProfile("/Users/x/proj", true)
	if !strings.Contains(p, `(subpath "/Users/x/proj")`) {
		t.Errorf("profile missing workspace subpath:\n%s", p)
	}
	if !strings.Contains(p, "(deny network*)") {
		t.Error("expected network deny when denyNet=true")
	}
	if strings.Contains(seatbeltProfile("/x", false), "(deny network*)") {
		t.Error("did not expect network deny when denyNet=false")
	}
}

func TestBwrapArgs(t *testing.T) {
	args := bwrapArgs("/home/x/proj", "/home/x/proj/sub", true)
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
}

// TestOSBackendConfinesWrites is an integration check that only runs when an OS
// sandbox mechanism is actually present on the host.
func TestOSBackendConfinesWrites(t *testing.T) {
	if _, _, ok := detectOSSandbox(); !ok {
		t.Skip("no OS sandbox mechanism on this host")
	}
	ws := t.TempDir()
	be, err := NewOSBackend(ws, false, nil)
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
