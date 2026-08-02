// Package ollamainfo detects whether a provider base URL is backed by an
// Ollama server and, if so, determines the *effective* context window Ollama
// will actually serve for a model. This matters because Aegis talks to Ollama
// through its OpenAI-compatible endpoint, which offers no way to set or read
// num_ctx — when a prompt exceeds the serving context, Ollama silently drops
// the oldest tokens (including the system prompt), so an agent run degrades
// into a model that no longer knows its instructions. Knowing the real window
// lets the daemon compact proactively and warn the user instead.
package ollamainfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultServeContext is Ollama's out-of-the-box context length
// (OLLAMA_CONTEXT_LENGTH default). Used as a conservative floor when the
// model is not loaded yet and its modelfile pins no num_ctx: assuming too
// small causes extra compaction; assuming too large causes silent prompt
// truncation, which is strictly worse.
const DefaultServeContext = 4096

// Source identifies how a context window value was determined, ordered by
// authority.
type Source string

const (
	// SourceLoaded means /api/ps reported the allocation of the loaded model —
	// the actual serving context, authoritative.
	SourceLoaded Source = "loaded"
	// SourceModelfile means the model's modelfile pins num_ctx.
	SourceModelfile Source = "modelfile"
	// SourceDefault means neither was available and DefaultServeContext was
	// assumed (capped by the model's training context when known).
	SourceDefault Source = "default"
)

// Result is a detected context window and where it came from.
type Result struct {
	ContextWindow int
	Source        Source
	// ModelMax is the model's training context length from /api/show
	// model_info, 0 when unknown. Informational: the serving window is
	// usually far below it.
	ModelMax int
}

// Authoritative reports whether the value reflects the real allocation rather
// than an inference; non-authoritative results are worth re-detecting once
// the model has been loaded by a first request.
func (r Result) Authoritative() bool { return r.Source == SourceLoaded }

// Recommended-window calibration (P35.3). aegis doctor's legacy-compat fix used
// to suggest a fixed 32768 context_window — a number chosen to fit a 16GB-VRAM
// budget, not to cover a real session. A skill-driven / long-running workload
// routinely builds a >40k-token prompt before writing any output, so that fixed
// ceiling makes the very first real task fail (Ollama returns a hard 400 with no
// compaction attempted first). When the model's real training-context maximum is
// detectable (Result.ModelMax), recommend a materially larger, memory-bounded
// fraction of it instead.
const (
	// BaselineContextWindow is the fallback recommendation used when the
	// model's real max is unknown — the 16GB-VRAM-safe value that predates
	// P35.3.
	BaselineContextWindow = 32768
	// RecommendedContextWindowCap bounds the recommendation so a model with a
	// very large training context (some report 262144+) doesn't yield a KV
	// cache that blows past a typical local GPU's memory.
	RecommendedContextWindowCap = 131072
)

// RecommendContextWindow calibrates a suggested provider.context_window from a
// model's detected training-context maximum. It recommends half that maximum —
// enough headroom for the skill-driven sessions a smoke-test-derived number
// starves (P35.3) — bounded above by RecommendedContextWindowCap, below by
// BaselineContextWindow, and never above the model's own max. modelMax <= 0
// (undetectable) yields BaselineContextWindow unchanged.
func RecommendContextWindow(modelMax int) int {
	if modelMax <= 0 {
		return BaselineContextWindow
	}
	rec := modelMax / 2
	if rec > RecommendedContextWindowCap {
		rec = RecommendedContextWindowCap
	}
	if rec < BaselineContextWindow {
		rec = BaselineContextWindow
	}
	if rec > modelMax {
		rec = modelMax
	}
	return rec
}

// NativeBase converts an OpenAI-compat base URL (…:11434/v1) to the Ollama
// native API base. Empty input maps to the local default.
func NativeBase(baseURL string) string {
	if baseURL == "" {
		return "http://localhost:11434"
	}
	b := strings.TrimRight(baseURL, "/")
	b = strings.TrimSuffix(b, "/v1")
	return strings.TrimRight(b, "/")
}

// IsOllama probes GET /api/version on the native base. It is the cheapest
// Ollama-specific endpoint and exists on every version.
func IsOllama(ctx context.Context, nativeBase string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBase+"/api/version", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var v struct {
		Version string `json:"version"`
	}
	return json.NewDecoder(resp.Body).Decode(&v) == nil && v.Version != ""
}

