// The swarm seam: how the daemon builds a sub-agent backend and what a
// sub-agent run does. Extracted from server.go (L4).
package server

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/swarm"
)

// onSubagentStop records the SUBAGENT_STOP lifecycle event in the audit trail.
func (s *Server) onSubagentStop(id swarm.Identity, res swarm.Result) {
	status := "done"
	summary := res.Output
	if res.Failed() {
		status, summary = "failed", res.Err
	}
	if s.audit != nil {
		s.audit.SubagentStop(id.AgentID, status, truncateSummary(summary, 200), res.Failed())
	}
	if s.execHook != nil {
		s.execHook.SubagentStop(context.Background(), id.AgentID, status, truncateSummary(summary, 200), res.Failed())
	}
	s.logger.Info("subagent stopped", "agent", id.AgentID, "status", status)
}

func truncateSummary(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "…"
	}
	return s
}

// buildSwarmBackend selects the sub-agent backend from config. The subprocess
// backend gives OS-level isolation by launching the harness binary in a headless
// worker mode; the default in-process backend runs teammates as goroutines.
func (s *Server) buildSwarmBackend(mailboxRoot string) swarm.Backend {
	if s.cfg.Swarm.Backend == "subprocess" {
		if exe, err := os.Executable(); err == nil {
			s.logger.Info("swarm backend: subprocess", "exe", exe)
			return swarm.NewSubprocessBackend(exe, "__worker", s.swarmReg, mailboxRoot, s.cfg.SessionDBPath(), s.cfg.Cost.BudgetUSD, s.cfg.Cost.MaxTokensPerRun)
		}
		s.logger.Warn("cannot resolve executable path; falling back to in-process swarm backend")
	}
	return swarm.NewInProcessBackend(s.subAgentRunner(), s.swarmReg, mailboxRoot, s.cfg.Cost.BudgetUSD, s.cfg.Cost.MaxTokensPerRun)
}

