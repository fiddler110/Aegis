package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
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
	if _, err := cl.ActivateSkill(ctx, meta.ID, "not-a-real-skill"); err == nil {
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

	if _, err := cl.ActivateSkill(ctx, meta.ID, "threat-modeling"); err != nil {
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

// TestActivateSkill_ReturnsSkillBody covers P36.1: activation hands the
// just-loaded skill body back to the caller so a slash command can prepend it
// deterministically to its message, instead of relying on the model calling
// the `skill` tool to fetch the top-level instructions itself.
func TestActivateSkill_ReturnsSkillBody(t *testing.T) {
	cl, _, cleanup := newSkillTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan", Workdir: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	body, err := cl.ActivateSkill(ctx, meta.ID, "threat-modeling")
	if err != nil {
		t.Fatalf("ActivateSkill: %v", err)
	}
	if !strings.Contains(body, "STRIDE") {
		t.Errorf("expected the returned skill body to mention STRIDE, got:\n%s", body)
	}
}

// TestActivateSkill_MaterializesIntoProjectWorkdir covers the fix: session-
// scoped activation (the path /threat-model and friends use) must
// materialize the builtin's files into the session's own project workdir,
// synchronously, before the activation request returns — so the very next
// tool call the model makes (read_file against a references/ or skeletons/
// asset) finds them already on disk.
func TestActivateSkill_MaterializesIntoProjectWorkdir(t *testing.T) {
	cl, _, cleanup := newSkillTestServer(t)
	defer cleanup()
	ctx := context.Background()
	workdir := t.TempDir()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan", Workdir: workdir})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := cl.ActivateSkill(ctx, meta.ID, "threat-modeling"); err != nil {
		t.Fatalf("ActivateSkill: %v", err)
	}

	asset := filepath.Join(workdir, ".aegis", "builtin-skills", "threat-modeling", "references", "stride.md")
	if _, err := os.Stat(asset); err != nil {
		t.Fatalf("expected threat-modeling's assets materialized into the session workdir, stat %s: %v", asset, err)
	}
}

// TestActivateSkill_RepeatedActivationDoesNotRewriteUnchangedFiles guards
// against a performance regression: /threat-model and friends call
// ActivateSkill on every invocation (internal/tui/slash.go), not just the
// first, so a blind overwrite on each call would churn mtimes and defeat
// Discover's signature-based cache every time.
func TestActivateSkill_RepeatedActivationDoesNotRewriteUnchangedFiles(t *testing.T) {
	cl, _, cleanup := newSkillTestServer(t)
	defer cleanup()
	ctx := context.Background()
	workdir := t.TempDir()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan", Workdir: workdir})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := cl.ActivateSkill(ctx, meta.ID, "threat-modeling"); err != nil {
		t.Fatalf("ActivateSkill: %v", err)
	}
	asset := filepath.Join(workdir, ".aegis", "builtin-skills", "threat-modeling", "references", "stride.md")
	info1, err := os.Stat(asset)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := cl.ActivateSkill(ctx, meta.ID, "threat-modeling"); err != nil {
		t.Fatalf("second ActivateSkill: %v", err)
	}
	info2, err := os.Stat(asset)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("expected mtime to stay stable across repeat activation, got %v -> %v", info1.ModTime(), info2.ModTime())
	}
}

// TestCreateSession_MaterializesConfigEnabledBuiltins covers the other
// enablement path: a builtin named in cfg.Skills.BuiltinEnabled (the
// config-driven /skills enable route) should be materialized into a new
// session's project workdir at session-creation time, without any explicit
// activation call.
func TestCreateSession_MaterializesConfigEnabledBuiltins(t *testing.T) {
	cl, srv, cleanup := newSkillTestServer(t)
	defer cleanup()
	srv.cfg.Skills.BuiltinEnabled = []string{"security-audit"}
	ctx := context.Background()
	workdir := t.TempDir()

	if _, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan", Workdir: workdir}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	asset := filepath.Join(workdir, ".aegis", "builtin-skills", "security-audit", "SKILL.md")
	if _, err := os.Stat(asset); err != nil {
		t.Fatalf("expected security-audit materialized into the new session's workdir, stat %s: %v", asset, err)
	}
}
