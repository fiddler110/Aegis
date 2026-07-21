package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"net/http"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/swarm"
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

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	srv.activateSessionSkill(meta.ID, "content-review")

	if _, ok := srv.sessionWorkdirs.Load(meta.ID); !ok {
		t.Fatal("sessionWorkdirs not populated before delete; test setup is wrong")
	}
	if _, ok := srv.sessionSkills.Load(meta.ID); !ok {
		t.Fatal("sessionSkills not populated before delete; test setup is wrong")
	}

	if err := cl.DeleteSession(ctx, meta.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, ok := srv.sessionWorkdirs.Load(meta.ID); ok {
		t.Error("sessionWorkdirs entry survived session delete (P26.2 leak)")
	}
	if _, ok := srv.sessionSkills.Load(meta.ID); ok {
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

	outAB := deferredToolsBlock(regAB)
	outBA := deferredToolsBlock(regBA)
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
