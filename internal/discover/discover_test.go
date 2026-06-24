package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "llama3:latest"},
					{"name": "codellama:7b"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	models, err := probeOllama(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("probe error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "llama3:latest" {
		t.Errorf("expected llama3:latest, got %s", models[0].Name)
	}
	if models[0].Provider != "ollama" {
		t.Errorf("expected provider ollama, got %s", models[0].Provider)
	}
}

func TestProbeOpenAICompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-4"},
					{"id": "mistral-7b"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	models, err := probeOpenAICompat(context.Background(), srv.URL, "lmstudio")
	if err != nil {
		t.Fatalf("probe error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[1].Provider != "lmstudio" {
		t.Errorf("expected provider lmstudio, got %s", models[1].Provider)
	}
}

func TestDiscoverSkipsUnreachable(t *testing.T) {
	sources := []Source{
		{Name: "fake", Endpoint: "http://127.0.0.1:1", Probe: probeOllama},
	}
	models := Discover(context.Background(), sources, 500*time.Millisecond)
	if len(models) != 0 {
		t.Errorf("expected 0 models from unreachable source, got %d", len(models))
	}
}

func TestDiscoverAggregates(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"name": "model-a"}},
		})
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "model-b"}},
		})
	}))
	defer srv2.Close()

	sources := []Source{
		{Name: "ollama", Endpoint: srv1.URL, Probe: probeOllama},
		{Name: "lmstudio", Endpoint: srv2.URL, Probe: probeLMStudio},
	}
	models := Discover(context.Background(), sources, 2*time.Second)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
}