// Detect returns the effective context window for model on an Ollama server,
// preferring (1) the loaded model's actual allocation from /api/ps, then
// (2) a modelfile-pinned num_ctx from /api/show, then (3) Ollama's server
// default, capped by the model's training context when known. ok is false
// when nativeBase is not a reachable Ollama server.
func Detect(ctx context.Context, nativeBase, model string) (Result, bool) {
	if !IsOllama(ctx, nativeBase) {
		return Result{}, false
	}

	res := Result{Source: SourceDefault, ContextWindow: DefaultServeContext}

	numCtx, modelMax := showInfo(ctx, nativeBase, model)
	res.ModelMax = modelMax
	if numCtx > 0 {
		res.ContextWindow = numCtx
		res.Source = SourceModelfile
	}
	if res.Source == SourceDefault && modelMax > 0 && modelMax < res.ContextWindow {
		res.ContextWindow = modelMax
	}

	if loaded := psContext(ctx, nativeBase, model); loaded > 0 {
		res.ContextWindow = loaded
		res.Source = SourceLoaded
	}
	return res, true
}

// psModel is one entry from /api/ps's loaded-model list.
type psModel struct {
	Name          string `json:"name"`
	Model         string `json:"model"`
	ContextLength int    `json:"context_length"`
}

// psModels fetches the currently-loaded models from /api/ps. ok is false when
// the request fails or the server does not respond 200 (older versions, or a
// non-Ollama base) — distinct from a successful empty list (nothing loaded).
func psModels(ctx context.Context, nativeBase string) (models []psModel, ok bool) {
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
		Models []psModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false
	}
	return out.Models, true
}

// psContext returns the context_length /api/ps reports for model when it is
// currently loaded, or 0. Older Ollama versions omit the field; model-name
// matching tolerates a missing ":latest" tag on either side.
func psContext(ctx context.Context, nativeBase, model string) int {
	models, ok := psModels(ctx, nativeBase)
	if !ok {
		return 0
	}
	for _, m := range models {
		if sameModel(m.Name, model) || sameModel(m.Model, model) {
			return m.ContextLength
		}
	}
	return 0
}

// IsLoaded reports whether model is currently resident in Ollama's memory per
// /api/ps. It keys on the model's presence in the loaded list, not on the
// context_length field (which older versions omit), so it stays correct as a
// pure "loaded / not loaded" gate. Returns false when nativeBase is
// unreachable or reports no such model — the safe default for a pre-warm
// decision, since warming an already-loaded model is a cheap no-op anyway.
func IsLoaded(ctx context.Context, nativeBase, model string) bool {
	models, ok := psModels(ctx, nativeBase)
	if !ok {
		return false
	}
	for _, m := range models {
		if sameModel(m.Name, model) || sameModel(m.Model, model) {
			return true
		}
	}
	return false
}

// Warm asks Ollama to load model into memory without generating anything, by
// POSTing an empty-prompt /api/generate request (the inverse of the
// keep_alive:0 unload in internal/cli/ollama.go). keep_alive is deliberately
// omitted so the model loads for Ollama's own default duration rather than
// being pinned — pre-warm removes the next cold load, it does not change the
// residency policy. Best-effort: a non-nil error means the warm-up did not
// happen, which is never fatal (the next real request simply pays the load).
func Warm(ctx context.Context, nativeBase, model string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"model": model})
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
		return fmt.Errorf("ollama warm-up: %s", resp.Status)
	}
	return nil
}

// WarmIfUnloaded loads model into memory only when /api/ps reports it is not
// already resident (P33.10 lever 2). It returns true when a warm-up was
// actually issued. The /api/ps gate is the item's explicit requirement — never
// pay a redundant load request for an already-loaded model.
func WarmIfUnloaded(ctx context.Context, nativeBase, model string) bool {
	if nativeBase == "" || model == "" {
		return false
	}
	if IsLoaded(ctx, nativeBase, model) {
		return false
	}
	_ = Warm(ctx, nativeBase, model)
	return true
}

