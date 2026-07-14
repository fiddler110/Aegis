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

	results, err := s.SearchMemory(ctx, "Stripe", "", 5)
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
	results, err := s.SearchMemory(ctx, "payments", "", 5)
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

	ranking, _, err := s.semanticRanking(ctx, "payments", "", 5)
	if err != nil {
		t.Fatalf("semanticRanking: %v", err)
	}
	for _, k := range ranking {
		if k == "entity:system:stale-entity@proj" {
			t.Errorf("mismatched-model vector should be excluded from semantic ranking, got %v", ranking)
		}
	}
}

// TestSearchMemoryProjectScoping is the P25.9 regression: the store is one
// shared file across every project a daemon has ever been pointed at (see
// Open's doc comment), so a project-scoped search must not leak another
// project's facts/entities, while an unscoped ("") search still sees
// everything (today's pre-P25.9 behavior, kept for any caller that wants
// cross-project recall).
func TestSearchMemoryProjectScoping(t *testing.T) {
	dir := t.TempDir()
	s, err := Open("unused", filepath.Join(dir, "longmem.db"), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.UpsertEntity(ctx, "projectA", "system", "billing-api", "handles Stripe webhooks"); err != nil {
		t.Fatalf("UpsertEntity projectA: %v", err)
	}
	if err := s.UpsertEntity(ctx, "projectB", "system", "billing-api", "handles PayPal webhooks"); err != nil {
		t.Fatalf("UpsertEntity projectB: %v", err)
	}
	if err := s.AddSession(ctx, "sess-a", "projectA", "discussed Stripe billing-api"); err != nil {
		t.Fatalf("AddSession projectA: %v", err)
	}
	if err := s.AddSession(ctx, "sess-b", "projectB", "discussed PayPal billing-api"); err != nil {
		t.Fatalf("AddSession projectB: %v", err)
	}

	resultsA, err := s.SearchMemory(ctx, "billing-api", "projectA", 10)
	if err != nil {
		t.Fatalf("SearchMemory projectA: %v", err)
	}
	for _, r := range resultsA {
		if !strings.HasSuffix(r.Key, "@projectA") && !strings.HasSuffix(r.Key, ":projectA") {
			t.Errorf("projectA-scoped search leaked result %+v", r)
		}
	}
	if len(resultsA) != 2 {
		t.Fatalf("got %d projectA results, want 2 (one entity, one session fact)", len(resultsA))
	}

	resultsB, err := s.SearchMemory(ctx, "billing-api", "projectB", 10)
	if err != nil {
		t.Fatalf("SearchMemory projectB: %v", err)
	}
	if len(resultsB) != 2 {
		t.Fatalf("got %d projectB results, want 2", len(resultsB))
	}

	all, err := s.SearchMemory(ctx, "billing-api", "", 10)
	if err != nil {
		t.Fatalf("SearchMemory unscoped: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("got %d unscoped results, want 4 (both projects)", len(all))
	}
}

// TestOpenAppliesPermissionHardening exercises the FIND-18/P27.10
// hardenDBPermissions call Open makes on the long-term memory database file
// and its WAL-mode sidecars (-wal, -shm), mirroring
// internal/session.TestOpenAppliesPermissionHardening. On POSIX this is a
// no-op (fsguard's RestrictToOwner returns nil immediately there), so the
// main assertion is simply that Open still succeeds and the store is usable
// — a regression where the hardening call errors out (including on a
// sidecar that hasn't been created yet) must not break every longmem open.
func TestOpenAppliesPermissionHardening(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hardened-longmem.db")

	s, err := Open("proj", dbPath, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.UpsertEntity(context.Background(), "proj", "system", "hardening-check", "exists"); err != nil {
		t.Fatalf("UpsertEntity after Open: %v", err)
	}

	// Re-open the same path: hardenDBPermissions runs again and must not
	// error even though the -wal/-shm sidecars now exist from the first
	// open's writes above.
	s2, err := Open("proj", dbPath, nil)
	if err != nil {
		t.Fatalf("reopen after hardening: %v", err)
	}
	s2.Close()
}
