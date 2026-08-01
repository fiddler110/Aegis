package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
)

// handleDebate runs a multi-agent debate (P12) directly against the daemon's
// configured model, independent of any session — the same underlying
// mechanism the `agent` tool's debate mode runs, exposed so `/debate` in the
// TUI (and `aegis debate`) can adversarially review a claim without first
// spending a conversational turn to produce it. Unlike /security/scan, this
// does spend model turns (one per role per round) since debate is inherently
// model-driven; there is no scanner-only equivalent.
func (s *Server) handleDebate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.DebateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Claim == "" {
		writeError(w, http.StatusBadRequest, "claim is required")
		return
	}
	if s.adapter == nil {
		writeError(w, http.StatusServiceUnavailable, "no model provider configured")
		return
	}

	// Debate is session-less, so it needs the same Workdir validation
	// handleCreateSession applies (P25.1/P25.8): must exist and, on a
	// remote-accessible daemon, fall within the configured trust boundary.
	workdir, werr := s.resolveSessionWorkdir(req.Workdir)
	if werr != nil {
		writeError(w, werr.status, werr.msg)
		return
	}

	// Debate is session-less, so no session cap applies — but it still spends
	// real model turns (one per role per round), so the cross-session daily
	// caps (P9.5/P10.5) must gate it exactly like a normal turn does. Before
	// this guard existed, /debate ran even with the daily cap exhausted, and
	// its spend was never recorded — invisible to every other cap check too.
	spend, dailyCapErr := s.beginDailySpend(r.Context())
	if dailyCapErr != nil {
		writeError(w, http.StatusPaymentRequired, dailyCapErr.Error())
		return
	}

	tracker := cost.NewTracker()
	cfg := debate.Config{
		Domain:          req.Domain,
		ProposerPersona: req.ProposerPersona,
		CriticPersona:   req.CriticPersona,
		ArbiterPersona:  req.ArbiterPersona,
		MaxRounds:       req.MaxRounds,
		Tracker:         tracker,
		BudgetUSD:       s.cfg.Cost.BudgetUSD,
		MaxTokens:       s.cfg.Cost.MaxTokensPerRun,
	}
	claim := debate.WithFiles(req.Claim, req.Files)
	transcript, err := debate.Run(r.Context(), claim, cfg, s.debateRoleRunner(tracker, workdir))
	// Record spend regardless of outcome: debate.Run returns the partial
	// transcript (and whatever the tracker accumulated) even on error, so an
	// aborted debate's spend must still count against the daily caps.
	spend.Finish(tracker.TotalUSD(), tracker.TotalTokens())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.DebateResponse{
		Report:     transcript.Format(),
		Verdict:    transcript.Verdict.Outcome,
		Confidence: transcript.Verdict.Confidence,
	})
}

// debateRoleRunner returns a debate.RunFunc that executes one role turn as a
// bare, session-less engine call over the daemon's shared adapter and tools —
// the same construction subAgentRunner uses for a spawned teammate, without
// the swarm identity/mailbox machinery a direct (non-session) debate call has
// no use for. Every role call shares tracker, so debate.Run's budget check
// (P12.6) sees the run's true cumulative spend across rounds. workdir (P25.8)
// grounds every role's tool calls in the request's directory rather than
// always falling back to the daemon's default workspace; "" keeps that
// default.
func (s *Server) debateRoleRunner(tracker *cost.Tracker, workdir string) debate.RunFunc {
	return func(ctx context.Context, systemPrompt, prompt string) (string, error) {
		gate, engineHooks := s.buildGate("build", s.approver(), persona.Persona{})
		eng, err := engine.New(engine.Options{
			Adapter:         s.adapter,
			Tools:           s.tools,
			Gate:            gate,
			Compactor:       s.compactor,
			Hooks:           engineHooks,
			Cost:            tracker,
			BudgetUSD:       s.cfg.Cost.BudgetUSD,
			MaxTokensPerRun: s.cfg.Cost.MaxTokensPerRun,
			Model:           s.cfg.Provider.Model,
			MaxTokens:       s.cfg.Provider.MaxTokens,
			Logger:          s.logger,
			Workdir:         workdir,
			ExtraRoots:      s.workspaceRootsFor(workdir),
		})
		if err != nil {
			return "", err
		}
		conv := &engine.Conversation{System: systemPrompt}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: prompt}}})

		const maxOutput = 1 << 20 // 1 MiB
		var sb strings.Builder
		runErr := eng.Run(ctx, conv, func(ev engine.Event) {
			if ev.Kind == engine.KindText && sb.Len() < maxOutput {
				sb.WriteString(ev.Text)
			}
		})
		return strings.TrimSpace(sb.String()), runErr
	}
}
