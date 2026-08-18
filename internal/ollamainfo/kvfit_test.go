package ollamainfo

import (
	"encoding/json"
	"strings"
	"testing"
)

// modelInfo builds a model_info map from a literal, mirroring the shape
// /api/show returns.
func modelInfo(t *testing.T, pairs map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := make(map[string]json.RawMessage, len(pairs))
	for k, v := range pairs {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %s: %v", k, err)
		}
		out[k] = raw
	}
	return out
}

// gib converts a gibibyte figure to bytes. Written as a function rather than
// an inline expression because Go rejects converting an untyped float constant
// to int64 directly.
func gib(f float64) int64 { return int64(f * float64(int64(1)<<30)) }

// The calibration anchor: this geometry and this answer were measured against
// a live Ollama, not derived. aegis-qwen35-9b loaded at 16000 tokens reports
// 6.01 GiB resident; the formula predicts 2.01 GiB of KV against 4.00 GiB of
// weights. If BytesPerToken ever stops returning 132 KiB/token for this shape,
// every fitted window on a qwen35 model moves with it.
func TestBytesPerTokenMatchesTheMeasuredQwen35(t *testing.T) {
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256, ContextMax: 262144}
	got, ok := g.BytesPerToken(KVTypeF16)
	if !ok {
		t.Fatal("BytesPerToken not ok for a complete geometry")
	}
	if want := int64(135168); got != want {
		t.Errorf("bytes/token = %d, want %d (132 KiB)", got, want)
	}
	kv, _ := KVBytes(g, 16000, KVTypeF16)
	if gib := float64(kv) / (1 << 30); gib < 2.00 || gib > 2.02 {
		t.Errorf("KV at 16000 = %.2f GiB, want ~2.01", gib)
	}
}

// Block-quantized KV is not a clean power of two per element: q8_0 stores 32
// values in 34 bytes and q4_0 in 18. Truncating to 8 and 4 bits would
// understate the cache, which is the one direction this package must never
// err in.
func TestQuantizedKVAccountsForBlockOverhead(t *testing.T) {
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256}
	f16, _ := g.BytesPerToken(KVTypeF16)

	q8, ok := g.BytesPerToken(KVTypeQ8_0)
	if !ok {
		t.Fatal("q8_0 not ok")
	}
	if want := f16 * 17 / 32; q8 != want {
		t.Errorf("q8_0 = %d, want %d (8.5 bits/element, not 8)", q8, want)
	}
	if q8 <= f16/2 {
		t.Errorf("q8_0 (%d) must exceed half of f16 (%d) — block scales are not free", q8, f16/2)
	}

	q4, _ := g.BytesPerToken(KVTypeQ4_0)
	if want := f16 * 9 / 32; q4 != want {
		t.Errorf("q4_0 = %d, want %d (4.5 bits/element)", q4, want)
	}

	if _, ok := g.BytesPerToken(KVCacheType("q5_1")); ok {
		t.Error("an unrecognized cache type must not silently produce a number")
	}
}

// An incomplete geometry must produce no estimate. Returning a partial answer
// here is the failure this package exists to avoid: a confidently wrong KV
// figure sized a window that then OOMs.
func TestIncompleteGeometryYieldsNoEstimate(t *testing.T) {
	for name, g := range map[string]KVGeometry{
		"no blocks":   {HeadCountKV: 4, KeyLength: 256, ValueLength: 256},
		"no kv heads": {BlockCount: 33, KeyLength: 256, ValueLength: 256},
		"no key len":  {BlockCount: 33, HeadCountKV: 4, ValueLength: 256},
		"no val len":  {BlockCount: 33, HeadCountKV: 4, KeyLength: 256},
		"empty":       {},
	} {
		t.Run(name, func(t *testing.T) {
			if g.Complete() {
				t.Fatal("Complete() true for an incomplete geometry")
			}
			if _, ok := g.BytesPerToken(KVTypeF16); ok {
				t.Error("BytesPerToken returned ok")
			}
			if _, ok := Fit(g, 16<<30, 4<<30, KVTypeF16); ok {
				t.Error("Fit returned ok")
			}
		})
	}
}

func TestGeometryReadsQwen35FromModelInfo(t *testing.T) {
	g := geometryFromModelInfo(modelInfo(t, map[string]any{
		"general.architecture":           "qwen35",
		"qwen35.block_count":             33,
		"qwen35.attention.head_count":    16,
		"qwen35.context_length":          262144,
		"qwen35.attention.head_count_kv": 4,
		"qwen35.attention.key_length":    256,
		"qwen35.attention.value_length":  256,
	}))
	if g.BlockCount != 33 || g.HeadCountKV != 4 || g.KeyLength != 256 || g.ValueLength != 256 {
		t.Fatalf("geometry = %+v", g)
	}
	if g.ContextMax != 262144 {
		t.Errorf("ContextMax = %d, want 262144", g.ContextMax)
	}
	if len(g.Inferred) != 0 {
		t.Errorf("nothing should be inferred when every key is present, got %v", g.Inferred)
	}
}

