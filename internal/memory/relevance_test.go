package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRelevantScoresHigherForMatching(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- The database uses PostgreSQL for persistence\n"+
			"- The frontend is built with React and TypeScript\n"+
			"- CI runs on GitHub Actions\n"+
			"- Security scans use Opengrep for static analysis\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("database migration PostgreSQL", 2, 0)

	if len(entries) == 0 {
		t.Fatal("expected entries")
	}
	// The database/PostgreSQL entry should score highest.
	if entries[0].Score <= 0 {
		t.Error("expected positive score for top entry")
	}
	if len(entries) > 1 && entries[0].Score < entries[1].Score {
		t.Error("top entry should have highest score")
	}
}

// TestLoadRelevantCacheInvalidatesOnFileChange verifies the P8.5 relevance
// cache (only active on a NewSources-constructed Sources, which carries a
// cache pointer) picks up edits to the underlying memory file rather than
// serving a stale snapshot forever, and that scoring a query doesn't corrupt
// the cached entries for a subsequent, differently-scored query.
func TestLoadRelevantCacheInvalidatesOnFileChange(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	memPath := filepath.Join(memDir, "memory.md")
	os.WriteFile(memPath, []byte("- The database uses PostgreSQL\n"), 0o644)

	src := NewSources(root, dataDir)

	first := src.LoadRelevant("database", 5, 0)
	if len(first) != 1 {
		t.Fatalf("got %d entries, want 1", len(first))
	}

	// A second call with a different query must not have its scores polluted
	// by the first call's in-place mutation of any shared cached slice.
	second := src.LoadRelevant("nonexistent-term-xyz", 5, 0)
	if len(second) != 1 {
		t.Fatalf("got %d entries, want 1", len(second))
	}
	if second[0].Score != 0 {
		t.Errorf("query with no matching terms should score 0, got %v (cache corruption?)", second[0].Score)
	}
	// Re-run the original query to confirm its score wasn't corrupted either.
	firstAgain := src.LoadRelevant("database", 5, 0)
	if firstAgain[0].Score != first[0].Score {
		t.Errorf("score changed across calls: %v -> %v", first[0].Score, firstAgain[0].Score)
	}

	// Editing the file (mtime/size change) must be reflected on the next call.
	os.WriteFile(memPath, []byte("- The database uses PostgreSQL\n- A brand new entry about widgets\n"), 0o644)
	updated := src.LoadRelevant("widgets", 5, 0)
	if len(updated) != 2 {
		t.Fatalf("after file edit, got %d entries, want 2 (cache not invalidated?)", len(updated))
	}
}

func TestLoadRelevantRespectsMaxEntries(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- entry one\n- entry two\n- entry three\n- entry four\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("one", 2, 0)
	if len(entries) > 2 {
		t.Errorf("expected at most 2 entries, got %d", len(entries))
	}
}

func TestLoadRelevantMaxTokens(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)

	// Each entry is ~20 chars = ~5 tokens. With maxTokens=8, should get ~1-2.
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- short entry alpha\n- short entry beta\n- short entry gamma\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("alpha", 10, 8)
	if len(entries) > 2 {
		t.Errorf("expected token-limited entries, got %d", len(entries))
	}
}

func TestLoadRelevantEmptyQuery(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte("- one\n- two\n"), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("", 10, 0)
	if len(entries) != 2 {
		t.Errorf("expected all 2 entries for empty query, got %d", len(entries))
	}
}

func TestLoadRelevantNoMemory(t *testing.T) {
	src := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	entries := src.LoadRelevant("test", 10, 0)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestFormatEntries(t *testing.T) {
	entries := []Entry{
		{Text: "uses PostgreSQL", Source: "Project memory"},
		{Text: "React frontend", Source: "Project memory"},
		{Text: "run tests first", Source: "User memory"},
	}
	out := FormatEntries(entries)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !contains(out, "Project memory") || !contains(out, "User memory") {
		t.Errorf("expected source headers in output: %s", out)
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! Go 1.22")
	expected := []string{"hello", "world", "go", "1", "22"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: got %q, want %q", i, tok, expected[i])
		}
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
