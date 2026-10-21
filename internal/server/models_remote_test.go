package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// listingAdapter is fixedAdapter plus a model listing.
type listingAdapter struct {
	fixedAdapter
	models []provider.ModelInfo
}

func (a listingAdapter) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return a.models, nil
}

func newRemoteModelsServer(t *testing.T, adapter provider.Adapter, baseURL string) *client.Client {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "anthropic", Model: "claude-opus-5-5", MaxTokens: 100, BaseURL: baseURL}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token")
}

func TestRemoteModelsAnnotatesAndFilters(t *testing.T) {
	cl := newRemoteModelsServer(t, listingAdapter{models: []provider.ModelInfo{
		{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5"},
		{ID: "text-embedding-3-large"},
		{ID: "claude-haiku-5-5", ContextWindow: 500_000},
	}}, "")
	resp, err := cl.ListRemoteModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Supported || resp.Provider != "anthropic" || len(resp.Models) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Models[0].ContextWindow != 1_000_000 || resp.Models[0].Notes == "" {
		t.Errorf("opus entry not annotated from the capability table: %+v", resp.Models[0])
	}
	if resp.Models[1].ContextWindow != 500_000 {
		t.Errorf("the provider's own window must win: %+v", resp.Models[1])
	}
}

func TestRemoteModelsUnsupportedWithoutALister(t *testing.T) {
	resp, err := newRemoteModelsServer(t, fixedAdapter{}, "").ListRemoteModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Supported {
		t.Errorf("an adapter with no listing must report unsupported: %+v", resp)
	}
}
