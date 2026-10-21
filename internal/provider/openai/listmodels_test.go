package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModelsSortsAndReadsContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("bad request: %s %v", r.URL, r.Header)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model","name":"A","context_length":200000},{"id":""}]}`))
	}))
	defer srv.Close()
	models, err := New("k", WithBaseURL(srv.URL)).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "a-model" || models[0].ContextWindow != 200000 || models[1].ID != "z-model" {
		t.Errorf("models = %+v", models)
	}
}
