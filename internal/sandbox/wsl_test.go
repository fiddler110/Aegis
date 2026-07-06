package sandbox

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

func withWSLSeams(t *testing.T, list func(ctx context.Context) ([]byte, error), run func(ctx context.Context, args ...string) ([]byte, error)) {
	t.Helper()
	origList, origRun := wslListDistros, wslRun
	if list != nil {
		wslListDistros = list
	}
	if run != nil {
		wslRun = run
	}
	t.Cleanup(func() {
		wslListDistros = origList
		wslRun = origRun
	})
}

func TestWSLDistroAvailableOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this checks the non-windows short-circuit")
	}
	if WSLDistroAvailable(context.Background()) {
		t.Fatal("expected false off windows regardless of seams")
	}
}

func TestDecodeWSLText(t *testing.T) {
	// wsl.exe emits UTF-16LE on a redirected stdout: each ASCII byte is
	// followed by a NUL byte.
	raw := []byte("U\x00b\x00u\x00n\x00t\x00u\x00\n\x00")
	got := decodeWSLText(raw)
	if got != "Ubuntu\n" {
		t.Fatalf("decodeWSLText = %q, want %q", got, "Ubuntu\n")
	}
}

func TestBashQuote(t *testing.T) {
	cases := map[string]string{
		"opengrep":       `'opengrep'`,
		"it's-a-test":    `'it'\''s-a-test'`,
		"has space here": `'has space here'`,
	}
	for in, want := range cases {
		if got := bashQuote(in); got != want {
			t.Errorf("bashQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWSLPathConvertsBackslashesViaForwardSlash(t *testing.T) {
	var gotArgs []string
	withWSLSeams(t, nil, func(_ context.Context, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("/mnt/d/Development/Aegis\n"), nil
	})
	p, err := WSLPath(context.Background(), `D:\Development\Aegis`)
	if err != nil {
		t.Fatal(err)
	}
	if p != "/mnt/d/Development/Aegis" {
		t.Errorf("WSLPath = %q", p)
	}
	if len(gotArgs) != 3 || gotArgs[2] != "D:/Development/Aegis" {
		t.Errorf("wslpath invoked with %v, want forward-slashed path as arg 2", gotArgs)
	}
}

func TestWSLPathEmptyResultIsError(t *testing.T) {
	withWSLSeams(t, nil, func(_ context.Context, args ...string) ([]byte, error) {
		return []byte("  \n"), nil
	})
	if _, err := WSLPath(context.Background(), `C:\x`); err == nil {
		t.Fatal("expected error for empty wslpath result")
	}
}

func TestRunWSLCommandBuildsCdAndQuotedArgv(t *testing.T) {
	var gotScript string
	withWSLSeams(t, nil, func(_ context.Context, args ...string) ([]byte, error) {
		// args = ["wslpath", "-a", ...] on the first call, ["bash", "-lc", script] on the second
		if len(args) >= 1 && args[0] == "wslpath" {
			return []byte("/mnt/d/proj\n"), nil
		}
		if len(args) == 3 && args[0] == "bash" && args[1] == "-lc" {
			gotScript = args[2]
		}
		return []byte("ok"), nil
	})
	out, err := RunWSLCommand(context.Background(), `D:\proj`, "opengrep", "--sarif", ".")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "ok" {
		t.Errorf("out = %q", out)
	}
	want := "cd '/mnt/d/proj' && 'opengrep' '--sarif' '.'"
	if gotScript != want {
		t.Errorf("script = %q, want %q", gotScript, want)
	}
}

func TestRunWSLCommandPropagatesWSLPathError(t *testing.T) {
	withWSLSeams(t, nil, func(_ context.Context, args ...string) ([]byte, error) {
		return nil, errors.New("boom")
	})
	if _, err := RunWSLCommand(context.Background(), `D:\proj`, "opengrep"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWSLInstallCommand(t *testing.T) {
	got := WSLInstallCommand("curl -fsSL https://example.com/install.sh | bash")
	want := "wsl -- bash -lc 'curl -fsSL https://example.com/install.sh | bash'"
	if got != want {
		t.Errorf("WSLInstallCommand = %q, want %q", got, want)
	}
}
