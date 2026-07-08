// Windows Subsystem for Linux support. This is deliberately a separate
// concept from RuntimeWSL/"wslc" in detect.go: RuntimeWSL is Windows
// Containers running atop WSL2 (a container *runtime*), while this file is
// about the classic WSL feature — a real Linux distro (Ubuntu, Kali, …)
// registered under `wsl.exe`. It exists as a fallback execution environment
// on Windows for tools that ship no native Windows build at all, or whose
// native Windows build is unreliable in practice (e.g. nmap's Npcap/
// admin-rights friction): if the tool has a Linux install and WSL has a
// distro, it can be installed and run there instead.
package sandbox

import (
	"context"
	"fmt"
	"math"
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

// wslRun is a seam over invoking a command inside a WSL distro so tests can
// inject canned output/errors. distro selects a specific registered distro
// (e.g. "kali-linux") via `-d`; empty uses whatever `wsl --set-default`
// currently points at.
var wslRun = func(ctx context.Context, distro string, args ...string) ([]byte, error) {
	if len(args) > math.MaxInt-3 {
		return nil, fmt.Errorf("too many arguments for wsl command")
	}
	full := make([]string, 0, len(args)+3)
	if distro != "" {
		full = append(full, "-d", distro)
	}
	full = append(full, "--")
	full = append(full, args...)
	return exec.CommandContext(ctx, "wsl.exe", full...).Output()
}

// decodeWSLText strips the NUL bytes wsl.exe emits when its stdout is
// redirected (it writes UTF-16LE even though the console is UTF-8) rather
// than pulling in a full UTF-16 decoder — distro names and shell output here
// are always ASCII-range, so dropping the high/low NUL bytes is lossless.
func decodeWSLText(b []byte) string {
	return strings.ReplaceAll(string(b), "\x00", "")
}

// WSLDistroAvailable reports whether WSL has a usable distro. With distro
// empty, it reports whether at least one Linux distro is registered at all.
// With distro set, it reports whether that specific distro is registered
// (case-insensitive) — so a configured target distro (e.g. "kali-linux")
// that isn't actually installed is correctly reported unavailable even when
// some other distro is present. Always false off Windows.
func WSLDistroAvailable(ctx context.Context, distro string) bool {
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
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if distro == "" || strings.EqualFold(name, distro) {
			return true
		}
	}
	return false
}

// WSLBinaryAvailable reports whether bin resolves on PATH inside the named
// WSL distro (empty distro = WSL's own default distro).
func WSLBinaryAvailable(ctx context.Context, bin, distro string) bool {
	if !WSLDistroAvailable(ctx, distro) {
		return false
	}
	out, err := wslRun(ctx, distro, "bash", "-lc", "command -v "+bashQuote(bin))
	return err == nil && strings.TrimSpace(decodeWSLText(out)) != ""
}

// WSLPath converts a Windows path to its WSL mount path (e.g.
// `D:\Development\Aegis` -> `/mnt/d/Development/Aegis`) via WSL's own
// wslpath utility, run inside the named distro (empty = default). The path
// is forward-slashed first: wsl.exe strips lone backslashes from arguments
// before handing them to the Linux side (a real, reproducible quirk —
// verified against a live WSL install, not speculative), so a raw Windows
// path with backslashes silently corrupts. Forward slashes survive untouched
// and wslpath accepts them equally.
func WSLPath(ctx context.Context, winPath, distro string) (string, error) {
	out, err := wslRun(ctx, distro, "wslpath", "-a", filepath.ToSlash(winPath))
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

// RunWSLCommand runs bin with args inside the named WSL distro (empty =
// default), cd'd into dir (a Windows path, translated via WSLPath). Combines
// stdout+stderr into the returned error on failure, mirroring exec.Cmd.
// Output's convention. For callers with no source directory to scope the
// command to (e.g. network recon tools), see RunWSLScript instead.
func RunWSLCommand(ctx context.Context, dir, distro, bin string, args ...string) ([]byte, error) {
	wslDir, err := WSLPath(ctx, dir, distro)
	if err != nil {
		return nil, err
	}
	argv := append([]string{bin}, args...)
	script := "cd " + bashQuote(wslDir) + " && " + strings.Join(BashQuoteArgs(argv), " ")
	out, err := wslRun(ctx, distro, "bash", "-lc", script)
	if len(out) == 0 && err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("wsl run %s: %w\n%s", bin, err, ee.Stderr)
		}
		return nil, fmt.Errorf("wsl run %s: %w", bin, err)
	}
	return out, nil
}

// RunWSLScript runs bin with args inside the named WSL distro (empty =
// default) with no working-directory change first — for callers that have
// no source directory to scope the command to (e.g. recon.go's nmap/nuclei,
// which target a network host, not a local directory), unlike
// RunWSLCommand.
func RunWSLScript(ctx context.Context, distro, bin string, args ...string) ([]byte, error) {
	argv := append([]string{bin}, args...)
	script := strings.Join(BashQuoteArgs(argv), " ")
	out, err := wslRun(ctx, distro, "bash", "-lc", script)
	if len(out) == 0 && err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("wsl run %s: %w\n%s", bin, err, ee.Stderr)
		}
		return nil, fmt.Errorf("wsl run %s: %w", bin, err)
	}
	return out, nil
}

// WSLInstallCommand builds the PowerShell-runnable command line that
// installs a tool inside the named WSL distro (empty = default) by running
// its Linux install command there (e.g. `wsl -- bash -lc 'curl ... | bash'`)
// — used as the Windows guided install fallback for tools with no native
// Windows build (P14.x).
func WSLInstallCommand(linuxCmd, distro string) string {
	if distro == "" {
		return "wsl -- bash -lc " + bashQuote(linuxCmd)
	}
	return "wsl -d " + distro + " -- bash -lc " + bashQuote(linuxCmd)
}
