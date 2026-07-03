package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEmbedder returns a deterministic vector per text so tests don't need a
// live Ollama server: the vector is a one-hot encoding of which synonym
// groups appear in the text, letting a query retrieve a document via a
// different word in the same group even with zero literal keyword overlap
// (standing in for what a real embedding model would do semantically).
type fakeEmbedder struct {
	synonymGroups [][]string
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, len(f.synonymGroups))
		lower := strings.ToLower(t)
		for j, group := range f.synonymGroups {
			for _, word := range group {
				if strings.Contains(lower, word) {
					v[j] = 1
					break
				}
			}
		}
		out[i] = v
	}
	return out, nil
}

func TestSearchBM25Only(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, filepath.Join(dir, "knowledge.db"), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.upsert(ctx, "docs/auth.md", "Auth", "authentication and session tokens"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.upsert(ctx, "docs/db.md", "Database", "postgres schema migrations"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	results, err := s.Search(ctx, "authentication", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Path != "docs/auth.md" {
		t.Fatalf("got %+v, want single match on docs/auth.md", results)
	}
}

func TestSearchHybridSemanticFallback(t *testing.T) {
	dir := t.TempDir()
	// The query word "secrets" never appears in the auth doc's text ("credentials
	// rotation policy"), so BM25 alone would miss it entirely. The fake embedder
	// treats "secrets" and "credentials" as the same synonym group, standing in
	// for a real embedding model's semantic understanding, so the RRF-fused
	// ranking should still surface the doc.
	embedder := &fakeEmbedder{synonymGroups: [][]string{{"credentials", "secrets"}}}
	s, err := Open(dir, filepath.Join(dir, "knowledge.db"), embedder)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.upsert(ctx, "docs/auth.md", "Auth", "credentials rotation policy"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.upsert(ctx, "docs/unrelated.md", "Other", "unrelated filler content"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	results, err := s.Search(ctx, "secrets", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Path != "docs/auth.md" {
		t.Fatalf("got %+v, want docs/auth.md surfaced via semantic fusion despite no BM25 keyword match", results)
	}
}
