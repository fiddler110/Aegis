package ollamainfo

// KV-cache fit calibration (P69.5).
//
// RecommendContextWindow answers "what window does this model deserve" from
// the model's training maximum alone. That is the right question for a machine
// with room to spare and the wrong one for a VRAM-constrained local GPU, where
// the binding constraint is not the model's training context but how much KV
// cache fits next to the weights. The two answers can differ by an order of
// magnitude: a 9B whose training context is 262144 gets recommended 131072,
// which is 16.5 GiB of KV cache alone — more than a 16 GB card holds before
// the weights are loaded.
//
// The arithmetic is exact and needs nothing Aegis does not already fetch. A
// transformer's KV cache is
//
//	caching_layers × kv_heads × (key_length + value_length) × bytes_per_element
//
// bytes per token, and every factor is in the /api/show model_info map this
// package already parses for context_length.
//
// The first factor is not the block count, and assuming it was cost a fourfold
// over-reservation on every hybrid-attention model (P83). A stack that uses
// linear or state-space attention in three layers of four caches in only the
// fourth; the others hold a fixed-size recurrent state that does not grow with
// the context. Ollama reports the period as <arch>.full_attention_interval and
// nothing read it, so a qwen35-9b was budgeted 132 KiB per token against 33
// measured — and `--fit`, autofit_context and the resident-set planner all
// served a quarter of the window the card could hold. See KVLayers.
//
// What this file deliberately does NOT do is detect total VRAM. internal/hwinfo
// rules that out ("no GPU/VRAM detection is attempted, on any platform, ever",
// per P17.5) because it would mean reimplementing Ollama's offload heuristic
// blind from a fragile proxy signal, and that reasoning still holds. So the
// memory budget is an input the caller supplies, and the *verification* that a
// chosen window actually fit is empirical: Loaded reports Ollama's own
// size/size_vram split, which is Ollama's accounting of its own decision rather
// than a guess about it.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// KVCacheType is the element type Ollama stores K and V in (Ollama's
// OLLAMA_KV_CACHE_TYPE / llama.cpp's --cache-type-k/v).
type KVCacheType string

const (
	// KVTypeF16 is the default: 16 bits per element, no block overhead.
	KVTypeF16 KVCacheType = "f16"
	// KVTypeQ8_0 is llama.cpp's q8_0 block format.
	KVTypeQ8_0 KVCacheType = "q8_0"
	// KVTypeQ4_0 is llama.cpp's q4_0 block format.
	KVTypeQ4_0 KVCacheType = "q4_0"
)

// bitsX2 returns twice the bits-per-element, so the block-quantized formats
// stay exact in integer arithmetic. The doubling is not cosmetic: a q8_0 block
// is a 2-byte scale plus 32 int8 values — 34 bytes for 32 elements, i.e. 8.5
// bits each, not 8. q4_0 is 18 bytes for 32 elements, 4.5 bits each. Rounding
// those down to 8 and 4 would understate the cache by ~6% and ~11%, and the
// whole point of this file is to err toward *over*-reserving.
func (t KVCacheType) bitsX2() (int64, bool) {
	switch t {
	case "", KVTypeF16:
		return 32, true
	case KVTypeQ8_0:
		return 17, true
	case KVTypeQ4_0:
		return 9, true
	}
	return 0, false
}

// KVGeometry is the per-model shape the KV-cache size derives from, read from
// /api/show's model_info.
type KVGeometry struct {
	Arch        string
	BlockCount  int // transformer blocks in total — NOT the number holding a KV cache; see KVLayers
	HeadCountKV int // grouped-query attention: KV heads, not attention heads
	KeyLength   int // per-head key dimension
	ValueLength int // per-head value dimension
	ContextMax  int // training context length, 0 when unknown

	// FullAttentionInterval is the period of full-attention layers in a hybrid
	// stack: 4 means every fourth block uses full attention and the other three
	// use linear/state-space attention, which holds a fixed-size recurrent state
	// instead of a per-token KV cache. 0 or 1 is a conventional stack where
	// every block caches.
	//
	// P83: leaving this out was a fourfold over-reservation. `blocks × kv_heads
	// × (k+v)` silently assumes every block caches, and on qwen35 that predicted
	// 132 KiB per token against 33 KiB measured — so every window `--fit`,
	// autofit_context and the resident-set planner produced was a quarter of
	// what the card could serve. Ollama reports the interval; nothing read it.
	FullAttentionInterval int

	// NextNPredictLayers is the count of multi-token-prediction blocks included
	// in BlockCount. They are recorded because they inflate the block count
	// without contributing a full-attention layer at the stack's own interval.
	NextNPredictLayers int

	// SWAWindow is the sliding-window size when the architecture reports one,
	// else 0. It is recorded but deliberately NOT applied as a discount — see
	// BytesPerToken.
	SWAWindow int

	// Inferred names the fields that came from a fallback rather than from
	// their own key. A non-empty Inferred means the estimate rests on an
	// assumption the caller should surface, not suppress: the gemma4 case that
	// motivated this field reported head_count_kv as JSON null, and silently
	// substituting head_count overstated its KV cache eightfold.
	Inferred []string
}

