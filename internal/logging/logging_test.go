package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		" debug ": slog.LevelDebug,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"bogus":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestNewWritesToFile verifies that with a Path and ToStderr unset, log
// records land in the file and nowhere else observable via the returned
// logger.
func TestNewWritesToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.log")
	logger, closer, err := New(Options{Level: "info", Path: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("hello", "k", "v")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("log file = %q, want it to contain \"hello\"", data)
	}
}

// TestNewAppendsAcrossCalls verifies re-opening the same path (e.g. daemon
// restart) appends rather than truncating prior log history.
func TestNewAppendsAcrossCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.log")

	logger1, closer1, err := New(Options{Path: path})
	if err != nil {
		t.Fatalf("New (1st): %v", err)
	}
	logger1.Info("first-line")
	if err := closer1.Close(); err != nil {
		t.Fatalf("Close (1st): %v", err)
	}

	logger2, closer2, err := New(Options{Path: path})
	if err != nil {
		t.Fatalf("New (2nd): %v", err)
	}
	logger2.Info("second-line")
	if err := closer2.Close(); err != nil {
		t.Fatalf("Close (2nd): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "first-line") || !strings.Contains(string(data), "second-line") {
		t.Errorf("expected both log lines present, got: %q", data)
	}
}

// TestNewToStderrMirrorsFile verifies ToStderr with a Path set still writes
// to the log file (mirroring, not replacing) — this is what `aegis serve
// --foreground` relies on to keep a durable log even while also printing to
// the terminal.
func TestNewToStderrMirrorsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.log")
	logger, closer, err := New(Options{Path: path, ToStderr: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("mirrored-line")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "mirrored-line") {
		t.Errorf("expected ToStderr to still write the log file, got: %q", data)
	}
}

// TestNewDefaultsToStderrWithoutPath verifies an empty Path doesn't error and
// returns a usable no-op closer (the CLI's default before a data dir exists).
func TestNewDefaultsToStderrWithoutPath(t *testing.T) {
	logger, closer, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if logger == nil {
		t.Fatal("expected a non-nil logger")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestNewInvalidPathErrors verifies an unwritable path surfaces an error
// instead of silently falling back to stderr.
func TestNewInvalidPathErrors(t *testing.T) {
	// A path inside a non-existent directory can't be opened with O_CREATE.
	badPath := filepath.Join(t.TempDir(), "does-not-exist", "aegis.log")
	_, _, err := New(Options{Path: badPath})
	if err == nil {
		t.Fatal("expected an error for an unwritable path")
	}
}
