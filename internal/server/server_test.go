package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"net/http"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/longmem"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/reqorigin"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/sysprompt"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// fixedAdapter returns a single text response regardless of input.
type fixedAdapter struct{ text string }

func (fixedAdapter) Name() string { return "fixed" }
func (a fixedAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: a.text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 2}}
	close(ch)
	return ch, nil
}

// zeroUsageAdapter simulates a local/Ollama model that reports no token
// usage at all, forcing the engine's estimation fallback (P25.5).
type zeroUsageAdapter struct{ text string }

func (zeroUsageAdapter) Name() string { return "zero-usage" }
func (a zeroUsageAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: a.text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	close(ch)
	return ch, nil
}

func newTestServer(t *testing.T) (*client.Client, func()) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hello from agent"}, tool.NewRegistry())
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	cl := client.New(ts.URL).WithToken("test-token")
	return cl, func() { ts.Close(); store.Close() }
}

func TestServerSessionLifecycle(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if meta.Mode != "build" {
		t.Errorf("mode = %q, want build", meta.Mode)
	}

	list, err := cl.ListSessions(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSessions: %v len=%d", err, len(list))
	}

	if err := cl.DeleteSession(ctx, meta.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	list, _ = cl.ListSessions(ctx)
	if len(list) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(list))
	}
}

// TestServerDeleteSessionClearsWorkdirAndSkillMaps is a regression for P26.2:
// handleDeleteSession only cleared sessionTools, leaking a sessionWorkdirs
// and sessionSkills entry per deleted session on a long-lived daemon.
func TestServerDeleteSessionClearsWorkdirAndSkillMaps(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: t.TempDir(), Origin: reqorigin.TUI})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	srv.activateSessionSkill(meta.ID, "content-review")

	if _, ok := srv.sess.workdirs.Load(meta.ID); !ok {
		t.Fatal("sessionWorkdirs not populated before delete; test setup is wrong")
	}
	if _, ok := srv.sess.skills.Load(meta.ID); !ok {
		t.Fatal("sessionSkills not populated before delete; test setup is wrong")
	}

	if err := cl.DeleteSession(ctx, meta.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, ok := srv.sess.workdirs.Load(meta.ID); ok {
		t.Error("sessionWorkdirs entry survived session delete (P26.2 leak)")
	}
	if _, ok := srv.sess.skills.Load(meta.ID); ok {
		t.Error("sessionSkills entry survived session delete (P26.2 leak)")
	}
}

func TestServerListTeammates(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"

	reg := swarm.NewRegistry()
	id := swarm.NewIdentity("explore-1", "default", "sess")
	reg.Add(id)
	reg.Update(id.AgentID, swarm.StatusDone, "found it")
	srv.swarmReg = reg

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")

	tms, err := cl.Teammates(context.Background())
	if err != nil {
		t.Fatalf("Teammates: %v", err)
	}
	if len(tms) != 1 {
		t.Fatalf("got %d teammates, want 1", len(tms))
	}
	if tms[0].AgentID != id.AgentID || tms[0].Status != "done" || tms[0].Summary != "found it" {
		t.Errorf("teammate = %+v", tms[0])
	}
}

func TestServerListTeammatesEmpty(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	tms, err := cl.Teammates(context.Background())
	if err != nil {
		t.Fatalf("Teammates: %v", err)
	}
	if len(tms) != 0 {
		t.Errorf("expected no teammates, got %d", len(tms))
	}
}

func TestServerMessageStreaming(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cl.PostMessage(ctx, meta.ID, "say hi")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	var text string
	var sawDone bool
	for ev := range ch {
		switch ev.Kind {
		case api.KindText:
			text += ev.Text
		case api.KindDone:
			sawDone = true
		case api.KindError:
			t.Fatalf("error event: %s", ev.Error)
		}
	}
	if text != "hello from agent" {
		t.Errorf("streamed text = %q", text)
	}
	if !sawDone {
		t.Error("did not receive done event")
	}

	// The exchange must have been persisted: user + assistant.
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 2 {
		t.Fatalf("persisted %d messages, want 2", len(sess.Messages))
	}
	if sess.Title == "" {
		t.Error("expected a derived title")
	}
	// Sanity: assistant message round-tripped as text.
	b, _ := json.Marshal(sess.Messages[1])
	if !json.Valid(b) {
		t.Error("assistant message did not serialize")
	}
}

func TestServerPersistsTurnTrace(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cl.PostMessage(ctx, meta.ID, "say hi")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	// Trace events must NOT leak to the SSE client.
	for ev := range ch {
		if string(ev.Kind) == "trace" {
			t.Errorf("trace event leaked to SSE client: %+v", ev)
		}
		if ev.Kind == api.KindError {
			t.Fatalf("error event: %s", ev.Error)
		}
	}

	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Traces) != 1 {
		t.Fatalf("persisted %d traces, want 1", len(sess.Traces))
	}
	tr := sess.Traces[0]
	if tr.InputTokens != 5 || tr.OutputTokens != 2 {
		t.Errorf("trace tokens = %d/%d, want 5/2", tr.InputTokens, tr.OutputTokens)
	}
	if tr.Model != "test" {
		t.Errorf("trace model = %q, want \"test\"", tr.Model)
	}
}

// TestSessionCostCapBlocksTurn verifies a session whose persisted cost has
// already reached the configured session_cap_usd is refused a new turn,
// before any model call is made (P9.5).
func TestSessionCostCapBlocksTurn(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Cost:       config.CostConfig{SessionCapUSD: 1.0},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hi"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddUsage(ctx, meta.ID, 100, 100, 1.5); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	_, err = cl.PostMessage(ctx, meta.ID, "hello")
	if err == nil {
		t.Fatal("expected PostMessage to fail once the session cost cap is reached")
	}
	if !strings.Contains(err.Error(), "session spend cap") {
		t.Errorf("error = %v, want mention of session spend cap", err)
	}
}

// TestDailyCostCapBlocksTurn verifies the cross-session daily cap refuses a
// new turn once today's accumulated spend reaches daily_cap_usd (P9.5).
func TestDailyCostCapBlocksTurn(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Cost:       config.CostConfig{DailyCapUSD: 1.0},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hi"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddDailyCost(ctx, 2.0); err != nil {
		t.Fatalf("AddDailyCost: %v", err)
	}

	_, err = cl.PostMessage(ctx, meta.ID, "hello")
	if err == nil {
		t.Fatal("expected PostMessage to fail once the daily cost cap is reached")
	}
	if !strings.Contains(err.Error(), "daily spend cap") {
		t.Errorf("error = %v, want mention of daily spend cap", err)
	}
}

// TestCostAlertThresholdFires verifies a KindCostAlert event is emitted the
// turn spend crosses alert_threshold of the session cap, using a priced model
// so the fixed-usage turn produces a non-zero cost (P9.5).
func TestCostAlertThresholdFires(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "claude-sonnet", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Cost:       config.CostConfig{SessionCapUSD: 0.00005, AlertThreshold: 0.5},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hi"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := cl.PostMessage(ctx, meta.ID, "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	var sawAlert bool
	for ev := range ch {
		if ev.Kind == api.KindCostAlert {
			sawAlert = true
		}
		if ev.Kind == api.KindError {
			t.Fatalf("error event: %s", ev.Error)
		}
	}
	if !sawAlert {
		t.Error("expected a cost_alert event once spend crossed the threshold")
	}
}

