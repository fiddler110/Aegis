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

// TestNewAppliesPermissionHardening is the cross-platform half of GAP-3.1's
// regression coverage (Windows DACL assertions live in
// logging_windows_test.go, mirroring fsguard's own split): on POSIX,
// fsguard.RestrictToOwner is a no-op, so this only pins that New still opens
// and writes successfully with the hardening call in place.
func TestNewAppliesPermissionHardening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.log")
	logger, closer, err := New(Options{Path: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("hardened")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "hardened") {
		t.Errorf("log file = %q, want it to contain \"hardened\"", data)
	}
}

// TestRotatingWriterRotatesOnSize verifies a write that would push the file
// past MaxSizeBytes rotates first (GAP-02): the prior content lands in
// aegis.log.1 and the new write starts a fresh, short file.
func TestRotatingWriterRotatesOnSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.log")
	w, err := NewRotatingWriter(path, 10, 3)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("0123456789")); err != nil { // exactly at cap, no rotation yet
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := w.Write([]byte("next")); err != nil { // pushes past cap, rotates first
		t.Fatalf("Write 2: %v", err)
	}

	live, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile live: %v", err)
	}
	if string(live) != "next" {
		t.Errorf("live file = %q, want %q", live, "next")
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("ReadFile backup: %v", err)
	}
	if string(backup) != "0123456789" {
		t.Errorf("backup file = %q, want %q", backup, "0123456789")
	}
}

// TestRotatingWriterDropsOldestBackup verifies MaxBackups is enforced: once
// the backup chain is full, the oldest generation is discarded on the next
// rotation rather than growing without bound.
func TestRotatingWriterDropsOldestBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.log")
	w, err := NewRotatingWriter(path, 5, 2)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	// Each write is under the cap alone but forces a rotation against the
	// previous one, so four writes produce three rotations.
	for _, chunk := range []string{"aaaaa", "bbbbb", "ccccc", "ddddd"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write(%q): %v", chunk, err)
		}
	}

	live, _ := os.ReadFile(path)
	b1, _ := os.ReadFile(path + ".1")
	b2, _ := os.ReadFile(path + ".2")
	if string(live) != "ddddd" {
		t.Errorf("live = %q, want %q", live, "ddddd")
	}
	if string(b1) != "ccccc" {
		t.Errorf(".1 = %q, want %q", b1, "ccccc")
	}
	if string(b2) != "bbbbb" {
		t.Errorf(".2 = %q, want %q", b2, "bbbbb")
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("expected no .3 backup (MaxBackups=2), got err=%v", err)
	}
}

// TestNewWithRotationDisabledByDefault verifies MaxSizeBytes <= 0 (the zero
// value) preserves the pre-GAP-02 unbounded-append behavior, so an existing
// caller that doesn't opt in sees no change.
func TestNewWithRotationDisabledByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.log")
	logger, closer, err := New(Options{Path: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info(strings.Repeat("x", 100))
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("expected no rotation without MaxSizeBytes, got err=%v", err)
	}
}