// The gemma4 case that motivated Inferred: head_count_kv comes back as JSON
// null. Treating null as "absent" (so the MHA fallback applies) is correct;
// what must not happen is the substitution passing unremarked, because it is
// an 8x swing in the cache estimate for this shape.
func TestNullHeadCountKVFallsBackAndSaysSo(t *testing.T) {
	g := geometryFromModelInfo(modelInfo(t, map[string]any{
		"general.architecture":           "gemma4",
		"gemma4.block_count":             48,
		"gemma4.attention.head_count":    16,
		"gemma4.attention.head_count_kv": nil,
		"gemma4.attention.key_length":    256,
		"gemma4.attention.value_length":  256,
	}))
	if g.HeadCountKV != 16 {
		t.Errorf("HeadCountKV = %d, want 16 (fallback to head_count)", g.HeadCountKV)
	}
	if len(g.Inferred) == 0 {
		t.Fatal("a fallback must be recorded in Inferred, not applied silently")
	}
	if !strings.Contains(strings.Join(g.Inferred, " "), "head_count_kv") {
		t.Errorf("Inferred = %v, want it to name head_count_kv", g.Inferred)
	}
}

func TestKeyLengthDerivedFromEmbeddingWhenAbsent(t *testing.T) {
	g := geometryFromModelInfo(modelInfo(t, map[string]any{
		"general.architecture":          "llama",
		"llama.block_count":             32,
		"llama.attention.head_count":    32,
		"llama.embedding_length":        4096,
		"llama.attention.head_count_kv": 8,
	}))
	if g.KeyLength != 128 || g.ValueLength != 128 {
		t.Errorf("key/value length = %d/%d, want 128/128 (4096/32)", g.KeyLength, g.ValueLength)
	}
	if len(g.Inferred) != 2 {
		t.Errorf("both derived lengths should be recorded, got %v", g.Inferred)
	}
}

// Some GGUFs emit per-layer dimensions as arrays. Taking the maximum keeps the
// estimate on the over-reserving side, which is the safe direction.
func TestPerLayerArrayDimensionsTakeTheMaximum(t *testing.T) {
	g := geometryFromModelInfo(modelInfo(t, map[string]any{
		"general.architecture":           "exotic",
		"exotic.block_count":             4,
		"exotic.attention.head_count_kv": []int{2, 4, 8, 4},
		"exotic.attention.key_length":    64,
		"exotic.attention.value_length":  64,
	}))
	if g.HeadCountKV != 8 {
		t.Errorf("HeadCountKV = %d, want 8 (max of the per-layer array)", g.HeadCountKV)
	}
}

// Sliding-window attention is recorded but not discounted. The estimate is a
// deliberate upper bound for these models.
func TestSlidingWindowIsRecordedButNotDiscounted(t *testing.T) {
	info := map[string]any{
		"general.architecture":           "gemma4",
		"gemma4.block_count":             48,
		"gemma4.attention.head_count_kv": 8,
		"gemma4.attention.key_length":    256,
		"gemma4.attention.value_length":  256,
	}
	plain := geometryFromModelInfo(modelInfo(t, info))
	info["gemma4.attention.sliding_window"] = 1024
	swa := geometryFromModelInfo(modelInfo(t, info))

	if swa.SWAWindow != 1024 {
		t.Errorf("SWAWindow = %d, want 1024", swa.SWAWindow)
	}
	a, _ := plain.BytesPerToken(KVTypeF16)
	b, _ := swa.BytesPerToken(KVTypeF16)
	if a != b {
		t.Errorf("SWA changed the estimate (%d vs %d); it must stay an upper bound", a, b)
	}
}

func TestFitSolvesForTheLargestWindowThatFits(t *testing.T) {
	// The real machine: 132 KiB/token, 4.00 GiB of weights, ~10.5 GiB budget
	// (a 16 GB card less the arbiter seat and driver overhead).
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256, ContextMax: 262144}
	weights := gib(4.00)

	win, ok := Fit(g, gib(10.5), weights, KVTypeF16)
	if !ok {
		t.Fatal("Fit not ok")
	}
	if win%fitStep != 0 {
		t.Errorf("window %d is not a multiple of %d", win, fitStep)
	}
	// 6.5 GiB / 132 KiB ≈ 51,655 tokens.
	if win < 51000 || win > 51712 {
		t.Errorf("window = %d, want ~51.6k", win)
	}
	// The fitted window must actually fit.
	kv, _ := KVBytes(g, win, KVTypeF16)
	if kv+weights > gib(10.5) {
		t.Errorf("fitted window overruns the budget: %s + %s", FormatGiB(kv), FormatGiB(weights))
	}
}