// TestSessionTokenCapBlocksTurn is the P10.5 counterpart to
// TestSessionCostCapBlocksTurn: unlike the dollar cap, the token cap must
// still work for a session whose usage was never priced (e.g. a local model),
// since AddUsage now records tokens regardless of estimation.
func TestSessionTokenCapBlocksTurn(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Cost:       config.CostConfig{SessionTokenCap: 100},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hi"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Zero cost mirrors an unpriced/local model's turns: tokens still count.
	if err := store.AddUsage(ctx, meta.ID, 80, 80, 0); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	_, err = cl.PostMessage(ctx, meta.ID, "hello")
	if err == nil {
		t.Fatal("expected PostMessage to fail once the session token cap is reached")
	}
	if !strings.Contains(err.Error(), "session token cap") {
		t.Errorf("error = %v, want mention of session token cap", err)
	}
}

// TestDailyTokenCapBlocksTurn is the P10.5 counterpart to
// TestDailyCostCapBlocksTurn.
func TestDailyTokenCapBlocksTurn(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Cost:       config.CostConfig{DailyTokenCap: 100},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "hi"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddDailyTokens(ctx, 150); err != nil {
		t.Fatalf("AddDailyTokens: %v", err)
	}

	_, err = cl.PostMessage(ctx, meta.ID, "hello")
	if err == nil {
		t.Fatal("expected PostMessage to fail once the daily token cap is reached")
	}
	if !strings.Contains(err.Error(), "daily token cap") {
		t.Errorf("error = %v, want mention of daily token cap", err)
	}
}

// TestDoneEventAndSessionMetaCarryEstimatedTokens is the P25.5 regression:
// previously the terminal "done" SSE event carried no token fields at all
// (in=0 out=0) for a provider that reports no usage (local/Ollama models),
// even though the TUI's live status bar showed non-zero estimated counts for
// the same run via the per-turn turn_done events. API clients (and the eval
// harness, which only reads the final done event) now see the same estimate,
// flagged tokens_estimated, and the session's persisted totals reflect it too.
func TestDoneEventAndSessionMetaCarryEstimatedTokens(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, zeroUsageAdapter{text: "hello from local model"}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := cl.PostMessage(ctx, meta.ID, "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	var doneEv *api.Event
	for ev := range ch {
		if ev.Kind == api.KindError {
			t.Fatalf("error event: %s", ev.Error)
		}
		if ev.Kind == api.KindDone {
			cp := ev
			doneEv = &cp
		}
	}
	if doneEv == nil {
		t.Fatal("no done event")
	}
	if doneEv.InputTokens == 0 || doneEv.OutputTokens == 0 {
		t.Errorf("done event tokens = in:%d out:%d, want both > 0", doneEv.InputTokens, doneEv.OutputTokens)
	}
	if !doneEv.TokensEstimated {
		t.Error("done event TokensEstimated should be true when the provider reported no usage")
	}

	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.InputTokens == 0 || sess.OutputTokens == 0 {
		t.Errorf("session totals = in:%d out:%d, want both > 0", sess.InputTokens, sess.OutputTokens)
	}
}

func TestServerHealthEndpoint(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	if err := cl.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

// TestHealthzDisclosesNothingBeyondReadiness is P81.19/FIND-19. /healthz is
// exempt from authMiddleware, so whatever it returns is readable by any
// process that can reach the loopback port with no credential at all. The
// payload is now a readiness verdict and nothing else. It exists so that *adding* a version
// string, a workspace path, a session or run count, a PID or a username to
// that payload fails here rather than shipping: each of those turns a
// negligible disclosure into a reconnaissance primitive for a local process
// deciding whether this box is worth attacking. The assertion is on the full
// decoded object, not a substring, precisely so a new field cannot slip
// through.
//
// Richer diagnostics belong on the authenticated GET /status
// (handleStatusInfo), which is where they already live — see
// TestServerStatusEndpoint below for the workspace path, the allowlist and
// the daily cost/token counters that /healthz deliberately omits.
func TestHealthzDisclosesNothingBeyondReadiness(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Default: "anthropic", Model: "test-model"},
		Permission: config.PermissionConfig{Mode: "plan"},
		Server:     config.ServerConfig{SessionWorkdirAllowlist: []string{"/srv/projects"}},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token-123"
	srv.workspace = "/srv/projects/secret-client"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()

	// No Authorization header at all: this is the unauthenticated view.
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unauthenticated /healthz status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}

	// The readiness verdict itself must be there — that is the endpoint's
	// entire job, and waitForDaemon depends on it.
	if body["status"] != "ok" {
		t.Errorf("status = %v, want \"ok\"", body["status"])
	}

	// And nothing else may be. Extend this set only with a deliberate decision
	// that the new field is safe to hand an unauthenticated local process; the
	// default answer is /status instead.
	//
	// This allowlist was briefly four entries. "model", "sandbox_fallback" and
	// "sandbox_fallback_reason" moved to the authenticated /status once it was
	// noticed that the last of them told an unauthenticated caller command
	// isolation had degraded to unsandboxed host execution — the single fact a
	// local attacker most benefits from, published on the one route that
	// requires no credential. api.HealthStatus carries the full reasoning.
	allowed := map[string]bool{
		"status": true,
	}
	var unexpected []string
	for k := range body {
		if !allowed[k] {
			unexpected = append(unexpected, k)
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("unauthenticated /healthz grew field(s) %v; richer diagnostics belong on the authenticated GET /status (FIND-19)", unexpected)
	}

	// Belt and braces against the specific values that would matter most:
	// the workspace path must not appear anywhere in the payload, even
	// embedded in a field this test's allowlist happens to permit.
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-client") {
		t.Errorf("/healthz payload leaks the workspace path: %s", raw)
	}

	// The counterpart half of the invariant: /status, which carries all of
	// that, is not reachable without the token.
	unauth, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	unauth.Body.Close()
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated /status status = %d, want 401", unauth.StatusCode)
	}
}

// TestServerStatusEndpoint is the P14.5 counterpart to
// TestServerHealthEndpoint: /status reports provider/model plus the
// cross-session daily spend the P9.5/P10.5 caps already track, which
// /healthz deliberately omits.
func TestServerStatusEndpoint(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Default: "anthropic", Model: "test-model"},
		Permission: config.PermissionConfig{Mode: "plan"},
		Cost:       config.CostConfig{DailyCapUSD: 5, DailyTokenCap: 1000},
		Server:     config.ServerConfig{SessionWorkdirAllowlist: []string{"/srv/projects"}},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	if err := store.AddDailyCost(ctx, 1.5); err != nil {
		t.Fatalf("AddDailyCost: %v", err)
	}
	if err := store.AddDailyTokens(ctx, 400); err != nil {
		t.Fatalf("AddDailyTokens: %v", err)
	}

	info, err := cl.StatusInfo(ctx)
	if err != nil {
		t.Fatalf("StatusInfo: %v", err)
	}
	if info.Provider != "anthropic" || info.Model != "test-model" {
		t.Errorf("provider/model = %q/%q, want anthropic/test-model", info.Provider, info.Model)
	}
	if info.DailyCostUSD != 1.5 {
		t.Errorf("DailyCostUSD = %v, want 1.5", info.DailyCostUSD)
	}
	if info.DailyTokens != 400 {
		t.Errorf("DailyTokens = %d, want 400", info.DailyTokens)
	}
	if info.DailyCapUSD != 5 || info.DailyTokenCap != 1000 {
		t.Errorf("caps = %v/%d, want 5/1000", info.DailyCapUSD, info.DailyTokenCap)
	}
	if info.AgentConcurrency != swarm.AdaptiveLimiterFloor {
		t.Errorf("AgentConcurrency = %d, want floor %d", info.AgentConcurrency, swarm.AdaptiveLimiterFloor)
	}
	if info.AgentConcurrencyMax != builtin.MaxParallelAgents {
		t.Errorf("AgentConcurrencyMax = %d, want %d", info.AgentConcurrencyMax, builtin.MaxParallelAgents)
	}
	if len(info.WorkdirAllowlist) != 1 || info.WorkdirAllowlist[0] != "/srv/projects" {
		t.Errorf("WorkdirAllowlist = %v, want [/srv/projects]", info.WorkdirAllowlist)
	}
	// P28.7: a cloud provider ("anthropic") with no API key resolved into
	// config (none set here) reports unreachable, mirroring `aegis doctor`'s
	// provider check for the same case.
	if info.ProviderReachable {
		t.Errorf("ProviderReachable = true, want false (no API key configured)")
	}
	if info.ProviderLatencyMS != 0 {
		t.Errorf("ProviderLatencyMS = %d, want 0 (unmeasured for a cloud provider)", info.ProviderLatencyMS)
	}
}

