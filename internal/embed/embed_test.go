package embed

import (
	"math"
	"testing"
)

func TestCosine(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{1, 0}); math.Abs(got-1) > 1e-9 {
		t.Errorf("identical vectors: got %v, want 1", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal vectors: got %v, want 0", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{-1, 0}); math.Abs(got+1) > 1e-9 {
		t.Errorf("opposite vectors: got %v, want -1", got)
	}
	if got := Cosine(nil, []float32{1}); got != 0 {
		t.Errorf("empty vector: got %v, want 0", got)
	}
	if got := Cosine([]float32{1, 2}, []float32{1}); got != 0 {
		t.Errorf("mismatched length: got %v, want 0", got)
	}
}

func TestVectorRoundtrip(t *testing.T) {
	v := []float32{0.5, -1.25, 3.0, 0}
	got := DecodeVector(EncodeVector(v))
	if len(got) != len(v) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(v))
	}
	for i := range v {
		if got[i] != v[i] {
			t.Errorf("index %d: got %v, want %v", i, got[i], v[i])
		}
	}
}

func TestRRF(t *testing.T) {
	bm25 := []string{"a", "b", "c"}
	semantic := []string{"b", "a", "d"}
	scores := RRF(60, bm25, semantic)

	if len(scores) != 4 {
		t.Fatalf("got %d scored keys, want 4", len(scores))
	}
	// "a" and "b" appear in both lists at ranks {1,2} and {2,1}, so they should
	// tie and both outrank "c" and "d", which appear in only one list.
	if scores["a"] != scores["b"] {
		t.Errorf("a=%v b=%v, expected a tie (both rank 1+2 across lists)", scores["a"], scores["b"])
	}
	if scores["a"] <= scores["c"] {
		t.Errorf("a=%v should outrank c=%v (a appears in both lists)", scores["a"], scores["c"])
	}
	if scores["c"] != scores["d"] {
		t.Errorf("c=%v d=%v, expected a tie (both rank 3 in one list)", scores["c"], scores["d"])
	}
}

func TestRRFDefaultK(t *testing.T) {
	scores := RRF(0, []string{"x"})
	if math.Abs(scores["x"]-1.0/61.0) > 1e-9 {
		t.Errorf("got %v, want 1/61 (default k=60)", scores["x"])
	}
}
