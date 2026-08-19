package ollamainfo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListLocalExcludesEmbeddingOnlyModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[
			{"name":"aegis-qwen35-9b:16k","size":7056739633,"capabilities":["completion","vision"],
			 "details":{"family":"qwen35","parameter_size":"9.2B","quantization_level":"Q4_K_M"}},
			{"name":"nomic-embed-text:latest","size":274302450,"capabilities":["embedding"],
			 "details":{"family":"nomic-bert","parameter_size":"137M","quantization_level":"F16"}}
		]}`))
	}))
	defer ts.Close()

	models, ok := ListLocal(context.Background(), ts.URL)
	if !ok {
		t.Fatal("ListLocal reported not ok")
	}
	if len(models) != 1 || models[0].Name != "aegis-qwen35-9b:16k" {
		t.Fatalf("want only the completion-capable model, got %+v", models)
	}
	if models[0].ParameterSize != "9.2B" || models[0].Family != "qwen35" || models[0].Quantization != "Q4_K_M" {
		t.Fatalf("metadata not carried through: %+v", models[0])
	}
}

// A model with no capabilities field at all (an older Ollama server) must
// still be listed — absence of the field is not evidence it can't chat, only
// that this server didn't say. Excluding on missing data would silently empty
// the picker against an older Ollama.
func TestListLocalKeepsModelsWithNoCapabilitiesField(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:1.5b","size":986062089,
			"details":{"family":"qwen2","parameter_size":"1.5B","quantization_level":"Q4_K_M"}}]}`))
	}))
	defer ts.Close()

	models, ok := ListLocal(context.Background(), ts.URL)
	if !ok || len(models) != 1 {
		t.Fatalf("want the one model kept, got ok=%v models=%+v", ok, models)
	}
}

func TestListLocalUnreachable(t *testing.T) {
	if _, ok := ListLocal(context.Background(), "http://127.0.0.1:1"); ok {
		t.Fatal("want ok=false against an unreachable server")
	}
}