func TestServerGetSessionNotFound(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	_, err := cl.GetSession(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestServerAuthRequired(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token-123"

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()

	// No token → 401 on session endpoints.
	resp, err := http.Get(ts.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	// With token → 200.
	req, _ := http.NewRequest("GET", ts.URL+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status with token = %d, want 200", resp.StatusCode)
	}

	// Health bypasses auth.
	resp, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}
}

// TestServerInvalidAuthAttemptsLoggedAndCounted covers FIND-11: repeated
// invalid-bearer-token requests must still be rejected exactly as before
// (401, no behavior change for the reject path) but now also bump a
// process-wide counter and emit an auditable slog.Warn on a coarse cadence,
// without ever logging the attempted token value itself.
func TestServerInvalidAuthAttemptsLoggedAndCounted(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, logger, store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token-123"

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const attemptedWrongToken = "totally-wrong-guess-value"

	// One request with no Authorization header at all (never sends the
	// Bearer prefix), five with a wrong token — six invalid attempts total.
	resp, err := http.Get(ts.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status (missing header) = %d, want 401", resp.StatusCode)
	}

	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("GET", ts.URL+"/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+attemptedWrongToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status (wrong token, attempt %d) = %d, want 401", i, resp.StatusCode)
		}
	}

	// Counter reflects every failed attempt, not just the logged ones.
	if got := srv.invalidAuthAttempts.Load(); got != 6 {
		t.Errorf("invalidAuthAttempts = %d, want 6", got)
	}

	logged := logBuf.String()
	if strings.Contains(logged, attemptedWrongToken) {
		t.Errorf("log output must never contain the attempted token value, got: %s", logged)
	}
	if !strings.Contains(logged, "rejected request with invalid or missing bearer token") {
		t.Errorf("expected a warning log line for invalid auth attempts, got: %s", logged)
	}
	// invalidAuthLogEvery is 5, and the first attempt always logs, so with 6
	// total attempts we expect exactly two log lines: at cumulative_count=1
	// and cumulative_count=5.
	if !strings.Contains(logged, "cumulative_count=1") {
		t.Errorf("expected a log line at cumulative_count=1, got: %s", logged)
	}
	if !strings.Contains(logged, "cumulative_count=5") {
		t.Errorf("expected a log line at cumulative_count=5, got: %s", logged)
	}
	if n := strings.Count(logged, "rejected request with invalid or missing bearer token"); n != 2 {
		t.Errorf("expected exactly 2 log lines for 6 attempts at cadence 5, got %d in: %s", n, logged)
	}

	// A subsequent valid-token request still succeeds and does not bump the
	// counter or log anything further.
	req, _ := http.NewRequest("GET", ts.URL+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status with valid token = %d, want 200", resp.StatusCode)
	}
	if got := srv.invalidAuthAttempts.Load(); got != 6 {
		t.Errorf("invalidAuthAttempts after valid request = %d, want unchanged 6", got)
	}
}

func TestServerOriginBlocking(t *testing.T) {
	cl, cleanup := newTestServer(t)
	_ = cl
	defer cleanup()

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()

	// Non-loopback origin → 403.
	req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-loopback origin: status = %d, want 403", resp.StatusCode)
	}

	// Loopback origin → OK.
	req, _ = http.NewRequest("GET", ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "http://127.0.0.1:4127")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("loopback origin: status = %d, want 200", resp.StatusCode)
	}
}

func TestEffectiveSystemCombinesMemory(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.memory = memory.Sources{ProjectRoot: root, DataDir: filepath.Join(root, "data")}

	// With no memory files, effectiveSystem returns the base system plus the
	// platform block (which is always injected).
	got := srv.effectiveSystem("base prompt", "")
	if !strings.Contains(got, "base prompt") {
		t.Errorf("effectiveSystem missing base prompt: %q", got)
	}
	if !strings.Contains(got, "Execution Environment") {
		t.Errorf("effectiveSystem missing platform block: %q", got)
	}

	// Create a memory file and check it gets appended.
	if err := memory.Append(srv.memory.ProjectMemoryPath(), "test fact"); err != nil {
		t.Fatal(err)
	}
	got = srv.effectiveSystem("base prompt", "")
	if !strings.Contains(got, "base prompt") || !strings.Contains(got, "test fact") {
		t.Errorf("effectiveSystem didn't include memory: %q", got)
	}
}