// subAgentRunner returns a swarm.RunFunc that executes a teammate by building a
// sub-engine over the daemon's shared adapter and tools. The child runs with its
// own (clamped) permission mode. Its cost tracker is normally the same shared
// ledger the top-level session run attached to ctx (D1): every sub-agent in a
// fan-out tree, at any depth, draws against one BudgetUSD ceiling instead of
// each spawn getting a fresh allowance — a background/detached spawn whose
// context was severed from the request falls back to a fresh tracker since
// there's no ledger left to share.
//
// FIND-14: when InProcessBackend.Spawn has attached a per-agent budget override
// (this teammate's own guaranteed floor share of the shared pool), the teammate
// runs against a fresh local tracker capped at that share instead of checking
// the shared tracker against the daemon's full cap — otherwise every teammate
// checks the same live aggregate, so one expensive sibling can push it past the
// cap and starve the rest. The local tracker's actual spend folds back into the
// shared ledger when the teammate finishes (mirroring SubprocessBackend's
// AddWorkerCost), so a sibling spawned afterward still sees the updated total.
func (s *Server) subAgentRunner() swarm.RunFunc {
	return func(ctx context.Context, cfg swarm.SpawnConfig) (string, error) {
		if s.adapter == nil {
			return "", fmt.Errorf("no model provider configured")
		}
		model := cfg.Model
		if model == "" {
			model = s.cfg.Provider.Model
		}
		sharedTracker, _ := swarm.CostTrackerFromContext(ctx).(*cost.Tracker)

		tracker := sharedTracker
		// P66.13/ARCH-06: one shared reading of the run bounds, so a knob added
		// to `cost:` (or to the three non-`cost:` bounds that ride with it —
		// max_iterations, loop_threshold, redact_secrets — or cold_cache_after)
		// reaches a spawned teammate too. Hand-copying five of the nine here is
		// what left security.redact_secrets inert for every sub-agent: a
		// teammate's read-tool output went to the provider unscrubbed on a
		// daemon whose operator had explicitly turned redaction on.
		limits := enginecfg.CostLimits(s.cfg)
		var foldBack *cost.Tracker
		if usd, toks, ok := swarm.BudgetOverrideFromContext(ctx); ok {
			foldBack = cost.NewTracker()
			tracker = foldBack
			// Taken whole, zeros included — see WithBudgetOverride. Only these
			// two dimensions are shared out (swarm.WithBudgetOverride carries
			// exactly two); every other bound below is inherited.
			limits = limits.WithBudgetOverride(usd, toks)
		} else if tracker == nil {
			tracker = cost.NewTracker()
		}
		// Sub-agents get the same gate stack a top-level run does (contextual
		// egress/network policy, text allow/deny rules) — only the mode gate
		// was ever meaningfully child-specific, and clampMode already confines
		// that above the swarm layer. A bare mode gate here let a spawned
		// teammate route straight around an operator's egress-then-write or
		// deny rule (P10.1).
		gate, engineHooks := s.buildGate(cfg.Mode, s.approver(), persona.Persona{})
		// A spawn can name its own model (swarm.SpawnConfig.Model), which hits
		// the same shared-adapter mismatch a routed turn does — serve it with
		// *its* window, not the primary model's num_ctx (P52.4). Identical to
		// today whenever the spawn runs on the global model.
		spawnWin, _ := s.effectiveContextWindowFor(ctx, model)
		// P66.4/ARCH-02: a clone of the parent session's registry, not
		// s.tools. Handing a sub-agent the daemon-wide registry undid exactly
		// what the P9 clone exists to prevent — its tool_search reached
		// Registry.Load on the global registry and permanently exposed that
		// tool's schema to every session created afterwards. Its own clone
		// also means the teammate inherits the parent's preloaded persona
		// tools and activated skill tools, instead of working from a different
		// tool set for reasons nobody chose.
		// P65.4: no InitialStartedTools/OnToolStarted/OnToolFinished here — a
		// spawned sub-agent has no durable session of its own to key a register
		// on (internal/swarm confirmed: a crashed teammate's goroutine leaves no
		// record today regardless), and is explicitly out of scope for this pass.
		opts := engine.Options{
			Adapter:   s.modelAdapter(spawnWin),
			Tools:     s.subAgentToolRegistry(cfg.ParentSessionID),
			Gate:      gate,
			Purpose:   provider.PurposeSubAgent, // P67.3
			Compactor: s.compactor,
			// A spawn had a Compactor but no window to measure against
			// (ContextWindowTokens was left 0), so the engine's *per-turn*
			// 85%-fill check never ran for a sub-agent however long it grew —
			// only Run's entry-point Compact did, gated by the summarizer's own
			// budget, which is tuned to the compaction model rather than to the
			// model this spawn is running on. Now that the window is resolved
			// per model just above, feed it in and the spawn gets the same
			// proactive protection (and the same nothing-left-to-compact notice)
			// a top-level run has.
			ContextWindowTokens: spawnWin,
			RoundResultCap:      roundCapFor(cfg.Workdir), // P67.1
			Hooks:               engineHooks,
			Cost:                tracker,
			Model:               model,
			Logger:              s.logger,
			// Set explicitly from cfg.Workdir (P25.8) rather than relying on
			// the parent session's tool.WithWorkdir ctx value leaking through
			// the spawn's context chain — that accidental inheritance only
			// ever reached foreground in-process spawns; a detached/
			// background spawn's job runs under a context derived from
			// context.Background() (task.Manager.Start) and loses it.
			Workdir: cfg.Workdir,
			// A spawn is confined the same way its parent session is: the
			// additional roots are a property of the workspace, not of who
			// is asking (P52.13).
			ExtraRoots: s.workspaceRootsFor(cfg.Workdir),
		}
		// P59.4/P52.15: every bound not shared out above is inherited whole
		// rather than divided. The cost/token floors are divisible because
		// spend is additive across siblings; elapsed time is not — teammates
		// run concurrently, so "N minutes" means the same N minutes for each.
		limits.Apply(&opts)
		// A teammate talks to the same model server as the session that spawned
		// it, so it needs the same sampling parameters, the same tool-calling
		// fallback (P53.6 — a shimmed session whose spawns weren't shimmed
		// hands every teammate a model that can't call a tool), and the same
		// backend identification for calibration (P66.14/LLM-03).
		enginecfg.ModelBackend(s.cfg).Apply(&opts)
		eng, err := engine.New(opts)
		if err != nil {
			return "", err
		}

		// Grandchildren clamp against this child's mode.
		ctx = swarm.WithParentMode(ctx, cfg.Mode)
		conv := &engine.Conversation{System: cfg.SystemPrompt}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: cfg.Prompt}}})

		const maxOutput = 1 << 20 // 1 MiB
		var sb strings.Builder
		runErr := eng.Run(ctx, conv, func(ev engine.Event) {
			if ev.Kind == engine.KindText && sb.Len() < maxOutput {
				sb.WriteString(ev.Text)
			}
		})
		// Fold this teammate's actual spend back into the shared ledger, so a
		// sibling spawned afterwards computes its own share against a total
		// that includes this one (FIND-14).
		if foldBack != nil && sharedTracker != nil {
			sharedTracker.AddWorkerCost(foldBack.TotalUSD(), foldBack.TotalTokens())
		}
		return strings.TrimSpace(sb.String()), runErr
	}
}