// Digests fetches every locally-pulled model's content digest from GET
// /api/tags, keyed by the name Ollama reports (both the `name` and the `model`
// spelling are indexed, since callers spell models either way).
//
// It exists for P53.5's staleness rule: a tag is mutable, so "qwen3:14b" after
// an `ollama pull` may be different weights with different capabilities under
// the same name. The digest is the one value that moves when that happens,
// which makes it the invalidation key for any persisted per-model record.
//
// ok is false when the request fails or the server does not answer 200 — a
// non-Ollama base or an unreachable one — as distinct from a successful empty
// list (nothing pulled). Callers must treat !ok as "cannot tell", never as
// "everything is stale".
func Digests(ctx context.Context, nativeBase string) (map[string]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBase+"/api/tags", nil)
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
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false
	}
	digests := make(map[string]string, len(out.Models)*2)
	for _, m := range out.Models {
		if m.Digest == "" {
			continue
		}
		if m.Name != "" {
			digests[m.Name] = m.Digest
		}
		if m.Model != "" {
			digests[m.Model] = m.Digest
		}
	}
	return digests, true
}

// NativeToolSupport reports whether model's own manifest advertises the "tools"
// capability, per POST /api/show. The bool result is the claim; the second
// return says whether the claim was observable at all (older Ollama versions
// omit the capabilities list entirely, and an unreachable server tells us
// nothing).
//
// This is deliberately only the *manifest's* claim, never a substitute for
// internal/toolcallprobe: a manifest can advertise tool support the weights
// don't deliver, which is the whole reason the probe exists. Recording both
// lets the disagreement be seen rather than silently resolved one way.
func NativeToolSupport(ctx context.Context, nativeBase, model string) (bool, bool) {
	caps, ok := showCapabilities(ctx, nativeBase, model)
	if !ok {
		return false, false
	}
	for _, c := range caps {
		if c == "tools" {
			return true, true
		}
	}
	return false, true
}

// showCapabilities returns /api/show's capabilities list for model. ok is false
// when the request fails or the field is absent (pre-0.6 Ollama).
func showCapabilities(ctx context.Context, nativeBase, model string) ([]string, bool) {
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
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false
	}
	if out.Capabilities == nil {
		return nil, false
	}
	return out.Capabilities, true
}

// showInfo queries POST /api/show for a modelfile-pinned num_ctx and the
// model's training context length. Either may be 0 when absent.
func showInfo(ctx context.Context, nativeBase, model string) (numCtx, modelMax int) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"model": model, "name": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nativeBase+"/api/show", bytes.NewReader(body))
	if err != nil {
		return 0, 0
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0
	}
	var out struct {
		Parameters string                     `json:"parameters"`
		ModelInfo  map[string]json.RawMessage `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, 0
	}
	return parseNumCtx(out.Parameters), contextLengthFromModelInfo(out.ModelInfo)
}

// parseNumCtx extracts a "num_ctx <N>" line from a modelfile parameters dump.
func parseNumCtx(parameters string) int {
	for _, line := range strings.Split(parameters, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "num_ctx" {
			if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// contextLengthFromModelInfo pulls "<arch>.context_length" out of model_info,
// resolving <arch> via "general.architecture" and falling back to scanning
// for any key with that suffix.
func contextLengthFromModelInfo(info map[string]json.RawMessage) int {
	if len(info) == 0 {
		return 0
	}
	var arch string
	if raw, ok := info["general.architecture"]; ok {
		_ = json.Unmarshal(raw, &arch)
	}
	if arch != "" {
		if n := intField(info, arch+".context_length"); n > 0 {
			return n
		}
	}
	for k := range info {
		if strings.HasSuffix(k, ".context_length") {
			if n := intField(info, k); n > 0 {
				return n
			}
		}
	}
	return 0
}

func intField(info map[string]json.RawMessage, key string) int {
	raw, ok := info[key]
	if !ok {
		return 0
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0
	}
	return int(f)
}

// sameModel compares Ollama model names, treating a missing ":latest" tag as
// equivalent ("gemma4:12b" ≠ "gemma4", but "llama3.2" == "llama3.2:latest").
func sameModel(a, b string) bool {
	if a == b {
		return true
	}
	norm := func(s string) string {
		if !strings.Contains(s, ":") {
			return s + ":latest"
		}
		return s
	}
	return norm(a) == norm(b)
}

// Describe renders a human-readable provenance string for logs and /status.
func (r Result) Describe() string {
	switch r.Source {
	case SourceLoaded:
		return fmt.Sprintf("%d tokens (reported by Ollama for the loaded model)", r.ContextWindow)
	case SourceModelfile:
		return fmt.Sprintf("%d tokens (modelfile num_ctx)", r.ContextWindow)
	default:
		return fmt.Sprintf("%d tokens (Ollama default; set OLLAMA_CONTEXT_LENGTH or a modelfile num_ctx to raise it)", r.ContextWindow)
	}
}