// TestEffectiveSystem_localProfileTrimsPrompt covers P25.6(a)'s acceptance
// criteria: the local prompt profile (auto-detected from a loopback
// provider.base_url) omits a repo map larger than localRepoMapMaxBytes and
// produces a measurably shorter assembled system prompt than the default
// profile given the same base config, session state, and (large) repo map.
// A remote base_url must keep the full repo map — the trim is opt-in/
// localhost-triggered only, never applied globally.
func TestEffectiveSystem_localProfileTrimsPrompt(t *testing.T) {
	bigRepoMap := "<repo_map>\n" + strings.Repeat("x", localRepoMapMaxBytes+500) + "\n</repo_map>"

	newSrv := func(t *testing.T, baseURL string) *Server {
		t.Helper()
		root := t.TempDir()
		store, err := session.Open(filepath.Join(root, "s.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		cfg := &config.Config{
			Provider:   config.ProviderConfig{Model: "test", BaseURL: baseURL},
			Permission: config.PermissionConfig{Mode: "plan"},
		}
		srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
		srv.memory = memory.Sources{ProjectRoot: root, DataDir: filepath.Join(root, "data")}
		srv.repoMap = bigRepoMap
		// Stamp the staleness clock the way New does after its own load
		// (P66.20/PERF-04), or repoMapFor re-reads this hand-injected block off
		// a temp dir that was never indexed and finds nothing.
		srv.repoMapCheckedAt = time.Now()
		return srv
	}

	localSrv := newSrv(t, "http://localhost:11434")
	defaultSrv := newSrv(t, "https://api.anthropic.com")

	localOut := localSrv.effectiveSystem("base prompt", "")
	defaultOut := defaultSrv.effectiveSystem("base prompt", "")

	if strings.Contains(localOut, "<repo_map>") {
		t.Error("local profile: oversized repo map should be omitted, but is present")
	}
	if !strings.Contains(defaultOut, "<repo_map>") {
		t.Error("default profile: repo map should still be injected regardless of size")
	}
	if len(localOut) >= len(defaultOut) {
		t.Errorf("local profile prompt should be shorter than default: local=%d default=%d", len(localOut), len(defaultOut))
	}

	// Both profiles must still carry the two P25.6(b) shared rules.
	for _, out := range []string{localOut, defaultOut} {
		if !strings.Contains(out, "prefer local tools") {
			t.Error("prefer-local-tools rule missing from effectiveSystem output")
		}
		if !strings.Contains(out, "only what was explicitly asked") {
			t.Error("no-scope-creep rule missing from effectiveSystem output")
		}
	}
}

// localBasePromptCeilingTokens is the P62.6 budget: the estimated token cost of
// everything a local-profile session sends before the first user message —
// the assembled system prompt (persona + shared blocks + skills/repo-map/memory
// when present) plus the always-exposed tool schemas the request carries.
//
// This is a BUDGET, NOT A TARGET. It exists because the number is invisible
// otherwise: P62.6 measured a real local run at 7,119 provider-reported prompt
// tokens on the *trimmed* profile, which on a 16GB-VRAM box (an 8k-16k served
// window) is most of the window before any work happens, and nothing in the
// default suite noticed. TestEffectiveSystem_localProfileTrimsPrompt asserts the
// local profile is *smaller* than the default one; it says nothing about whether
// it is small enough.
//
// Measured 2026-08-10 against an empty workspace (no repo map, no memory, no
// skills enabled) on windows/amd64: 7,790 estimated tokens — 4,176 of assembled
// system prompt (of which the <deferred_tools> inventory alone is 2,953) and
// 3,614 of always-exposed tool schemas over 27 tools. That is the same shape as
// P62.6's live 7,119 provider-reported figure; tokenest is a heuristic, not a
// tokenizer, so the two are not expected to agree to the token.
//
// Re-measured 2026-08-14 at 4,907 after P62.6's first three fixes, and the
// ceiling lowered from 8,200 to match. Where the 2,883 went, in the order they
// were applied:
//
//   - 339 were never real. tokenest.Tools priced OutputSchema, which no adapter
//     puts on the wire. Fixing the instrument before measuring the optimization
//     is P62.4's lesson, applied here.
//   - 2,279 came out of <deferred_tools>, which had been printing each unloaded
//     tool's full manual. It now prints tool.Summarize's one line: 2,953 → 674
//     at 26 tools.
//   - 265 more came out of the same block when the local profile stopped
//     registering the team/cron/entity families (26 deferred tools → 13).
//
// No exposed tool schema was touched, and no tool became unreachable: the
// omitted families come back via tools.families, and every summarized tool is
// still found by tool_search, which matches the full description held in the
// registry.
//
// Re-measured 2026-08-14 at 4,317 after P62.9 took the exposed-schema half,
// and the ceiling lowered from 5,200 to match. Two changes, 590 tokens:
//
//   - 420 out of the three shared prose blocks (1,001 → 581), which now have
//     local variants: the same rules in fewer words, plus the one genuine
//     duplication between the platform and tool-use blocks. No rule was
//     dropped — see the comment above persona.PlatformBlockFor for why that
//     line is where it is, and TestLocalBlocksKeepEveryRule for what holds it.
//   - 185 from deferring edit_file under the local profile (schemas 3,275 →
//     3,090 over 26 tools rather than 27), less the ~16 its summary line costs
//     in <deferred_tools>. See builtin.Register for why that tool and not the
//     handle-based ones it is more expensive than.
//
// Both are behaviour changes on the local profile, unlike P62.6's three, and
// the live-tier confirmation the item asks for (TestLiveWorkflow) is still
// outstanding — the numbers above are what the change costs, not evidence that
// it is free.
//
// The ceiling is the measured value plus ~5% headroom, so ordinary prose edits
// to a persona block or a tool description do not trip it but a new
// always-exposed tool or a new injected block does. It is an upper bound across
// platforms: PlatformBlock is largest on Windows (the PowerShell command table),
// so linux/darwin measure a few hundred tokens lower and pass with more room.
//
// When it trips: run TestBasePromptComposition_localProfile with -v first. It
// prints the per-component and per-tool-schema breakdown, which tells you
// whether the growth is a block you meant to add or a tool schema that grew by
// accident. If the growth is deliberate, move this number and say in the commit
// what was added and what it bought — a silent bump turns the budget back into
// the invisible number it was written to make visible.
const localBasePromptCeilingTokens = 4550

// localInjectedPromptCeilingTokens is the second half of the budget, added by
// P66.7. localBasePromptCeilingTokens above measures a bare workspace, where
// every project-varying component is empty — which made it structurally blind
// to the one block that actually grows with a project (LLM-01: CLAUDE.md was
// injected with no cap at all). This ceiling covers the same prompt assembled
// over a workspace that *has* a real, over-cap CLAUDE.md, so the number the
// suite pins is the prefix a real local session sends.
//
// It is the base ceiling plus what localContextFilesMaxBytes is allowed to
// cost: 8,000 bytes at tokenest's ASCII rate is ~2,000 tokens, and the `#
// <name>` header plus the trust.Wrap provenance envelope add ~100 more. The
// gap between the two constants is therefore the cap, expressed in tokens —
// if it ever exceeds that, the cap is not binding and this test says so.
//
// Raise this only together with localContextFilesMaxBytes, and for the same
// reason; raising it alone means the cap stopped working.
const localInjectedPromptCeilingTokens = localBasePromptCeilingTokens + 2100

// TestEffectiveSystem_localProfileBudget is P62.6's regression assertion,
// extended by P66.7. See localBasePromptCeilingTokens and
// localInjectedPromptCeilingTokens for what the numbers mean and what to do
// when this fails.
//
// Two fixtures, because they answer different questions. The bare workspace
// pins the prompt the project itself authors (personas, deferred-tools block,
// tool schemas). The context-file workspace pins what a real project adds on
// top, and is the arm that fails if the context-file cap is removed: the
// fixture is deliberately larger than localContextFilesMaxBytes, so an
// uncapped injection lands well over the second ceiling.
func TestEffectiveSystem_localProfileBudget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		contextFile bool
		ceiling     int
	}{
		{"bare workspace", false, localBasePromptCeilingTokens},
		{"with context files", true, localInjectedPromptCeilingTokens},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, reg := newLocalProfileServer(t, tc.contextFile)
			system := srv.effectiveSystem(personaBaseSystem(t), "")
			schemas := reg.Schemas()

			systemTokens := tokenest.Estimate(system)
			schemaTokens := tokenest.Tools(schemas)
			total := systemTokens + schemaTokens

			t.Logf("local-profile prompt: system=%d tokens (%d bytes), tool schemas=%d tokens (%d tools), total=%d tokens (ceiling %d)",
				systemTokens, len(system), schemaTokens, len(schemas), total, tc.ceiling)

			if total > tc.ceiling {
				t.Errorf("local-profile prompt is %d estimated tokens, over the %d-token budget (system=%d, tool schemas=%d over %d tools).\n"+
					"Run `go test ./internal/server/ -run TestBasePromptComposition_localProfile -v` for the per-component breakdown.\n"+
					"If the growth is deliberate, raise the ceiling and note what was added.",
					total, tc.ceiling, systemTokens, schemaTokens, len(schemas))
			}
		})
	}
}