// Complete reports whether every factor BytesPerToken needs is present. An
// incomplete geometry yields no estimate at all rather than a guess.
func (g KVGeometry) Complete() bool {
	return g.BlockCount > 0 && g.HeadCountKV > 0 && g.KeyLength > 0 && g.ValueLength > 0
}

// BytesPerToken returns the KV-cache bytes one token of context costs.
//
// Sliding-window attention is knowingly ignored, which makes this an upper
// bound for SWA models. A SWA model's cache stops growing at the window on the
// layers that use it, but GGUF does not reliably say *which* layers those are
// (architectures interleave SWA and full-attention blocks on their own
// patterns), so applying the discount would require guessing the interleave.
// Over-reserving costs context; under-reserving costs an OOM or a silent
// spill to system RAM, so the bound goes in the safe direction and SWAWindow
// is reported separately for the caller to mention.
func (g KVGeometry) BytesPerToken(t KVCacheType) (int64, bool) {
	if !g.Complete() {
		return 0, false
	}
	bx2, ok := t.bitsX2()
	if !ok {
		return 0, false
	}
	// caching layers × kv_heads × (k+v) dims × bits/element ÷ 8 bits per byte,
	// with the ×2 from bitsX2 divided back out: ÷16 total.
	return int64(g.KVLayers()) * int64(g.HeadCountKV) * int64(g.KeyLength+g.ValueLength) * bx2 / 16, true
}

// KVLayers is the number of blocks that actually hold a per-token KV cache.
//
// On a conventional stack that is every block. On a hybrid one it is every
// FullAttentionInterval-th block: the rest use linear or state-space attention,
// whose recurrent state is a fixed size per sequence and does not grow with the
// context, so it belongs in the weights rather than in the per-token cost.
//
// The multi-token-prediction blocks are excluded before dividing. They are
// counted in block_count but sit outside the repeating attention pattern, so
// including them rounds the interval arithmetic up by one layer on a stack whose
// block count is not a multiple of the interval — qwen35 reports 33 blocks with
// interval 4, which is 32 real blocks plus 1 MTP block, and 32/4 = 8 exactly.
//
// Verified against measurement: 8 layers predicts 32.00 KiB per token where
// `aegis models --calibrate` measures 32.66 on that model — a 2% shortfall,
// against the 304% overshoot of counting every block. The residual is the
// state-space layers' own constant, which this deliberately does not model:
// it is a per-sequence cost, not a per-token one.
func (g KVGeometry) KVLayers() int {
	blocks := g.BlockCount
	if g.FullAttentionInterval <= 1 {
		return blocks
	}
	if g.NextNPredictLayers > 0 && g.NextNPredictLayers < blocks {
		blocks -= g.NextNPredictLayers
	}
	if n := blocks / g.FullAttentionInterval; n > 0 {
		return n
	}
	// An interval larger than the whole stack still leaves one full-attention
	// layer; returning 0 here would make the cache free, which it is not.
	return 1
}

// Hybrid reports whether this architecture caches in only some of its blocks.
func (g KVGeometry) Hybrid() bool {
	return g.FullAttentionInterval > 1 && g.KVLayers() < g.BlockCount
}

// fitStep is the granularity fitted windows are rounded down to. A window is a
// user-facing number that ends up in a config file and in log lines; landing on
// 15872 rather than 15900 costs nothing and reads as a decision rather than a
// remainder.
const fitStep = 512

// MinFittedContextWindow is the floor below which a fitted window is reported
// as not viable instead of returned. A window this small cannot hold Aegis's
// system prompt plus a tool round, so returning it would trade a clear "this
// does not fit" for a config that fails on its first real turn.
const MinFittedContextWindow = 2048

// Fit solves for the largest context window whose KV cache fits in
// budgetBytes alongside weightsBytes, rounded down to fitStep and capped at the
// model's training maximum. ok is false when the geometry is incomplete, the
// weights alone exceed the budget, or the result lands under
// MinFittedContextWindow.
func Fit(g KVGeometry, budgetBytes, weightsBytes int64, t KVCacheType) (int, bool) {
	perToken, ok := g.BytesPerToken(t)
	if !ok || perToken <= 0 {
		return 0, false
	}
	avail := budgetBytes - weightsBytes
	if avail <= 0 {
		return 0, false
	}
	tokens := avail / perToken
	if g.ContextMax > 0 && tokens > int64(g.ContextMax) {
		tokens = int64(g.ContextMax)
	}
	win := int(tokens) / fitStep * fitStep
	if win < MinFittedContextWindow {
		return 0, false
	}
	return win, true
}

