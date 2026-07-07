package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWinSingleQuoteEscape(t *testing.T) {
	cases := map[string]string{
		"C:\\Temp\\foo.png": "C:\\Temp\\foo.png",
		"it's a path.png":   "it''s a path.png",
		"''already''":       "''''already''''",
	}
	for in, want := range cases {
		if got := winSingleQuoteEscape(in); got != want {
			t.Errorf("winSingleQuoteEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTempImagePath(t *testing.T) {
	path, err := tempImagePath()
	if err != nil {
		t.Fatalf("tempImagePath: %v", err)
	}
	defer os.Remove(path)

	if filepath.Ext(path) != ".png" {
		t.Errorf("expected .png extension, got %q", path)
	}
	if !strings.Contains(filepath.Base(path), "aegis-paste-") {
		t.Errorf("expected aegis-paste- prefix, got %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}

	path2, err := tempImagePath()
	if err != nil {
		t.Fatalf("tempImagePath (2nd): %v", err)
	}
	defer os.Remove(path2)
	if path == path2 {
		t.Error("expected distinct temp paths across calls")
	}
}

func TestCommandExists(t *testing.T) {
	if commandExists("aegis-definitely-not-a-real-binary-xyz") {
		t.Error("expected false for a nonexistent binary")
	}
}
