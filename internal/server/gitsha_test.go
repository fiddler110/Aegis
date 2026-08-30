package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// fakeGitBinSrc sleeps well past captureGitSHACmdTimeout and exits
// nonzero. Compiled to a standalone binary rather than shelled out via a
// .bat/.sh wrapper: on Windows a .bat is launched through an implicit
// cmd.exe, and killing that wrapper process does not kill the grandchild it
// spawned, which defeated the timeout this test exists to pin.
const fakeGitBinSrc = `package main

import (
	"os"
	"time"
)

func main() {
	time.Sleep(20 * time.Second)
	os.Exit(1)
}
`

var (
	fakeGitBinOnce sync.Once
	fakeGitBinDir  string
)

// buildFakeGitBinary compiles fakeGitBinSrc once per test binary run and
// returns the directory containing the resulting "git"/"git.exe".
func buildFakeGitBinary(t *testing.T) string {
	t.Helper()
	fakeGitBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "aegis-fakegit")
		if err != nil {
			t.Fatalf("mkdir temp: %v", err)
		}
		srcPath := filepath.Join(dir, "main.go")
		if err := os.WriteFile(srcPath, []byte(fakeGitBinSrc), 0o644); err != nil {
			t.Fatalf("write fake git source: %v", err)
		}
		name := "git"
		if runtime.GOOS == "windows" {
			name = "git.exe"
		}
		out := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", out, srcPath)
		cmd.Env = os.Environ()
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build fake git binary: %v\n%s", err, output)
		}
		fakeGitBinDir = dir
	})
	if fakeGitBinDir == "" {
		t.Fatal("fake git binary was not built")
	}
	return fakeGitBinDir
}

// fakeGitOnPATH prepends the compiled fake git's directory to PATH for the
// duration of the test, so exec.LookPath("git") resolves to it instead of
// any real git on the host. Exercises GAP-2.1: a stuck `git rev-parse`
// (corrupted repo, network-mounted workdir, a hanging hook, a held
// .git/index lock) must not hang captureGitSHA forever.
func fakeGitOnPATH(t *testing.T) {
	t.Helper()
	dir := buildFakeGitBinary(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if p, err := exec.LookPath("git"); err != nil || filepath.Dir(p) != dir {
		t.Fatalf("fake git not resolved from PATH (path=%q err=%v)", p, err)
	}
}

// TestCaptureGitSHA_RespectsTimeout pins GAP-2.1: captureGitSHA used to be
// called with context.Background(), giving exec.CommandContext no deadline
// to enforce, so a hung `git rev-parse` leaked its goroutine and child
// process for the life of the daemon. captureGitSHA must now bound its own
// call (captureGitSHACmdTimeout) regardless of what context the caller
// passes in.
func TestCaptureGitSHA_RespectsTimeout(t *testing.T) {
	fakeGitOnPATH(t)

	start := time.Now()
	sha := captureGitSHA(context.Background(), t.TempDir())
	elapsed := time.Since(start)

	if elapsed > captureGitSHACmdTimeout+5*time.Second {
		t.Fatalf("captureGitSHA did not respect its bound: took %v (limit ~%v)", elapsed, captureGitSHACmdTimeout)
	}
	if sha != "" {
		t.Fatalf("expected empty SHA on timeout, got %q", sha)
	}
}

// TestCaptureGitSHA_RespectsShutdownCancellation pins the other half of
// GAP-2.1's fix: the call site derives its context from the daemon's
// lifetime (s.daemonCtx), so a cancelled daemon context aborts an in-flight
// capture immediately rather than waiting out captureGitSHACmdTimeout.
func TestCaptureGitSHA_RespectsShutdownCancellation(t *testing.T) {
	fakeGitOnPATH(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	sha := captureGitSHA(ctx, t.TempDir())
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("captureGitSHA did not honor caller cancellation: took %v", elapsed)
	}
	if sha != "" {
		t.Fatalf("expected empty SHA on cancellation, got %q", sha)
	}
}
