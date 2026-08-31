package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// TestCreateSessionOriginNormalizesUnknownValues covers the HTTP boundary for
// P81.14/P80.1's origin stamp: a caller's declared origin is trusted when it
// names a known surface, and normalized to "web" (the safe default for the
// browser UI, which has no Go call site to stamp it itself) otherwise —
// never persisted verbatim, since an arbitrary string here would defeat the
// mcpserver/acp origin-based refusal that reads it back.
func TestCreateSessionOriginNormalizesUnknownValues(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, &readFileAdapter{}, reg)
	srv.workspace = root
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	create := func(origin string) *api.SessionMeta {
		body, _ := json.Marshal(api.CreateSessionRequest{Mode: "build", Origin: origin})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sessions", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create session (origin=%q): status = %d", origin, resp.StatusCode)
		}
		var meta api.SessionMeta
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
			t.Fatal(err)
		}
		return &meta
	}

	cases := []struct{ in, want string }{
		{"mcp", "mcp"},
		{"acp", "acp"},
		{"tui", "tui"},
		{"cli", "cli"},
		{"", "web"},
		{"anything-a-caller-makes-up", "web"},
	}
	for _, tc := range cases {
		meta := create(tc.in)
		if meta.Origin != tc.want {
			t.Errorf("origin %q: got %q, want %q", tc.in, meta.Origin, tc.want)
		}
	}
}
