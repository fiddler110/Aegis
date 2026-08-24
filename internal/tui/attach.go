package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
)

// imageRefRe matches @image:<path> tokens. The path is either double-quoted
// (may contain spaces — what pasted Windows/macOS paths often look like) or a
// bare whitespace-free token.
var imageRefRe = regexp.MustCompile(`@image:(?:"([^"]+)"|(\S+))`)

// extractImageRefs pulls @image:<path> tokens out of the submitted text and
// resolves each path (expanding ~ and making it absolute relative to workDir)
// into an image attachment. The remaining text is returned with those tokens
// removed. Paths containing spaces must be double-quoted (the paste handler
// quotes them automatically, TQ9).
func extractImageRefs(text, workDir string) (clean string, images []api.ImageInput) {
	clean = imageRefRe.ReplaceAllStringFunc(text, func(tok string) string {
		sub := imageRefRe.FindStringSubmatch(tok)
		path := sub[1]
		if path == "" {
			path = sub[2]
		}
		images = append(images, api.ImageInput{Path: resolveAttachPath(path, workDir)})
		return ""
	})
	if len(images) == 0 {
		return text, nil
	}
	// Tidy the holes the removed tokens left, preserving intentional newlines.
	lines := strings.Split(clean, "\n")
	for i, ln := range lines {
		lines[i] = strings.Join(strings.Fields(ln), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), images
}

// imageExts are the file extensions the paste handler recognizes as images.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
}

// looksLikeImagePath reports whether pasted text is a single-line path to an
// image file (TQ9): no newlines, an image extension, and no leftover quotes.
func looksLikeImagePath(s string) bool {
	if s == "" || strings.ContainsAny(s, "\n\r\"") {
		return false
	}
	return imageExts[strings.ToLower(filepath.Ext(s))]
}

// attachTokenFor builds the @image: token for a pasted path, quoting it when
// it contains spaces so extractImageRefs can recover it intact.
func attachTokenFor(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `@image:"` + path + `" `
	}
	return "@image:" + path + " "
}

// resolveAttachPath expands a leading ~ and resolves relative paths against the
// workspace directory so the daemon receives an absolute path it can read.
func resolveAttachPath(path, workDir string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if workDir != "" {
		return filepath.Join(workDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// shellRefDefaultLines is how many trailing lines of the terminal pane's most
// recent output an unqualified "@shell" token injects (P13.3.2).
const shellRefDefaultLines = 50

// shellRefRe matches "@shell" and "@shell:N" tokens (N = line count). \b
// anchors the end so it doesn't fire inside a longer word like "@shellac".
var shellRefRe = regexp.MustCompile(`@shell(?::(\d+))?\b`)

// extractShellRefs resolves @shell / @shell:N tokens in text into the last N
// lines of the embedded terminal pane's most recent command + output,
// splicing the resolved text in place of each token (unlike @image:, this is
// a textual injection, not an attachment). A missing terminal run never
// fails submission — it substitutes a short placeholder instead.
func extractShellRefs(text string, term termPane) string {
	if !shellRefRe.MatchString(text) {
		return text
	}
	return shellRefRe.ReplaceAllStringFunc(text, func(tok string) string {
		n := shellRefDefaultLines
		if sub := shellRefRe.FindStringSubmatch(tok); sub[1] != "" {
			if v, err := strconv.Atoi(sub[1]); err == nil && v > 0 {
				n = v
			}
		}
		return shellRefText(term, n)
	})
}

// shellRefText renders the resolved text for a single @shell token, mirroring
// the phrasing of the P13.3.1 diagnose-on-failure prompt (tui.go
// diagnoseLastFailureCmd) so the model sees a consistent framing for
// terminal-pane activity it didn't run as a tool call.
func shellRefText(term termPane, n int) string {
	if term.lastCmd == "" {
		return "(no terminal output yet)"
	}
	out := lastNLines(term.lastOutput, n)
	status := "succeeded"
	if term.lastFailed {
		status = fmt.Sprintf("failed with exit code %d", term.lastExitCode)
	}
	return fmt.Sprintf(
		"The following command (run in the terminal pane, not a tool call) %s:\n\n```\n%s\n```\n\nOutput (last %d lines):\n```\n%s\n```",
		status, term.lastCmd, n, out)
}

// lastNLines returns the trailing n newline-delimited lines of s (fewer if s
// has less), with any trailing blank line from a final "\n" trimmed first.
func lastNLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
