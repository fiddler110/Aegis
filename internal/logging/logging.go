// Package logging configures the harness's structured logger.
//
// The TUI owns the terminal, so logs are written to a file inside the data
// directory rather than to stdout/stderr.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options controls logger construction.
type Options struct {
	Level   string // "debug", "info", "warn", "error"
	Path    string // log file path; empty means stderr
	ToStderr bool  // also mirror logs to stderr (useful for `serve` in foreground)
}

// New builds a *slog.Logger and returns it along with a closer for the
// underlying file (no-op when logging to stderr).
func New(opts Options) (*slog.Logger, io.Closer, error) {
	var w io.Writer = os.Stderr
	var closer io.Closer = nopCloser{}

	if opts.Path != "" && !opts.ToStderr {
		f, err := os.OpenFile(opts.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file %s: %w", opts.Path, err)
		}
		w = f
		closer = f
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: parseLevel(opts.Level)})
	return slog.New(handler), closer, nil
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