// TestEffectiveSystem_contextFilesAreCapped is P66.7's direct assertion, kept
// separate from the ceiling test so the failure message says which of the two
// things broke. It pins three properties of the cap: it binds under the local
// profile, it truncates rather than dropping (a context file is instructions,
// and silently losing them is the failure this cap must not introduce), and it
// does not apply off the local profile.
func TestEffectiveSystem_contextFilesAreCapped(t *testing.T) {
	srv, _ := newLocalProfileServer(t, true)
	capped := srv.memory.LoadContextCapped(localContextFilesMaxBytes)
	uncapped := srv.memory.LoadContext()

	if len(uncapped) <= localContextFilesMaxBytes {
		t.Fatalf("fixture is %d bytes, not over the %d-byte cap — the test would pass with the cap removed", len(uncapped), localContextFilesMaxBytes)
	}
	if len(capped) >= len(uncapped) {
		t.Errorf("context files were not capped: %d bytes capped vs %d uncapped", len(capped), len(uncapped))
	}
	// The header and provenance envelope ride outside the content budget, so
	// allow a small margin over the cap itself rather than pinning it exactly.
	if over := len(capped) - localContextFilesMaxBytes; over > 400 {
		t.Errorf("capped context files are %d bytes, %d over the %d-byte cap — more than the header/envelope overhead accounts for", len(capped), over, localContextFilesMaxBytes)
	}
	if !strings.Contains(capped, "[truncated:") {
		t.Error("capped context files carry no truncation notice; a model cannot tell it is reading a partial instruction file")
	}
	if !strings.Contains(capped, "# CLAUDE.md") {
		t.Error("capped context files dropped CLAUDE.md entirely; the posture is head-kept truncation, not omission")
	}
	// The assembled prompt must carry the capped form, not the raw file.
	system := srv.effectiveSystem(personaBaseSystem(t), "")
	if !strings.Contains(system, "[truncated:") {
		t.Error("effectiveSystem injected the uncapped context files under the local profile")
	}

	// Off the local profile the cap does not apply, exactly as with the repo
	// map: the whole point of the local profile is that it is the constrained
	// one.
	srv.cfg.Provider.BaseURL = "https://api.anthropic.com"
	if srv.cfg.Provider.LocalPromptProfile() {
		t.Fatal("remote base_url still detects as the local prompt profile; the rest of this check is meaningless")
	}
	if remote := srv.effectiveSystem(personaBaseSystem(t), ""); strings.Contains(remote, "[truncated:") {
		t.Error("the context-file cap applied off the local prompt profile")
	}
}

