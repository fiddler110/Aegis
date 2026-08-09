package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// captureOptions runs one request against a stub server and returns the
// options object the adapter actually put on the wire.
func captureOptions(t *testing.T, req provider.Request) map[string]any {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	req.Model = "m"
	req.Messages = []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}}
	stream, err := New(WithBaseURL(srv.URL)).Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream { //nolint:revive // drain
	}
	if body == nil {
		return nil
	}
	opts, _ := body["options"].(map[string]any)
	return opts
}

// Temperature and seed are what make a drive reproducible; before P39.14 the
// engine plumbed Temperature all the way to this adapter and nothing ever set
// it, so every local run inherited Ollama's 0.8 default and took a different
// path each time.
func TestSamplingOptionsReachTheWire(t *testing.T) {
	temp := 0.0
	seed := 42
	opts := captureOptions(t, provider.Request{Temperature: &temp, Seed: &seed})
	if opts == nil {
		t.Fatal("no options object sent")
	}
	got, ok := opts["temperature"]
	if !ok {
		t.Error("temperature 0 was dropped — the value a deterministic run most wants")
	} else if got != float64(0) {
		t.Errorf("temperature = %v, want 0", got)
	}
	if got, ok := opts["seed"]; !ok || got != float64(42) {
		t.Errorf("seed = %v (present=%v), want 42", got, ok)
	}
}

// Unset must stay unset: sending a default temperature would silently override
// a Modelfile that deliberately pins one.
func TestSamplingOptionsOmittedWhenUnset(t *testing.T) {
	opts := captureOptions(t, provider.Request{MaxTokens: 16})
	if _, ok := opts["temperature"]; ok {
		t.Error("temperature sent when the caller set none")
	}
	if _, ok := opts["seed"]; ok {
		t.Error("seed sent when the caller set none")
	}
}
