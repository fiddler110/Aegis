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
	// Resolved up front (P69.6) so the seats — and therefore the models that
	// will be resident together — are known before any of them runs. withDefaults
	// is idempotent, so handing Run an already-resolved config changes nothing
	// about the debate; what it buys is that the set planned for is exactly the
	// set that executes.
	cfg := debate.WithDefaults(debate.Config{
		Domain:          req.Domain,
		ProposerPersona: req.ProposerPersona,
		CriticPersona:   req.CriticPersona,
		ArbiterPersona:  req.ArbiterPersona,
		MaxRounds:       req.MaxRounds,
		Tracker:         tracker,
		BudgetUSD:       s.cfg.Cost.BudgetUSD,
		MaxTokens:       s.cfg.Cost.MaxTokensPerRun,
	})

	// Plan the seats as one resident set and hold the plan for the debate's
	// duration. A no-op unless provider.vram_budget_gb is configured; when it is
	// and the set cannot fit, this refuses here — before the first model turn —
	// rather than letting Ollama discover it by spilling half the arbiter to
	// system RAM. debateRoleRunner needs no change: it resolves each seat's
	// window through effectiveContextWindowFor, which is now answering from the
	// installed plan.
	release, claimErr := s.claimResidentSet(r.Context(), s.debateSeatModels(cfg))
	if claimErr != nil {
		spend.Finish(0, 0)
		writeError(w, http.StatusServiceUnavailable, claimErr.Error())
		return
	}
	defer release()

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
	// One clone for the whole debate: the roles share exposure with each other
	// (a tool one role loaded is loaded for the rest of the debate, as it was
	// when they all ran against s.tools) but not with the daemon, so a role's
	// tool_search no longer permanently widens every other session's surface.
	// Same defect as ARCH-02 in subAgentRunner, same shape (P66.4).
	tools := s.tools.Clone()
	return func(ctx context.Context, seat debate.Seat, systemPrompt, prompt string) (string, error) {
		// Resolve this seat's model the same way a session resolves a
		// persona's (personaModel: config override -> persona file -> global),
		// and serve it with *its own* detected window rather than the primary
		// model's (P69.1, same reasoning as modelAdapter's P52.4). Before this,
		// every role ran on s.cfg.Provider.Model, so a debate could not put a
		// different model in the arbiter's seat even though the persona layer
		// already carried a Model override and the session path already honored
		// it. On a single-GPU local backend that is the whole feature: the
		// arbiter needs no tools and is the one seat a smaller, differently-
		// trained model can take, which is also where a different model
		// decorrelates error instead of merely costing VRAM.
		//
		// The window matters as much as the model. A 3.8B arbiter handed the
		// 9B's 32k num_ctx allocates a KV cache it will never fill out of the
		// same budget the debater is holding, which is the cold-reload churn
		// modelAdapter exists to avoid.
		p, _ := persona.Get(seat.Persona)
		model := s.personaModel(p)
		ctxWin, _ := s.effectiveContextWindowFor(ctx, model)

		// buildGate still gets an empty Persona deliberately: the seat's
		// persona supplies the system prompt and the model, not the tool gate.
		// Letting a persona file widen a debate role's permissions is a
		// separate decision with a security review attached, and folding it in
		// here would make it by accident.
		gate, engineHooks := s.buildGate("build", s.approver(), persona.Persona{})
		// P65.4: no InitialStartedTools/OnToolStarted/OnToolFinished here — a
		// debate role is a bounded sub-run of an already-durable parent turn,
		// not itself a resumable session with a session ID to key a register on.
		eng, err := engine.New(engine.Options{
			Adapter:         s.modelAdapter(ctxWin),
			Tools:           tools,
			Gate:            gate,
			Compactor:       s.compactor,
			Hooks:           engineHooks,
			Cost:            tracker,
			Purpose:         provider.PurposeDebate, // P67.3
			BudgetUSD:       s.cfg.Cost.BudgetUSD,
			MaxTokensPerRun: s.cfg.Cost.MaxTokensPerRun,
			Model:           model,
			MaxTokens:       s.cfg.Provider.MaxTokens,
			Logger:          s.logger,
			Workdir:         workdir,
			RoundResultCap:  roundCapFor(workdir), // P67.1
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