// TestBasePromptComposition_localProfile is P62.6's measurement harness: it
// prints where the local-profile base prompt's tokens actually go, per assembled
// block and per tool schema. It asserts only the things that would make the
// table a lie — that the components sum to the assembled prompt, and that the
// schema block is actually populated — because its job is to report a number,
// not to pin one (TestEffectiveSystem_localProfileBudget does that).
//
// Run with -v to see the table.
func TestBasePromptComposition_localProfile(t *testing.T) {
	srv, reg := newLocalProfileServer(t, true)
	base := personaBaseSystem(t)
	system := srv.effectiveSystem(base, "")
	schemas := reg.Schemas()

	// Components, in effectiveSystem's assembly order. Recomputed from the same
	// sources rather than parsed out of the joined string, then cross-checked
	// against the assembled length below so the table cannot silently drift from
	// what effectiveSystem actually emits.
	workdir := srv.workdirFor("")
	type component struct {
		name string
		text string
	}
	components := []component{
		{"persona system prompt", base},
		{"tool-use block", persona.ToolUseBlockFor(true)},
		{"completing-tasks block", persona.CompletingTasksBlockFor(true)},
		{"platform block", persona.PlatformBlockFor(true)},
		{"memory: context files", srv.memory.LoadContextCapped(localContextFilesMaxBytes)},
		{"memory: project/user", srv.memory.Load()},
		{"<skills_available>", skills.BuildIndex(workdir, srv.cfg.DataDir, srv.sessionEnabledSkills(""))},
		{"<repo_map>", srv.repoMapFor(workdir)},
		{fmt.Sprintf("<deferred_tools> (%d tools)", len(reg.Deferred())), sysprompt.DeferredToolsBlock(reg)},
		{"debate block", sysprompt.DebateIntegrationBlock(srv.cfg.Security.Debate)},
	}

	// Assembly cross-check: effectiveSystem joins the non-empty parts with a
	// blank line. If this fails, a block was added to effectiveSystem and not to
	// the table above, and every percentage below is wrong.
	want, nonEmpty := 0, 0
	for _, c := range components {
		if c.text == "" {
			continue
		}
		want += len(c.text)
		nonEmpty++
	}
	if nonEmpty > 1 {
		want += 2 * (nonEmpty - 1)
	}
	if want != len(system) {
		t.Errorf("component table does not account for the assembled prompt: components=%d bytes, effectiveSystem=%d bytes — a block was added to effectiveSystem without being added here", want, len(system))
	}

	// OutputSchema is excluded from the byte column for the same reason
	// tokenest.Tools excludes it from the token column (P62.6): no adapter puts
	// it on the wire, so counting it would make the bytes and the tokens
	// disagree about what a request actually carries.
	schemaBytes := 0
	for _, s := range schemas {
		schemaBytes += len(s.Name) + len(s.Description) + len(s.InputSchema)
	}
	if len(schemas) == 0 {
		t.Fatal("no tool schemas exposed; the measurement would be meaningless")
	}

	systemTokens := tokenest.Estimate(system)
	schemaTokens := tokenest.Tools(schemas)
	totalTokens := systemTokens + schemaTokens
	totalBytes := len(system) + schemaBytes

	pct := func(n, of int) float64 {
		if of == 0 {
			return 0
		}
		return 100 * float64(n) / float64(of)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\nP62.6 local-profile base prompt composition (workspace with an over-cap CLAUDE.md, P66.7)\n\n")
	fmt.Fprintf(&b, "%-28s %10s %10s %8s\n", "component", "bytes", "est.tokens", "%tokens")
	for _, c := range components {
		tk := tokenest.Estimate(c.text)
		fmt.Fprintf(&b, "%-28s %10d %10d %7.1f%%\n", c.name, len(c.text), tk, pct(tk, totalTokens))
	}
	fmt.Fprintf(&b, "%-28s %10d %10d %7.1f%%\n", fmt.Sprintf("tool schemas (%d tools)", len(schemas)), schemaBytes, schemaTokens, pct(schemaTokens, totalTokens))
	fmt.Fprintf(&b, "%-28s %10d %10d %7.1f%%\n", "TOTAL", totalBytes, totalTokens, 100.0)

	// Per-tool cost, most expensive first: the schema block is the obvious
	// suspect, and "which tools" is the question any fix has to answer.
	type toolCost struct {
		name   string
		bytes  int
		tokens int
	}
	costs := make([]toolCost, 0, len(schemas))
	for _, s := range schemas {
		costs = append(costs, toolCost{
			name:   s.Name,
			bytes:  len(s.Name) + len(s.Description) + len(s.InputSchema),
			tokens: tokenest.Tools([]provider.ToolSchema{s}),
		})
	}
	sort.Slice(costs, func(i, j int) bool {
		if costs[i].tokens != costs[j].tokens {
			return costs[i].tokens > costs[j].tokens
		}
		return costs[i].name < costs[j].name
	})
	// The deferred inventory is measured too, and separately: it is the price
	// paid for *not* exposing a schema, and if it approaches what a schema costs
	// then deferral has stopped being a saving — which is the state P62.6 found
	// it in. The line is measured as deferredToolsBlock actually emits it (name
	// + Summary), and the tool's full Description is shown alongside so a
	// summary that has drifted into uselessness is visible here rather than only
	// in a live run. Sorted by what the prompt pays, not by what the manual
	// weighs.
	deferredInfos := reg.Deferred()
	deferredLine := func(d tool.Info) string { return "- " + d.Name + ": " + d.Summary + "\n" }
	sort.Slice(deferredInfos, func(i, j int) bool {
		li, lj := len(deferredLine(deferredInfos[i])), len(deferredLine(deferredInfos[j]))
		if li != lj {
			return li > lj
		}
		return deferredInfos[i].Name < deferredInfos[j].Name
	})
	fmt.Fprintf(&b, "\nmost expensive <deferred_tools> lines\n\n")
	fmt.Fprintf(&b, "%-4s %-24s %10s %10s %10s\n", "#", "tool", "bytes", "est.tokens", "full.desc")
	for i, d := range deferredInfos {
		if i >= 10 {
			break
		}
		line := deferredLine(d)
		fmt.Fprintf(&b, "%-4d %-24s %10d %10d %10d\n", i+1, d.Name, len(line), tokenest.Estimate(line), len(d.Description))
	}

	fmt.Fprintf(&b, "\nmost expensive tool schemas\n\n")
	fmt.Fprintf(&b, "%-4s %-24s %10s %10s %8s\n", "#", "tool", "bytes", "est.tokens", "%tokens")
	for i, c := range costs {
		if i >= 15 {
			break
		}
		fmt.Fprintf(&b, "%-4d %-24s %10d %10d %7.1f%%\n", i+1, c.name, c.bytes, c.tokens, pct(c.tokens, totalTokens))
	}
	t.Log(b.String())
}

// newLocalProfileServer builds a Server wired the way a local-model session is:
// a loopback base_url (which LocalPromptProfile auto-detects), the real builtin
// tool registry registered with LocalProfile set, and a workspace with no repo
// map, memory, or skills so nothing incidental inflates the measurement.
//
// contextFile writes a realistic, deliberately over-cap CLAUDE.md into the
// workspace (P66.7). With it false the fixture is the bare one P62.6 measured,
// where every project-varying component is empty — the state that made the
// budget test blind to LLM-01.
func newLocalProfileServer(t *testing.T, contextFile bool) (*Server, *tool.Registry) {
	t.Helper()
	root := t.TempDir()
	if contextFile {
		writeContextFileFixture(t, root)
	}
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", BaseURL: "http://localhost:11434"},
		Permission: config.PermissionConfig{Mode: "build"},
		DataDir:    filepath.Join(root, "data"),
	}
	if !cfg.Provider.LocalPromptProfile() {
		t.Fatal("loopback base_url should auto-detect as the local prompt profile")
	}
	// The tool surface has to match what the daemon actually registers, or the
	// measurement misses whole families of tools: New() passes a task manager,
	// cron scheduler, todo list, team task list, knowledge store and long-term
	// memory store, and each one contributes tools to the exposed set or to the
	// deferred block. Everything below is wired the same way New() wires it,
	// sharing the session DB. Only the sandbox, LSP manager and toolpath
	// resolver are left nil — none of them changes a schema, and probing for a
	// container runtime does not belong in the default suite.
	taskStore, err := task.NewStore(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	cronStore, err := cron.NewStore(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	teamTasks, err := swarm.NewTaskList(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	knowledgeStore, err := knowledge.Open(root, cfg.KnowledgeDBPath(root), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { knowledgeStore.Close() })
	longMemStore, err := longmem.Open(filepath.Base(root), cfg.LongMemDBPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { longMemStore.Close() })

	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{
		Root:         root,
		DataDir:      cfg.DataDir,
		Tasks:        task.NewManager(taskStore, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Cron:         cron.NewScheduler(cronStore, nil, slog.New(slog.NewTextHandler(io.Discard, nil))),
		FileTracker:  filetracker.New(),
		TodoList:     builtin.NewTodoList(),
		TeamTasks:    teamTasks,
		MailboxRoot:  swarm.MailboxRoot(cfg.DataDir),
		Knowledge:    knowledgeStore,
		LongMem:      longMemStore,
		LocalProfile: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, reg)
	srv.workspace = root
	srv.memory = memory.Sources{ProjectRoot: root, DataDir: cfg.DataDir}
	return srv, reg
}

// contextFileFixtureBytes is the size writeContextFileFixture targets. It is
// this repository's own CLAUDE.md rounded up — a curated, actively trimmed
// file, not a pathological one — and it is over localContextFilesMaxBytes,
// which is the point: a fixture under the cap would leave the budget test as
// blind to the cap's removal as the bare fixture was to LLM-01.
const contextFileFixtureBytes = 12000

// writeContextFileFixture writes a CLAUDE.md of roughly contextFileFixtureBytes
// into root. The content is shaped like a real one — a build section, a testing
// section, an architecture table and a run of invariant paragraphs — because
// the measurement is token-density-sensitive and a block of filler characters
// would price differently from English prose with code spans in it.
func writeContextFileFixture(t *testing.T, root string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# CLAUDE.md\n\nGuidance for agents working in this repository.\n\n" +
		"## Build & Run\n\n```bash\ngo build -o ./app ./cmd/app\ngo run ./cmd/app\n```\n\n" +
		"## Testing\n\n```bash\ngo test ./...\ngo test -race ./...\n```\n\n" +
		"## Architecture\n\n| Package | Role |\n|---------|------|\n" +
		"| `internal/engine` | Agent loop: model turns, tool dispatch, compaction, budgets |\n" +
		"| `internal/server` | HTTP daemon; wires sessions, tools, permissions, personas |\n" +
		"| `internal/tool` | Tool interface and registry (register/expose separation) |\n\n" +
		"## Invariants worth knowing before you edit\n\n")
	for i := 1; b.Len() < contextFileFixtureBytes; i++ {
		fmt.Fprintf(&b, "- **Invariant %d.** The %s path holds a property that a second call site can "+
			"silently bypass, so `internal/pkg%d` pins it with a test rather than a comment. Changing "+
			"the shape here means changing `Registry.Clone()` and the %d call sites that depend on it; "+
			"raising the bound is allowed, raising it silently is not. See docs/notes%d.md for the "+
			"measurement this number came from and what it bought.\n", i, []string{"read", "write", "dispatch", "compaction"}[i%4], i, 10+i, i)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// personaBaseSystem returns the system prompt a session gets by default — the
// "general" built-in persona, which is what handleCreateSession falls back to.
func personaBaseSystem(t *testing.T) string {
	t.Helper()
	p, ok := persona.Get("general")
	if !ok {
		t.Fatal(`built-in persona "general" not found`)
	}
	return p.System
}

// TestEffectiveSystem_ByteStable is P39.1: effectiveSystem's assembly (persona
// blocks, memory, skills, repo map, deferred-tools, debate block) must render
// byte-identically across calls given unchanged inputs. This is the
// determinism the KV-cache-reuse story for local models (P35.4, P35.9) relies
// on — a non-deterministic system prompt silently defeats prefill reuse on
// every turn.
func TestEffectiveSystem_ByteStable(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.memory = memory.Sources{ProjectRoot: root, DataDir: filepath.Join(root, "data")}
	if err := memory.Append(srv.memory.ProjectMemoryPath(), "test fact"); err != nil {
		t.Fatal(err)
	}

	got1 := srv.effectiveSystem("base prompt", "")
	got2 := srv.effectiveSystem("base prompt", "")
	if got1 != got2 {
		t.Errorf("effectiveSystem not byte-stable across calls with unchanged inputs:\n--- call 1 ---\n%s\n--- call 2 ---\n%s", got1, got2)
	}
}

// TestPromptSections_StabilityInvariant is P67.2, the stability analogue of
// TestEffectiveSystem_localProfileBudget's size ceiling. The size test says the
// prefix is not too big; this one says it does not move. A block that varies
// turn to turn — a timestamp, a running cost, a count that tracks session state
// — breaks the provider's prompt cache on every turn and is visible only as
// unexplained prefill cost, so every section is computed twice and any
// difference that was not declared with volatileSection fails.
//
// The fixture is the local-profile server with context files, because that is
// the arm where every section is non-empty; a section that renders "" in a bare
// workspace cannot be caught differing.
func TestPromptSections_StabilityInvariant(t *testing.T) {
	srv, _ := newLocalProfileServer(t, true)
	sections := srv.promptSections(personaBaseSystem(t), "")
	if len(sections) == 0 {
		t.Fatal("no prompt sections; the invariant would hold vacuously")
	}

	seen := make(map[string]bool, len(sections))
	for _, sec := range sections {
		if sec.name == "" {
			t.Error("prompt section with an empty name; the failure messages here name sections, and an anonymous one is what P67.2 removed")
		}
		if seen[sec.name] {
			t.Errorf("duplicate prompt section name %q — the memo is keyed on the name, so two sections sharing one would serve each other's text", sec.name)
		}
		seen[sec.name] = true
		if sec.build == nil {
			t.Fatalf("prompt section %q has no builder", sec.name)
		}
		if sec.volatile && strings.TrimSpace(sec.rationale) == "" {
			t.Errorf("volatile prompt section %q carries no justification", sec.name)
		}
		if !sec.volatile && sec.rationale != "" {
			t.Errorf("stable prompt section %q carries a volatility justification; it is either volatile or it is not", sec.name)
		}

		first := sec.build()
		second := sec.build()
		if first == second || sec.volatile {
			continue
		}
		t.Errorf("prompt section %q is not stable across two computations but was not declared volatile.\n"+
			"It is memoized for the life of the conversation, so this serves stale text as well as costing cache misses.\n"+
			"Either make it deterministic, or declare it with volatileSection and say in one sentence why it must recompute per turn.\n"+
			"--- first ---\n%s\n--- second ---\n%s", sec.name, first, second)
	}
}

// TestPromptSectionCache_MemoizesStablePerSession pins the two halves of P67.2's
// caching contract that the stability test cannot see: a stable section is built
// once per conversation, and the memo is keyed per session — a shared one would
// hand session B the workdir, skills and tool inventory of session A.
func TestPromptSectionCache_MemoizesStablePerSession(t *testing.T) {
	srv, _ := newLocalProfileServer(t, false)

	stableBuilds := 0
	stable := stableSection("test: stable", func() string {
		stableBuilds++
		return "stable text"
	})
	if got := srv.sectionText("sess-1", stable, true); got != "stable text" {
		t.Fatalf("sectionText = %q, want %q", got, "stable text")
	}
	if got := srv.sectionText("sess-1", stable, true); got != "stable text" {
		t.Fatalf("cached sectionText = %q, want %q", got, "stable text")
	}
	if stableBuilds != 1 {
		t.Errorf("stable section built %d times for one session, want 1 (it is not memoized)", stableBuilds)
	}
	srv.sectionText("sess-2", stable, true)
	if stableBuilds != 2 {
		t.Errorf("stable section built %d times across two sessions, want 2 (the memo is not keyed per session)", stableBuilds)
	}
	// The profile is part of the key too: the persona blocks branch on it.
	srv.sectionText("sess-1", stable, false)
	if stableBuilds != 3 {
		t.Errorf("stable section built %d times, want 3 — the local-profile flag is not part of the cache key", stableBuilds)
	}

	volatileBuilds := 0
	vol := volatileSection("test: volatile", "test fixture", func() string {
		volatileBuilds++
		return "volatile text"
	})
	srv.sectionText("sess-1", vol, true)
	srv.sectionText("sess-1", vol, true)
	if volatileBuilds != 2 {
		t.Errorf("volatile section built %d times, want 2 (it was memoized despite being declared volatile)", volatileBuilds)
	}
}

// TestVolatileSectionRequiresJustification: the required-argument half of P67.2
// is only real if an empty string is rejected. It panics rather than returning
// an error because the section list is constructed on every effectiveSystem
// call — there is no caller in a position to handle it.
func TestVolatileSectionRequiresJustification(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("volatileSection accepted an empty justification")
		}
	}()
	volatileSection("test: unjustified", "   ", func() string { return "" })
}

// TestEffectiveSystem_DeferredToolsOrderIndependent is P39.1's map-iteration
// regression guard: tool.Registry.Deferred() ranges a Go map (randomized
// iteration order) and relies entirely on its trailing sort.Slice for stable
// output. Registering the same tools in reverse order across two registries
// must still produce byte-identical deferredToolsBlock output — this is the
// test that would actually catch a regression if that sort were ever removed.
func TestEffectiveSystem_DeferredToolsOrderIndependent(t *testing.T) {
	regAB := tool.NewRegistry()
	if err := regAB.RegisterDeferred(&preloadFakeTool{name: "alpha_tool"}); err != nil {
		t.Fatal(err)
	}
	if err := regAB.RegisterDeferred(&preloadFakeTool{name: "beta_tool"}); err != nil {
		t.Fatal(err)
	}

	regBA := tool.NewRegistry()
	if err := regBA.RegisterDeferred(&preloadFakeTool{name: "beta_tool"}); err != nil {
		t.Fatal(err)
	}
	if err := regBA.RegisterDeferred(&preloadFakeTool{name: "alpha_tool"}); err != nil {
		t.Fatal(err)
	}

	outAB := sysprompt.DeferredToolsBlock(regAB)
	outBA := sysprompt.DeferredToolsBlock(regBA)
	if outAB != outBA {
		t.Errorf("deferredToolsBlock depends on registration order:\n--- registered A,B ---\n%s\n--- registered B,A ---\n%s", outAB, outBA)
	}
}

func TestAuditCloseCleanup(t *testing.T) {
	dir := t.TempDir()
	a := hooks.NewAudit(filepath.Join(dir, "audit.jsonl"))
	a.PreToolUse(context.Background(), "test_tool", nil)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Double-close should be safe.
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestDeriveTitle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello world", "hello world"},
		{"  spaced  out  ", "spaced  out"},
		{strings.Repeat("x", 100), strings.Repeat("x", 60) + "…"},
	}
	for _, tt := range tests {
		got := deriveTitle(tt.in)
		if got != tt.want {
			t.Errorf("deriveTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPatchSession(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}

	// Patch mode.
	mode := "build"
	updated, err := cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Mode: &mode})
	if err != nil {
		t.Fatalf("UpdateSession mode: %v", err)
	}
	if updated.Mode != "build" {
		t.Errorf("mode = %q, want build", updated.Mode)
	}

	// Patch system via legacy persona: prefix — routes through the full
	// persona switch, so the persona column updates too.
	system := "persona:security"
	_, err = cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{System: &system})
	if err != nil {
		t.Fatalf("UpdateSession persona: %v", err)
	}
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sess.System, "SECURITY PLATFORM ARCHITECT") {
		t.Errorf("system not updated to security persona, got %q...", sess.System[:50])
	}
	if sess.Persona != "security" {
		t.Errorf("persona = %q, want security (legacy prefix must persist the persona name)", sess.Persona)
	}

	// Patch via the Persona field.
	name := "developer"
	if _, err = cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Persona: &name}); err != nil {
		t.Fatalf("UpdateSession persona field: %v", err)
	}
	sess, err = cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Persona != "developer" {
		t.Errorf("persona = %q, want developer", sess.Persona)
	}
	if !strings.Contains(sess.System, "SOFTWARE DEVELOPER") {
		t.Errorf("system not updated to developer persona, got %q...", sess.System[:50])
	}

	// Unknown persona rejected.
	unknown := "does-not-exist"
	if _, err = cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Persona: &unknown}); err == nil {
		t.Error("expected error for unknown persona")
	}

	// Invalid mode rejected.
	bad := "invalid"
	_, err = cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Mode: &bad})
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

