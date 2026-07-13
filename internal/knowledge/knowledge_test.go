package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/embed"
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

func (f *fakeEmbedder) Model() string { return "fake-test-model" }

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

// TestSemanticRankingIgnoresMismatchedModelVectors is the P9 regression: a
// vector stored under a different embedding model (e.g. left over from
// before an embedder swap) must not be compared as if it shared the current
// model's vector space, even if it happens to have the same dimensionality —
// Cosine's length check can't detect that case, so the model column must
// gate it instead.
func TestSemanticRankingIgnoresMismatchedModelVectors(t *testing.T) {
	dir := t.TempDir()
	embedder := &fakeEmbedder{synonymGroups: [][]string{{"credentials", "secrets"}}}
	s, err := Open(dir, filepath.Join(dir, "knowledge.db"), embedder)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// A vector planted directly under a different model name, simulating a
	// stale row left over from before an embedder swap. Its content would
	// otherwise rank first for this query via the fake embedder's synonym
	// grouping.
	vecs, err := embedder.Embed(ctx, []string{"secrets secrets secrets"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO docs_vec (path, vector, model) VALUES (?, ?, ?)`,
		"docs/stale.md", embed.EncodeVector(vecs[0]), "some-other-model"); err != nil {
		t.Fatalf("plant stale vector: %v", err)
	}

	ranking, _, err := s.semanticRanking(ctx, "secrets", 5)
	if err != nil {
		t.Fatalf("semanticRanking: %v", err)
	}
	for _, p := range ranking {
		if p == "docs/stale.md" {
			t.Errorf("mismatched-model vector should be excluded from semantic ranking, got %v", ranking)
		}
	}
}

// TestOpenAppliesPermissionHardening exercises the FIND-18/P27.10
// hardenDBPermissions call Open makes on the knowledge database file and its
// WAL-mode sidecars (-wal, -shm), mirroring
// internal/session.TestOpenAppliesPermissionHardening. On POSIX this is a
// no-op (fsguard's RestrictToOwner returns nil immediately there), so the
// main assertion is simply that Open still succeeds and the store is usable
// — a regression where the hardening call errors out (including on a
// sidecar that hasn't been created yet) must not break every knowledge
// store open.
func TestOpenAppliesPermissionHardening(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "hardened-knowledge.db")

	s, err := Open(dir, dbPath, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.upsert(context.Background(), "docs/hardening.md", "Hardening", "exists"); err != nil {
		t.Fatalf("upsert after Open: %v", err)
	}

	// Re-open the same path: hardenDBPermissions runs again and must not
	// error even though the -wal/-shm sidecars now exist from the first
	// open's writes above.
	s2, err := Open(dir, dbPath, nil)
	if err != nil {
		t.Fatalf("reopen after hardening: %v", err)
	}
	s2.Close()
}