// KVBytes returns the KV-cache bytes a window of tokens costs, for printing a
// window→footprint table. ok mirrors BytesPerToken.
func KVBytes(g KVGeometry, tokens int, t KVCacheType) (int64, bool) {
	perToken, ok := g.BytesPerToken(t)
	if !ok {
		return 0, false
	}
	return perToken * int64(tokens), true
}

// Geometry reads a model's KV geometry from POST /api/show.
func Geometry(ctx context.Context, nativeBase, model string) (KVGeometry, bool) {
	info, ok := showModelInfo(ctx, nativeBase, model)
	if !ok {
		return KVGeometry{}, false
	}
	return geometryFromModelInfo(info), true
}

// geometryFromModelInfo is the pure half of Geometry, split out so every
// fallback path is unit-testable against a recorded model_info map without a
// live server.
func geometryFromModelInfo(info map[string]json.RawMessage) KVGeometry {
	var g KVGeometry
	if raw, ok := info["general.architecture"]; ok {
		_ = json.Unmarshal(raw, &g.Arch)
	}
	// key resolves "<arch>.suffix", falling back to a suffix scan for GGUFs
	// whose architecture string does not match their key prefix.
	key := func(suffix string) int {
		if g.Arch != "" {
			if n := dimField(info, g.Arch+"."+suffix); n > 0 {
				return n
			}
		}
		for k := range info {
			if strings.HasSuffix(k, "."+suffix) {
				if n := dimField(info, k); n > 0 {
					return n
				}
			}
		}
		return 0
	}

	g.BlockCount = key("block_count")
	g.ContextMax = key("context_length")
	g.SWAWindow = key("attention.sliding_window")
	g.FullAttentionInterval = key("full_attention_interval")
	g.NextNPredictLayers = key("nextn_predict_layers")

	g.HeadCountKV = key("attention.head_count_kv")
	heads := key("attention.head_count")
	if g.HeadCountKV == 0 && heads > 0 {
		// Multi-head attention (no GQA) omits head_count_kv, and at least one
		// architecture emits it as JSON null. Both mean "one KV head per
		// attention head".
		g.HeadCountKV = heads
		g.Inferred = append(g.Inferred, "attention.head_count_kv (assumed = head_count; no grouped-query attention)")
	}

	g.KeyLength = key("attention.key_length")
	g.ValueLength = key("attention.value_length")
	if (g.KeyLength == 0 || g.ValueLength == 0) && heads > 0 {
		// Architectures that keep head_dim = embedding_length / head_count
		// omit the explicit lengths.
		if embed := key("embedding_length"); embed > 0 {
			headDim := embed / heads
			if headDim > 0 {
				if g.KeyLength == 0 {
					g.KeyLength = headDim
					g.Inferred = append(g.Inferred, "attention.key_length (derived from embedding_length/head_count)")
				}
				if g.ValueLength == 0 {
					g.ValueLength = headDim
					g.Inferred = append(g.Inferred, "attention.value_length (derived from embedding_length/head_count)")
				}
			}
		}
	}
	sort.Strings(g.Inferred)
	return g
}

// dimField reads a model_info dimension that may be a scalar or a per-layer
// array. Arrays yield their maximum: a model with per-layer KV head counts
// sizes its cache from the widest layer, and taking the max keeps the estimate
// on the over-reserving side. JSON null (which some GGUFs emit for an absent
// grouped-query head count) reads as 0 — absent — rather than as a value.
func dimField(info map[string]json.RawMessage, key string) int {
	raw, ok := info[key]
	if !ok {
		return 0
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f)
	}
	var arr []float64
	if err := json.Unmarshal(raw, &arr); err != nil {
		return 0
	}
	max := 0
	for _, v := range arr {
		if int(v) > max {
			max = int(v)
		}
	}
	return max
}