// TestPatchSessionModel is the P14.7 regression: a per-session model override
// round-trips through PATCH /sessions/{id} and GET /sessions/{id} (both the
// full Session and the SessionMeta returned by the patch itself), and an
// empty string clears it back to "".
func TestPatchSessionModel(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "" {
		t.Errorf("new session model = %q, want empty", meta.Model)
	}

	model := "claude-opus-4-8"
	updated, err := cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Model: &model})
	if err != nil {
		t.Fatalf("UpdateSession model: %v", err)
	}
	if updated.Model != model {
		t.Errorf("patch response model = %q, want %q", updated.Model, model)
	}
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Model != model {
		t.Errorf("GetSession model = %q, want %q", sess.Model, model)
	}

	// Clearing back to "" reverts to the persona/global default.
	empty := ""
	updated, err = cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Model: &empty})
	if err != nil {
		t.Fatalf("UpdateSession clear model: %v", err)
	}
	if updated.Model != "" {
		t.Errorf("cleared model = %q, want empty", updated.Model)
	}
}

// TestPersonaHotReload verifies persona files added while the daemon runs
// become visible without a restart, and that switching to one carries its
// full profile (persona name persisted on the session).
func TestPersonaHotReload(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{text: "ok"}, tool.NewRegistry())
	srv.authToken = "test-token"
	dir := t.TempDir()
	srv.personaDirs = []string{dir}
	// Reset the package-global loaded persona set after the test.
	t.Cleanup(func() { persona.Refresh("", false) })

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	hasPersona := func(name string) bool {
		personas, err := cl.ListPersonas(ctx)
		if err != nil {
			t.Fatalf("ListPersonas: %v", err)
		}
		for _, p := range personas {
			if p.Name == name {
				return true
			}
		}
		return false
	}

	if hasPersona("hot-added") {
		t.Fatal("hot-added persona present before the file was written")
	}

	// Drop a persona file while the daemon is running.
	content := "---\ndescription: added at runtime\n---\nYou are the hot-added persona."
	if err := os.WriteFile(filepath.Join(dir, "hot-added.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasPersona("hot-added") {
		t.Fatal("persona file added at runtime not visible via /personas")
	}

	// Switch a session to it and confirm the full profile landed.
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	name := "hot-added"
	if _, err := cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Persona: &name}); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	sess, err := cl.GetSession(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Persona != "hot-added" || !strings.Contains(sess.System, "hot-added persona") {
		t.Errorf("persona=%q system=%q", sess.Persona, sess.System)
	}

	// Deleting the file drops the persona on the next refresh.
	if err := os.Remove(filepath.Join(dir, "hot-added.md")); err != nil {
		t.Fatal(err)
	}
	if hasPersona("hot-added") {
		t.Error("deleted persona file still listed")
	}
}

