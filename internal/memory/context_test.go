package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadContext(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("agent instructions here"), 0o644)
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("claude guidelines"), 0o644)

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}
	ctx := src.LoadContext()
	if !strings.Contains(ctx, "AGENTS.md") {
		t.Error("expected AGENTS.md header")
	}
	if !strings.Contains(ctx, "agent instructions here") {
		t.Error("expected AGENTS.md content")
	}
	if !strings.Contains(ctx, "claude guidelines") {
		t.Error("expected CLAUDE.md content")
	}
}

func TestLoadContextNoFiles(t *testing.T) {
	src := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	if ctx := src.LoadContext(); ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}
}

func TestLoadIgnorePatterns(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".agentharnessignore"), []byte(
		"# comment\n*.log\nnode_modules\n\n*.tmp\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}
	patterns := src.LoadIgnorePatterns()
	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d: %v", len(patterns), patterns)
	}
	if patterns[0] != "*.log" || patterns[1] != "node_modules" || patterns[2] != "*.tmp" {
		t.Errorf("unexpected patterns: %v", patterns)
	}
}

func TestLoadIgnorePatternsNoFile(t *testing.T) {
	src := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	if patterns := src.LoadIgnorePatterns(); len(patterns) != 0 {
		t.Errorf("expected 0 patterns, got %d", len(patterns))
	}
}

func TestShouldIgnore(t *testing.T) {
	patterns := []string{"*.log", "node_modules", "*.tmp"}

	tests := []struct {
		path   string
		ignore bool
	}{
		{"app.log", true},
		{"node_modules", true},
		{"temp.tmp", true},
		{"main.go", false},
		{"src/app.ts", false},
	}
	for _, tc := range tests {
		if got := ShouldIgnore(tc.path, patterns); got != tc.ignore {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", tc.path, got, tc.ignore)
		}
	}
}
