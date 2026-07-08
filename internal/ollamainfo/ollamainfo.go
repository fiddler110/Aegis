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

// psContext returns the context_length /api/ps reports for model when it is
// currently loaded, or 0. Older Ollama versions omit the field; model-name
// matching tolerates a missing ":latest" tag on either side.
func psContext(ctx context.Context, nativeBase, model string) int {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBase+"/api/ps", nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var out struct {
		Models []struct {
			Name          string `json:"name"`
			Model         string `json:"model"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0
	}
	for _, m := range out.Models {
		if sameModel(m.Name, model) || sameModel(m.Model, model) {
			return m.ContextLength
		}
	}
	return 0
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
