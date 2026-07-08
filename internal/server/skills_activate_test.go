package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newSkillTestServer is like newTestServer but wires a real `skill` tool
// backed by a materialized data dir, so activation can be verified end to end
// (system-prompt index + the skill tool actually loading the content).
func newSkillTestServer(t *testing.T) (*client.Client, *Server, func()) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if err := skills.MaterializeBuiltins(dataDir); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		DataDir:    dataDir,
	}
	reg := tool.NewRegistry()
	if err := reg.Register(builtin.NewSkillTool(t.TempDir(), dataDir, nil)); err != nil {
		t.Fatal(err)
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hello"}, reg)
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL).WithToken("test-token")
	return cl, srv, func() { ts.Close(); store.Close() }
}

func TestActivateSkill_UnknownName(t *testing.T) {
	cl, _, cleanup := newSkillTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := cl.ActivateSkill(ctx, meta.ID, "not-a-real-skill"); err == nil {
		t.Fatal("expected an error activating an unknown skill name")
	}
}

func TestActivateSkill_DormantUntilActivated(t *testing.T) {
	cl, srv, cleanup := newSkillTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Dormant by default: not in the system-prompt skills index, and the
	// skill tool refuses to load it.
	before := srv.effectiveSystem("base", meta.ID)
	if strings.Contains(before, "threat-modeling") {
		t.Errorf("expected threat-modeling to be dormant before activation:\n%s", before)
	}
	skillTool, _ := srv.sessionToolRegistry(meta.ID).Get("skill")
	res, _ := skillTool.Execute(ctx, mustJSON(t, map[string]string{"name": "threat-modeling"}))
	if !res.IsError {
		t.Fatal("expected the skill tool to reject a dormant skill before activation")
	}

	if err := cl.ActivateSkill(ctx, meta.ID, "threat-modeling"); err != nil {
		t.Fatalf("ActivateSkill: %v", err)
	}

	// Now active for this session: shows up in the index, and the skill tool
	// can load its full content.
	after := srv.effectiveSystem("base", meta.ID)
	if !strings.Contains(after, "threat-modeling") {
		t.Errorf("expected threat-modeling in the skills index after activation:\n%s", after)
	}
	skillTool, _ = srv.sessionToolRegistry(meta.ID).Get("skill")
	res, err = skillTool.Execute(ctx, mustJSON(t, map[string]string{"name": "threat-modeling"}))
	if err != nil || res.IsError {
		t.Fatalf("expected the skill tool to load threat-modeling after activation, got err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "STRIDE") {
		t.Errorf("expected the loaded skill content to mention STRIDE, got:\n%s", res.Content)
	}

	// Activation is session-scoped: a different session stays dormant.
	other, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	otherPrompt := srv.effectiveSystem("base", other.ID)
	if strings.Contains(otherPrompt, "threat-modeling") {
		t.Errorf("expected threat-modeling to stay dormant in an unrelated session:\n%s", otherPrompt)
	}
}
