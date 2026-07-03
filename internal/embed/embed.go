// Package embed provides an optional semantic-embedding layer for the
// FTS5-backed knowledge and long-term-memory stores (P5.8). It is a thin
// client for Ollama's /api/embed endpoint plus the math (cosine similarity,
// reciprocal-rank fusion) needed to blend semantic recall with BM25 keyword
// search. Nothing here is required: callers pass a nil Embedder to keep the
// stores BM25-only, which remains the zero-config default.
package embed

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder turns text into vectors. Implementations may batch: len(out) ==
// len(texts) and out[i] corresponds to texts[i].
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OllamaEmbedder calls a local (or remote) Ollama server's /api/embed endpoint.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

// New builds an Embedder from primitive config values, or returns nil when
// disabled — the signal callers use to keep a store BM25-only. Only "ollama"
// (or an empty provider, which also means Ollama) is supported today.
func New(enabled bool, provider, model, baseURL string) Embedder {
	if !enabled {
		return nil
	}
	switch provider {
	case "ollama", "":
		return NewOllama(baseURL, model)
	default:
		return nil
	}
}

// NewOllama constructs an OllamaEmbedder. baseURL defaults to
// http://localhost:11434 when empty; model defaults to "nomic-embed-text".
func NewOllama(baseURL, model string) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbedder{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed implements Embedder against Ollama's /api/embed endpoint.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(ollamaEmbedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read embed response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed request: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out ollamaEmbedResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed response: got %d vectors for %d inputs", len(out.Embeddings), len(texts))
	}
	return out.Embeddings, nil
}

// Cosine returns the cosine similarity of a and b, in [-1, 1]. Mismatched or
// zero-length vectors return 0.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// EncodeVector packs a float32 vector into little-endian bytes for BLOB storage.
func EncodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeVector unpacks bytes previously produced by EncodeVector.
func DecodeVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// DefaultRRFK is the standard reciprocal-rank-fusion smoothing constant.
const DefaultRRFK = 60

// RRF fuses one or more ranked key lists (best match first) into a single
// fused score per key using reciprocal rank fusion: score = sum(1/(k+rank)),
// rank starting at 1. Keys absent from a list simply don't contribute a term
// from it. Use k=DefaultRRFK absent a reason to tune it.
func RRF(k int, rankings ...[]string) map[string]float64 {
	if k <= 0 {
		k = DefaultRRFK
	}
	scores := make(map[string]float64)
	for _, ranking := range rankings {
		for i, key := range ranking {
			scores[key] += 1.0 / float64(k+i+1)
		}
	}
	return scores
}
