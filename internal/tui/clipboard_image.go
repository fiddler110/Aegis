package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// pasteClipboardImage reads an image from the OS clipboard and saves it to a
// new temp PNG file, returning its path (P16.8). ok is false with a nil err
// when the clipboard simply holds no image — not a failure, just nothing to
// attach. The per-OS treatment mirrors copyToClipboard's: native APIs on
// Windows, an external tool on macOS/Linux since neither ships a stdlib path
// to clipboard image data.
func pasteClipboardImage() (path string, ok bool, err error) {
	switch runtime.GOOS {
	case "windows":
		return pasteClipboardImageWindows()
	case "darwin":
		return pasteClipboardImageDarwin()
	case "linux":
		return pasteClipboardImageLinux()
	default:
		return "", false, fmt.Errorf("clipboard image paste not supported on %s", runtime.GOOS)
	}
}

func pasteClipboardImageWindows() (string, bool, error) {
	path, err := tempImagePath()
	if err != nil {
		return "", false, err
	}
	// Bitmap.Save requires an STA thread; Clipboard access does too.
	script := `Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
if ([System.Windows.Forms.Clipboard]::ContainsImage()) {
    $img = [System.Windows.Forms.Clipboard]::GetImage()
    $img.Save('` + winSingleQuoteEscape(path) + `', [System.Drawing.Imaging.ImageFormat]::Png)
    Write-Output 'AEGIS_OK'
} else {
    Write-Output 'AEGIS_NOIMAGE'
}`
	out, err := exec.Command(sandbox.WindowsShellBinary(), "-NoProfile", "-NonInteractive", "-Sta", "-Command", script).Output()
	if err != nil {
		os.Remove(path)
		return "", false, err
	}
	if !strings.Contains(string(out), "AEGIS_OK") {
		os.Remove(path)
		return "", false, nil
	}
	return path, true, nil
}

func pasteClipboardImageDarwin() (string, bool, error) {
	if _, err := exec.LookPath("pngpaste"); err != nil {
		return "", false, fmt.Errorf("no clipboard image tool found (install pngpaste: brew install pngpaste)")
	}
	path, err := tempImagePath()
	if err != nil {
		return "", false, err
	}
	if err := exec.Command("pngpaste", path).Run(); err != nil {
		os.Remove(path)
		return "", false, nil // clipboard has no image
	}
	return path, true, nil
}

func pasteClipboardImageLinux() (string, bool, error) {
	var cmd *exec.Cmd
	switch {
	case commandExists("wl-paste"):
		cmd = exec.Command("wl-paste", "--no-newline", "--type", "image/png")
	case commandExists("xclip"):
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	default:
		return "", false, fmt.Errorf("no clipboard image tool found (install wl-clipboard or xclip)")
	}
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return "", false, nil // no image on the clipboard
	}
	path, err := tempImagePath()
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return "", false, err
	}
	return path, true, nil
}

func tempImagePath() (string, error) {
	f, err := os.CreateTemp("", "aegis-paste-*.png")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	return path, nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// winSingleQuoteEscape escapes a string for embedding in a PowerShell
// single-quoted literal (doubling any single quote).
func winSingleQuoteEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