// The bug this whole file exists to prevent, stated as a test: a model with a
// 262144 training context on a 16 GB card must not be sized from its training
// max. RecommendContextWindow says 131072; Fit must say far less.
func TestFitBeatsTheModelMaxRecommendationOnAConstrainedCard(t *testing.T) {
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256, ContextMax: 262144}
	rec := RecommendContextWindow(g.ContextMax)
	if rec != 131072 {
		t.Fatalf("precondition changed: RecommendContextWindow(262144) = %d", rec)
	}
	fitted, ok := Fit(g, gib(14.5), gib(4.00), KVTypeF16)
	if !ok {
		t.Fatal("Fit not ok")
	}
	if fitted >= rec {
		t.Errorf("fitted %d did not improve on the model-max recommendation %d", fitted, rec)
	}
	kv, _ := KVBytes(g, rec, KVTypeF16)
	if kv < gib(16) {
		t.Errorf("precondition changed: KV at %d is %s, expected >16 GiB", rec, FormatGiB(kv))
	}
}

func TestFitRefusesRatherThanReturningAUselessWindow(t *testing.T) {
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256, ContextMax: 262144}
	t.Run("weights exceed budget", func(t *testing.T) {
		if _, ok := Fit(g, 2<<30, 4<<30, KVTypeF16); ok {
			t.Error("Fit returned ok when the weights alone overrun the budget")
		}
	})
	t.Run("below the viable floor", func(t *testing.T) {
		// Room for ~1000 tokens: under MinFittedContextWindow.
		budget := int64(4<<30) + 135168*1000
		if win, ok := Fit(g, budget, 4<<30, KVTypeF16); ok {
			t.Errorf("Fit returned %d, want a refusal below %d", win, MinFittedContextWindow)
		}
	})
}

func TestFitIsCappedAtTheModelMax(t *testing.T) {
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256, ContextMax: 8192}
	win, ok := Fit(g, 64<<30, 1<<30, KVTypeF16)
	if !ok {
		t.Fatal("Fit not ok")
	}
	if win != 8192 {
		t.Errorf("window = %d, want the model max 8192", win)
	}
}

// WeightsBytes must subtract the loaded window's KV from Ollama's resident
// size rather than trusting /api/tags, whose on-disk figure includes a vision
// projector that is not resident. The numbers here are the measured ones:
// 6.01 GiB resident at 16000 tokens is 4.00 GiB of weights, where /api/tags
// reports 6.57 GiB.
func TestWeightsBytesSubtractsTheLoadedKVCache(t *testing.T) {
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256}
	f := Footprint{Size: gib(6.01), SizeVRAM: gib(6.01), ContextLength: 16000}

	w, ok := WeightsBytes(f, g, KVTypeF16)
	if !ok {
		t.Fatal("WeightsBytes not ok")
	}
	if gib := float64(w) / (1 << 30); gib < 3.98 || gib > 4.02 {
		t.Errorf("weights = %.2f GiB, want ~4.00", gib)
	}
	const tagsSize = 6.57
	if gib := float64(w) / (1 << 30); tagsSize-gib < 2.0 {
		t.Errorf("expected the on-disk size to overstate weights by >2 GiB; got %.2f vs %.2f", tagsSize, gib)
	}
}

func TestWeightsBytesNeedsALoadedModel(t *testing.T) {
	g := KVGeometry{BlockCount: 33, HeadCountKV: 4, KeyLength: 256, ValueLength: 256}
	if _, ok := WeightsBytes(Footprint{}, g, KVTypeF16); ok {
		t.Error("WeightsBytes returned ok for an unloaded model")
	}
	if _, ok := WeightsBytes(Footprint{Size: 1 << 30}, g, KVTypeF16); ok {
		t.Error("WeightsBytes returned ok without a loaded context length")
	}
}

func TestFullyOnGPUIsTheEmpiricalFitCheck(t *testing.T) {
	if !(Footprint{Size: 100, SizeVRAM: 100}).FullyOnGPU() {
		t.Error("a fully-resident model should report FullyOnGPU")
	}
	if (Footprint{Size: 100, SizeVRAM: 60}).FullyOnGPU() {
		t.Error("a partially-offloaded model must not report FullyOnGPU")
	}
	if (Footprint{}).FullyOnGPU() {
		t.Error("an empty footprint must not report FullyOnGPU")
	}
}
