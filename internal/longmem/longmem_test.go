package longmem

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEmbedder mirrors internal/knowledge's test double: a one-hot vector
// over synonym groups, standing in for a real embedding model's semantic
// understanding without requiring a live Ollama server.
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

func TestSearchMemoryBM25Only(t *testing.T) {
	dir := t.TempDir()
	s, err := Open("proj", filepath.Join(dir, "longmem.db"), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.UpsertEntity(ctx, "proj", "system", "billing-api", "handles Stripe webhooks"); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	if err := s.UpsertEntity(ctx, "proj", "system", "auth-service", "issues JWTs"); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	results, err := s.SearchMemory(ctx, "Stripe", 5)
	if err != nil {
		t.Fatalf("SearchMemory: %v", err)
	}
	if len(results) != 1 || results[0].Key != "system:billing-api@proj" {
		t.Fatalf("got %+v, want single match on system:billing-api@proj", results)
	}
}

func TestSearchMemoryHybridSemanticFallback(t *testing.T) {
	dir := t.TempDir()
	embedder := &fakeEmbedder{synonymGroups: [][]string{{"stripe", "payments"}}}
	s, err := Open("proj", filepath.Join(dir, "longmem.db"), embedder)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.UpsertEntity(ctx, "proj", "system", "billing-api", "handles Stripe webhooks"); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	if err := s.UpsertEntity(ctx, "proj", "system", "auth-service", "issues JWTs"); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	// "payments" never appears literally in the stored facts, so BM25 alone
	// would return nothing; the fake embedder's synonym group should still
	// rank billing-api first via the semantic ranking.
	results, err := s.SearchMemory(ctx, "payments", 5)
	if err != nil {
		t.Fatalf("SearchMemory: %v", err)
	}
	if len(results) == 0 || results[0].Key != "system:billing-api@proj" {
		t.Fatalf("got %+v, want system:billing-api@proj ranked first via semantic fusion", results)
	}
}