// showModelInfo fetches the model_info map from POST /api/show.
func showModelInfo(ctx context.Context, nativeBase, model string) (map[string]json.RawMessage, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"model": model, "name": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nativeBase+"/api/show", bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var out struct {
		ModelInfo map[string]json.RawMessage `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false
	}
	if len(out.ModelInfo) == 0 {
		return nil, false
	}
	return out.ModelInfo, true
}

// Footprint is what Ollama reports for a currently-loaded model: its total
// resident size, how much of that is in VRAM, and the window it was loaded at.
type Footprint struct {
	Size          int64
	SizeVRAM      int64
	ContextLength int
}

// FullyOnGPU reports whether Ollama placed the entire model in VRAM. This is
// the empirical fit check the package doc refers to: when it is false, Ollama
// spilled layers or KV to system RAM, which is the observable form of "the
// requested window did not fit" — and it is Ollama's own accounting of its own
// placement decision, not an inference drawn from a GPU driver query.
func (f Footprint) FullyOnGPU() bool { return f.Size > 0 && f.SizeVRAM >= f.Size }

// Loaded returns the footprint Ollama reports for model, when it is currently
// resident. ok is false when the server is unreachable or the model is not
// loaded — in which case there is nothing measured to report, and the caller
// should say so rather than substitute an estimate.
func Loaded(ctx context.Context, nativeBase, model string) (Footprint, bool) {
	models, ok := psFootprints(ctx, nativeBase)
	if !ok {
		return Footprint{}, false
	}
	for _, m := range models {
		if sameModel(m.Name, model) || sameModel(m.Model, model) {
			return Footprint{Size: m.Size, SizeVRAM: m.SizeVRAM, ContextLength: m.ContextLength}, true
		}
	}
	return Footprint{}, false
}

// WeightsBytes derives the resident weight bytes from a loaded model's
// footprint by subtracting the KV cache its loaded window accounts for.
//
// This exists because the obvious source is wrong. /api/tags reports a model's
// on-disk size, which for a multimodal model includes a vision projector that
// is not resident unless an image is sent: qwen35-9b reports 6.57 GiB on disk
// against 4.00 GiB of actually-loaded weights, a 2.57 GiB overstatement that
// would silently eat a fitted window's entire headroom.
func WeightsBytes(f Footprint, g KVGeometry, t KVCacheType) (int64, bool) {
	if f.Size <= 0 || f.ContextLength <= 0 {
		return 0, false
	}
	kv, ok := KVBytes(g, f.ContextLength, t)
	if !ok {
		return 0, false
	}
	w := f.Size - kv
	if w <= 0 {
		return 0, false
	}
	return w, true
}

// psFootprint is one /api/ps entry with the size fields Loaded needs.
type psFootprint struct {
	Name          string `json:"name"`
	Model         string `json:"model"`
	ContextLength int    `json:"context_length"`
	Size          int64  `json:"size"`
	SizeVRAM      int64  `json:"size_vram"`
}

func psFootprints(ctx context.Context, nativeBase string) ([]psFootprint, bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBase+"/api/ps", nil)
	if err != nil {
		return nil, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var out struct {
		Models []psFootprint `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false
	}
	return out.Models, true
}

// FormatGiB renders a byte count for display.
func FormatGiB(b int64) string { return fmt.Sprintf("%.2f GiB", float64(b)/(1<<30)) }

// WarmAt loads model into memory at a given serving window and waits for the
// load to finish, so its resident weights can be measured.
//
// It exists because Warm (ollamainfo.go) is a pre-warm — best-effort, fixed 3s,
// "the next request should not pay a cold load" — and this is not that. A
// boot-time fit (P72.1) cannot proceed at all until /api/ps has something to
// report, so the caller here needs to *wait* for the load and needs its own
// deadline to decide how long that is worth. A cold 9B on a mid-range card is
// well past 3s.
//
// numCtx > 0 loads at that window rather than the model's default. That is not
// cosmetic either: the fit is computed from weights, which are window-invariant,
// but the *runner* Ollama leaves behind is at whatever window this call asked
// for — so loading at the window the first turn would have used means a fit that
// changes nothing costs no reload at all.
func WarmAt(ctx context.Context, nativeBase, model string, numCtx int) error {
	if nativeBase == "" || model == "" {
		return fmt.Errorf("no ollama base url or model")
	}
	payload := map[string]any{"model": model}
	if numCtx > 0 {
		payload["options"] = map[string]any{"num_ctx": numCtx}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nativeBase+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama load %s: %s", model, resp.Status)
	}
	// Ollama answers an empty-prompt /api/generate once the model is resident,
	// but the body must be drained before /api/ps is asked about it — reading
	// nothing and closing can return before the load is accounted for.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Unload evicts a model from memory by sending keep_alive:0, the inverse of
// WarmAt.
//
// It exists for the moment a co-resident plan ends (P72.3). A plan's members
// stay resident for keep_alive after the workload that needed them is over, so
// the model the daemon goes back to serving is restored to a solo window while
// the others are still holding the memory that window assumes is free. Ollama
// resolves that by evicting something itself, but only after it has tried to
// place the load — the observable form being a spill to system RAM. Saying which
// models are finished with is cheaper and deterministic.
//
// Best-effort: a non-nil error means the model may still be resident, which
// costs memory rather than correctness.
func Unload(ctx context.Context, nativeBase, model string) error {
	if nativeBase == "" || model == "" {
		return fmt.Errorf("no ollama base url or model")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"model": model, "keep_alive": 0})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nativeBase+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama unload %s: %s", model, resp.Status)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
