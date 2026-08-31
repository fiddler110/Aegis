package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestConfigPatchWritesAuditRecord covers P81.14/P81.3's stated gap: an
// accepted PATCH /config/<section> call — which can weaken a security
// posture (sandbox backend, redaction, cost ceilings) with only the bearer
// token — previously left no durable record at all. It now writes a
// "config_patch" audit record naming the endpoint, the caller's remote
// address, what was requested, and what was actually applied.
func TestConfigPatchWritesAuditRecord(t *testing.T) {
	redirectConfigDir(t)

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.adminToken = "test-admin-token"
	srv.workspace = t.TempDir()
	srv.audit = hooks.NewAudit(auditPath)
	defer srv.audit.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token").WithAdminToken("test-admin-token")

	if _, err := cl.PatchConfigSandbox(context.Background(), api.ConfigSandboxPatchRequest{
		Scope:   "global",
		Backend: strPtr("auto"),
	}); err != nil {
		t.Fatalf("PatchConfigSandbox: %v", err)
	}
	srv.audit.Close() // flush before reading

	f, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	defer f.Close()

	var found bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("invalid audit line: %v", err)
		}
		if rec["phase"] != "config_patch" {
			continue
		}
		found = true
		if rec["path"] != "/config/sandbox" {
			t.Errorf("path = %v, want /config/sandbox", rec["path"])
		}
		if rec["remote"] == nil || rec["remote"] == "" {
			t.Errorf("remote address missing from config_patch record: %+v", rec)
		}
		applied, _ := rec["applied"].(map[string]any)
		if applied["Backend"] != "auto" {
			t.Errorf("applied.Backend = %v, want auto: %+v", applied["Backend"], rec)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("no config_patch audit record was written for the accepted PATCH")
	}
}
