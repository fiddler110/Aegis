package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModelsPaginates(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-api-key") != "k" || r.URL.Path != "/v1/models" {
			t.Errorf("bad request: %s %v", r.URL, r.Header)
		}
		w.Header().Set("content-type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","max_input_tokens":1000000,"max_tokens":128000}],"has_more":true,"last_id":"claude-opus-5-5"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-haiku-5-5"}],"has_more":false,"last_id":"claude-haiku-5-5"}`))
	}))
	defer srv.Close()
	models, err := New("k", WithBaseURL(srv.URL)).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(models) != 2 || models[0].ContextWindow != 1_000_000 || models[0].MaxOutput != 128000 || models[1].ID != "claude-haiku-5-5" {
		t.Errorf("calls=%d models=%+v", calls, models)
	}
}

func TestListModelsReportsAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()
	if _, err := New("bad", WithBaseURL(srv.URL)).ListModels(context.Background()); err == nil {
		t.Fatal("want an error for a 401")
	}
	if _, err := New("", WithBaseURL(srv.URL)).ListModels(context.Background()); err == nil {
		t.Fatal("want an error for a missing key")
	}
}
