package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestDebateIntegrationBlockOptInDefault is the P12.5 regression: with no
// config toggles set, effectiveSystem must not mention debate mode at all —
// the mechanism multiplies model calls per item, so it must never appear
// unless explicitly opted into.
func TestDebateIntegrationBlockOptInDefault(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())

	got := srv.effectiveSystem("base prompt", "")
	if strings.Contains(got, "Debate mode") {
		t.Errorf("effectiveSystem should not mention debate mode with both toggles off: %q", got)
	}
}

// TestDebateIntegrationBlockPerToggle proves each toggle independently
// injects only its own instruction text, not the other's.
func TestDebateIntegrationBlockPerToggle(t *testing.T) {
	cases := []struct {
		name        string
		cfg         config.DebateIntegrationConfig
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "threat_model only",
			cfg:         config.DebateIntegrationConfig{ThreatModel: true},
			wantContain: []string{"Debate mode (P12)", "Threat modeling:"},
			wantAbsent:  []string{"Security-audit triage:"},
		},
		{
			name:        "triage only",
			cfg:         config.DebateIntegrationConfig{Triage: true},
			wantContain: []string{"Debate mode (P12)", "Security-audit triage:"},
			wantAbsent:  []string{"Threat modeling:"},
		},
		{
			name:        "both",
			cfg:         config.DebateIntegrationConfig{ThreatModel: true, Triage: true},
			wantContain: []string{"Threat modeling:", "Security-audit triage:"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := debateIntegrationBlock(tc.cfg)
			for _, s := range tc.wantContain {
				if !strings.Contains(got, s) {
					t.Errorf("block missing %q:\n%s", s, got)
				}
			}
			for _, s := range tc.wantAbsent {
				if strings.Contains(got, s) {
					t.Errorf("block should not contain %q:\n%s", s, got)
				}
			}
		})
	}
}

// roleScriptedAdapter returns a scripted response keyed by the request's
// System prompt, so a test can simulate the critic/proposer/arbiter turns of
// a real /debate call through the actual engine (not by calling
// internal/debate directly).
type roleScriptedAdapter struct {
	responses map[string]string
}

func (roleScriptedAdapter) Name() string { return "role-scripted" }

func (a roleScriptedAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	text, ok := a.responses[req.System]
	if !ok {
		text = "(no scripted response)"
	}
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 2}}
	close(ch)
	return ch, nil
}

func newDebateTestServer(t *testing.T, adapter provider.Adapter) *client.Client {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token")
}

// TestHandleDebateProducesFormattedVerdict is the core /debate regression: a
// claim run through the real HTTP endpoint (not internal/debate directly)
// comes back with a formatted transcript and a parsed verdict.
func TestHandleDebateProducesFormattedVerdict(t *testing.T) {
	adapter := roleScriptedAdapter{responses: map[string]string{
		debate.PersonaSystem(debate.DefaultCriticPersona):   "Evidence: file.go:10 contradicts the claimed severity.",
		debate.PersonaSystem(debate.DefaultProposerPersona): "Agreed, revising severity down.",
		debate.PersonaSystem(debate.DefaultArbiterPersona):  "VERDICT: REVISE\nCONFIDENCE: medium\nREASON: round 1 evidence accepted.",
	}}
	cl := newDebateTestServer(t, adapter)

	resp, err := cl.Debate(context.Background(), api.DebateRequest{Claim: "Some finding.", MaxRounds: 1})
	if err != nil {
		t.Fatalf("Debate: %v", err)
	}
	if resp.Verdict != "REVISE" {
		t.Errorf("Verdict = %q, want REVISE", resp.Verdict)
	}
	if resp.Confidence != "medium" {
		t.Errorf("Confidence = %q, want medium", resp.Confidence)
	}
	if !strings.Contains(resp.Report, "[evidence cited]") {
		t.Errorf("Report missing evidence tag:\n%s", resp.Report)
	}
}

// TestHandleDebateRequiresClaim checks the required-field validation on the
// raw endpoint.
func TestHandleDebateRequiresClaim(t *testing.T) {
	cl := newDebateTestServer(t, roleScriptedAdapter{})
	_, err := cl.Debate(context.Background(), api.DebateRequest{})
	if err == nil {
		t.Fatal("expected an error for a missing claim")
	}
}

// TestHandleDebateBlockedByDailyCostCap is the debate counterpart to
// TestDailyCostCapBlocksTurn: /debate is session-less (no session cap
// applies) but still spends real model turns, so the cross-session daily cap
// must refuse it exactly like a normal turn once today's spend already
// reached daily_cap_usd — before this fix, /debate ignored the cap entirely.
func TestHandleDebateBlockedByDailyCostCap(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
		Cost:       config.CostConfig{DailyCapUSD: 1.0},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, roleScriptedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	if err := store.AddDailyCost(ctx, 2.0); err != nil {
		t.Fatalf("AddDailyCost: %v", err)
	}

	_, err = cl.Debate(ctx, api.DebateRequest{Claim: "Some finding.", MaxRounds: 1})
	if err == nil {
		t.Fatal("expected Debate to fail once the daily cost cap is reached")
	}
	if !strings.Contains(err.Error(), "daily spend cap") {
		t.Errorf("error = %v, want mention of daily spend cap", err)
	}
}

// TestHandleDebateRecordsDailySpend proves a successful /debate run writes
// its accumulated cost into the same daily ledger a normal session turn
// uses, so it counts against a later daily cap check (from either another
// /debate call or a regular session turn) instead of spending invisibly.
func TestHandleDebateRecordsDailySpend(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "claude-sonnet", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
		Cost:       config.CostConfig{DailyCapUSD: 100.0},
	}
	adapter := roleScriptedAdapter{responses: map[string]string{
		debate.PersonaSystem(debate.DefaultCriticPersona):   "CONCEDE",
		debate.PersonaSystem(debate.DefaultProposerPersona): "Agreed.",
		debate.PersonaSystem(debate.DefaultArbiterPersona):  "VERDICT: UPHOLD\nCONFIDENCE: high\nREASON: no challenge survived.",
	}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	cl := client.New(ts.URL).WithToken("test-token")
	ctx := context.Background()

	before, err := store.TodayCost(ctx)
	if err != nil {
		t.Fatalf("TodayCost before: %v", err)
	}
	if before != 0 {
		t.Fatalf("TodayCost before = %v, want 0", before)
	}

	if _, err := cl.Debate(ctx, api.DebateRequest{Claim: "Some finding.", MaxRounds: 1}); err != nil {
		t.Fatalf("Debate: %v", err)
	}

	after, err := store.TodayCost(ctx)
	if err != nil {
		t.Fatalf("TodayCost after: %v", err)
	}
	if after <= before {
		t.Errorf("TodayCost after debate = %v, want > %v (debate spend should be recorded)", after, before)
	}
}

// TestHandleDebateRejectsBadJSON mirrors TestHandleScanRejectsBadJSON.
func TestHandleDebateRejectsBadJSON(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test", MaxTokens: 100}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, roleScriptedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/debate", strings.NewReader("{not valid json"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
