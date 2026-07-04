package longmem

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/embed"
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

func (f *fakeEmbedder) Model() string { return "fake-test-model" }

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

// TestSemanticRankingIgnoresMismatchedModelVectors is the P9 regression: a
// vector stored under a different embedding model (e.g. left over from
// before an embedder swap) must not be compared as if it shared the current
// model's vector space, even if it happens to have the same dimensionality —
// Cosine's length check can't detect that case, so the model column must
// gate it instead.
func TestSemanticRankingIgnoresMismatchedModelVectors(t *testing.T) {
	dir := t.TempDir()
	embedder := &fakeEmbedder{synonymGroups: [][]string{{"stripe", "payments"}}}
	s, err := Open("proj", filepath.Join(dir, "longmem.db"), embedder)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// A vector under the currently configured model.
	if err := s.UpsertEntity(ctx, "proj", "system", "billing-api", "handles Stripe webhooks"); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	// A vector planted directly under a different model name, simulating a
	// stale row left over from before an embedder swap. Its content would
	// otherwise rank first for this query via the fake embedder's synonym
	// grouping.
	vecs, err := embedder.Embed(ctx, []string{"payments payments payments"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO mem_vec (kind, key, vector, model) VALUES (?, ?, ?, ?)`,
		"entity", "system:stale-entity@proj", embed.EncodeVector(vecs[0]), "some-other-model"); err != nil {
		t.Fatalf("plant stale vector: %v", err)
	}

	ranking, _, err := s.semanticRanking(ctx, "payments", 5)
	if err != nil {
		t.Fatalf("semanticRanking: %v", err)
	}
	for _, k := range ranking {
		if k == "entity:system:stale-entity@proj" {
			t.Errorf("mismatched-model vector should be excluded from semantic ranking, got %v", ranking)
		}
	}
}
