// Windows Subsystem for Linux support. This is deliberately a separate
// concept from RuntimeWSL/"wslc" in detect.go: RuntimeWSL is Windows
// Containers running atop WSL2 (a container *runtime*), while this file is
// about the classic WSL feature — a real Linux distro (Ubuntu, Kali, …)
// registered under `wsl.exe`. It exists as a fallback execution environment
// on Windows for tools that ship no native Windows build at all: if the tool
// has a Linux install path and WSL has a distro, it can be installed and run
// there instead of being reported unavailable.
package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// wslListDistros is a seam over `wsl.exe -l -q` so tests can inject a
// canned distro list without a real WSL install.
var wslListDistros = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "wsl.exe", "-l", "-q").Output()
}

// wslRun is a seam over invoking a command inside WSL's default distro so
// tests can inject canned output/errors.
var wslRun = func(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"--"}, args...)
	return exec.CommandContext(ctx, "wsl.exe", full...).Output()
}

// decodeWSLText strips the NUL bytes wsl.exe emits when its stdout is
// redirected (it writes UTF-16LE even though the console is UTF-8) rather
// than pulling in a full UTF-16 decoder — distro names and shell output here
// are always ASCII-range, so dropping the high/low NUL bytes is lossless.
func decodeWSLText(b []byte) string {
	return strings.ReplaceAll(string(b), "\x00", "")
}

// WSLDistroAvailable reports whether at least one Linux distro is registered
// under WSL. Always false off Windows.
func WSLDistroAvailable(ctx context.Context) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return false
	}
	out, err := wslListDistros(ctx)
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(decodeWSLText(out), "\n") {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

// WSLBinaryAvailable reports whether bin resolves on PATH inside WSL's
// default distro.
func WSLBinaryAvailable(ctx context.Context, bin string) bool {
	if !WSLDistroAvailable(ctx) {
		return false
	}
	out, err := wslRun(ctx, "bash", "-lc", "command -v "+bashQuote(bin))
	return err == nil && strings.TrimSpace(decodeWSLText(out)) != ""
}

// WSLPath converts a Windows path to its WSL mount path (e.g.
// `D:\Development\Aegis` -> `/mnt/d/Development/Aegis`) via WSL's own
// wslpath utility. The path is forward-slashed first: wsl.exe strips lone
// backslashes from arguments before handing them to the Linux side (a real,
// reproducible quirk — verified against a live WSL install, not
// speculative), so a raw Windows path with backslashes silently corrupts.
// Forward slashes survive untouched and wslpath accepts them equally.
func WSLPath(ctx context.Context, winPath string) (string, error) {
	out, err := wslRun(ctx, "wslpath", "-a", filepath.ToSlash(winPath))
	if err != nil {
		return "", fmt.Errorf("wslpath %s: %w", winPath, err)
	}
	p := strings.TrimSpace(decodeWSLText(out))
	if p == "" {
		return "", fmt.Errorf("wslpath %s: empty result", winPath)
	}
	return p, nil
}

// bashQuote wraps s in single quotes for safe embedding in a bash -c/-lc
// script, escaping any embedded single quote.
func bashQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BashQuoteArgs single-quotes each arg for safe embedding in a bash script
// line (e.g. building the argv of a command run inside WSL).
func BashQuoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = bashQuote(a)
	}
	return out
}

// RunWSLCommand runs bin with args inside WSL's default distro, cd'd into
// dir (a Windows path, translated via WSLPath). Combines stdout+stderr into
// the returned error on failure, mirroring exec.Cmd.Output's convention.
func RunWSLCommand(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	wslDir, err := WSLPath(ctx, dir)
	if err != nil {
		return nil, err
	}
	argv := append([]string{bin}, args...)
	script := "cd " + bashQuote(wslDir) + " && " + strings.Join(BashQuoteArgs(argv), " ")
	out, err := wslRun(ctx, "bash", "-lc", script)
	if len(out) == 0 && err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("wsl run %s: %w\n%s", bin, err, ee.Stderr)
		}
		return nil, fmt.Errorf("wsl run %s: %w", bin, err)
	}
	return out, nil
}

// WSLInstallCommand builds the PowerShell-runnable command line that
// installs a tool inside WSL by running its Linux install command there
// (e.g. `wsl -- bash -lc 'curl ... | bash'`) — used as the Windows guided
// install fallback for tools with no native Windows build (P14.x).
func WSLInstallCommand(linuxCmd string) string {
	return "wsl -- bash -lc " + bashQuote(linuxCmd)
}