func TestListPersonas(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	personas, err := cl.ListPersonas(context.Background())
	if err != nil {
		t.Fatalf("ListPersonas: %v", err)
	}
	if len(personas) < 10 {
		t.Errorf("expected at least 10 personas, got %d", len(personas))
	}
	found := false
	for _, p := range personas {
		if p.Name == "security" {
			found = true
		}
	}
	if !found {
		t.Error("security persona not found")
	}
}

func TestMemoryEndpoints(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:    filepath.Join(root, "data"),
		Provider:   config.ProviderConfig{Model: "test"},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.memory = memory.Sources{ProjectRoot: root, DataDir: cfg.DataDir}
	srv.workspace = root
	srv.authToken = "test-token"

	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); store.Close() }()
	cl := client.New(ts.URL).WithToken("test-token")

	ctx := context.Background()

	// Initially empty.
	mem, err := cl.GetMemory(ctx)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if mem.ProjectMemory != "" || mem.UserMemory != "" {
		t.Errorf("expected empty memory, got project=%q user=%q", mem.ProjectMemory, mem.UserMemory)
	}

	// Append.
	if err := cl.AppendMemory(ctx, api.AppendMemoryRequest{Entry: "test fact", Scope: "project"}); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}

	// Verify.
	mem, err = cl.GetMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mem.ProjectMemory, "test fact") {
		t.Errorf("project memory = %q, want to contain 'test fact'", mem.ProjectMemory)
	}
}

func TestListCommands(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	cmds, err := cl.ListCommands(context.Background())
	if err != nil {
		t.Fatalf("ListCommands: %v", err)
	}
	// No custom commands expected in test environment.
	if cmds == nil {
		t.Error("expected non-nil (empty) list")
	}
}

func TestIsLoopbackOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://127.0.0.1:4127", true},
		{"http://localhost:4127", true},
		{"http://[::1]:4127", true},
		{"http://[::1]", true}, // IPv6 loopback without an explicit port
		{"http://evil.com", false},
		{"http://192.168.1.1:4127", false},
	}
	for _, tt := range tests {
		if got := isLoopbackOrigin(tt.origin); got != tt.want {
			t.Errorf("isLoopbackOrigin(%q) = %v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestEffectiveSystem_containsToolUseBlock(t *testing.T) {
	s := &Server{
		memory:    memory.Sources{},
		workspace: "",
		cfg:       &config.Config{},
	}
	out := s.effectiveSystem("base-system", "")

	if !strings.Contains(out, "A tool result is input") {
		t.Error("effectiveSystem output missing ToolUseBlock content")
	}
	tuIdx := strings.Index(out, "A tool result is input")
	ctIdx := strings.Index(out, "## Completing tasks")
	if tuIdx == -1 || ctIdx == -1 {
		t.Fatalf("missing expected block markers: tuIdx=%d ctIdx=%d", tuIdx, ctIdx)
	}
	if tuIdx > ctIdx {
		t.Error("ToolUseBlock must appear before CompletingTasksBlock in effectiveSystem output")
	}
}

func TestToAPIEventGuard(t *testing.T) {
	ev := toAPIEvent(engine.Event{Kind: engine.KindGuard, GuardReason: "missing citations"})
	if ev.Kind != api.KindGuard {
		t.Errorf("kind = %q, want guard", ev.Kind)
	}
	if ev.Text != "missing citations" {
		t.Errorf("text = %q, want the guard reason", ev.Text)
	}
	if ev.GuardRetrying {
		t.Error("GuardRetrying should default to false")
	}
	// P25.3: the retry flag must survive the engine→API translation so clients
	// know to withdraw the answer they just rendered.
	ev = toAPIEvent(engine.Event{Kind: engine.KindGuard, GuardReason: "missing citations", GuardRetrying: true})
	if !ev.GuardRetrying {
		t.Error("GuardRetrying = false, want true")
	}
}

// TestToAPIEventTurnDoneCarriesPromptEvalDuration is the P38.3 regression:
// PromptEvalDurationMS (the KV-cache-hit signal, P35.7/P35.13) must survive
// the engine→API translation alongside the other usage fields, or an SSE
// consumer has no way to distinguish a cache-hit turn from a full reprocess
// without debug-log tailing.
func TestToAPIEventTurnDoneCarriesPromptEvalDuration(t *testing.T) {
	ev := toAPIEvent(engine.Event{
		Kind: engine.KindTurnDone,
		Usage: &provider.Usage{
			InputTokens:          100,
			PromptEvalDurationMS: 250,
		},
		CostUSD: 0.01,
	})
	if ev.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", ev.InputTokens)
	}
	if ev.PromptEvalDurationMS != 250 {
		t.Errorf("PromptEvalDurationMS = %d, want 250", ev.PromptEvalDurationMS)
	}
}
