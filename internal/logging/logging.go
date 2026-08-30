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
	"sync"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// Options controls logger construction.
type Options struct {
	Level    string // "debug", "info", "warn", "error"
	Path     string // log file path; empty means stderr
	ToStderr bool   // also mirror logs to stderr (useful for `serve` in foreground)
	// MaxSizeBytes bounds how large Path may grow before it is rotated to
	// Path+".1" and a fresh file is started (GAP-02). <= 0 disables rotation,
	// matching the historical unbounded-append behavior.
	MaxSizeBytes int64
	// MaxBackups is how many rotated files (Path+".1", ".2", ...) are kept;
	// the oldest is deleted once this is exceeded. Ignored when MaxSizeBytes
	// disables rotation.
	MaxBackups int
}

// New builds a *slog.Logger and returns it along with a closer for the
// underlying file (no-op when logging to stderr).
func New(opts Options) (*slog.Logger, io.Closer, error) {
	var w io.Writer = os.Stderr
	var closer io.Closer = nopCloser{}

	if opts.Path != "" {
		var f io.WriteCloser
		var err error
		if opts.MaxSizeBytes > 0 {
			f, err = newRotatingWriter(opts.Path, opts.MaxSizeBytes, opts.MaxBackups)
		} else {
			f, err = os.OpenFile(opts.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			// GAP-3.1: 0o600 alone is cosmetic on Windows (new files inherit
			// the parent directory's ACL) — every other security-sensitive
			// file in this codebase applies this hardening, and log content
			// is not guaranteed metadata-only forever.
			if err == nil {
				_ = fsguard.RestrictToOwner(opts.Path)
			}
		}
		if err != nil {
			return nil, nil, fmt.Errorf("open log file %s: %w", opts.Path, err)
		}
		closer = f
		if opts.ToStderr {
			w = io.MultiWriter(os.Stderr, f)
		} else {
			w = f
		}
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

// rotatingWriter is a size-capped, backup-rotated append writer for a single
// log file. It has no external dependency — CLAUDE.md's build story is
// deliberately container/Node-free, and the rotation logic itself is small
// enough not to warrant one.
type rotatingWriter struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxBackups int
	f          *os.File
	size       int64
}

func newRotatingWriter(path string, maxSize int64, maxBackups int) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	_ = fsguard.RestrictToOwner(path) // GAP-3.1, see New's comment
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &rotatingWriter{path: path, maxSize: maxSize, maxBackups: maxBackups, f: f, size: info.Size()}, nil
}

// Write appends p, rotating first if the file has already reached maxSize —
// so a single Write is never split across the boundary, and a write larger
// than maxSize still lands whole in the fresh file rather than being
// rejected.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotateLocked shifts path.N -> path.N+1 down to maxBackups (dropping
// whatever would fall past it), moves the live file to path.1, and reopens a
// fresh path. Called with w.mu held.
func (w *rotatingWriter) rotateLocked() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	if w.maxBackups > 0 {
		oldest := fmt.Sprintf("%s.%d", w.path, w.maxBackups)
		_ = os.Remove(oldest)
		for n := w.maxBackups - 1; n >= 1; n-- {
			src := fmt.Sprintf("%s.%d", w.path, n)
			dst := fmt.Sprintf("%s.%d", w.path, n+1)
			if _, err := os.Stat(src); err == nil {
				_ = os.Rename(src, dst)
			}
		}
		_ = os.Rename(w.path, w.path+".1")
	} else {
		_ = os.Remove(w.path)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = fsguard.RestrictToOwner(w.path) // GAP-3.1, see New's comment
	w.f = f
	w.size = 0
	return nil
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
