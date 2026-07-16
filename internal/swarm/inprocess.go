package swarm

import (
	"context"
	"strings"
	"sync"
)

// InProcessBackend runs each teammate as a goroutine in the current process,
// sharing the daemon's adapter and tools via the supplied RunFunc. Results are
// delivered through the returned Handle and also recorded durably in the
// teammate's mailbox.
type InProcessBackend struct {
	run         RunFunc
	registry    *Registry
	mailboxRoot string
	// budgetUSD/maxTokensPerRun are the daemon's configured cost.budget_usd /
	// cost.max_tokens_per_run caps (0 = unlimited); Spawn uses them to give
	// each teammate its own guaranteed floor share of the shared swarm budget
	// (FIND-14) — the same remainingBudget/remainingTokens computation
	// SubprocessBackend already applies to each WorkerSpec.
	budgetUSD       float64
	maxTokensPerRun int
	onStop          func(Identity, Result)
	wg              sync.WaitGroup
}

// OnStop registers a teammate-completion listener (SUBAGENT_STOP).
func (b *InProcessBackend) OnStop(fn func(Identity, Result)) { b.onStop = fn }

// NewInProcessBackend builds an in-process backend. run executes a teammate to
// completion; registry tracks lifecycle; mailboxRoot is MailboxRoot(dataDir).
// budgetUSD/maxTokensPerRun are the daemon's configured cost caps (0 =
// unlimited), used to compute each spawned teammate's own guaranteed floor
// share of the shared budget; pass 0 for either to skip that floor — a caller
// with no cap configured has nothing to guarantee a share of.
func NewInProcessBackend(run RunFunc, registry *Registry, mailboxRoot string, budgetUSD float64, maxTokensPerRun int) *InProcessBackend {
	return &InProcessBackend{
		run:             run,
		registry:        registry,
		mailboxRoot:     mailboxRoot,
		budgetUSD:       budgetUSD,
		maxTokensPerRun: maxTokensPerRun,
	}
}

// Spawn launches a teammate goroutine and returns a handle to await its result.
func (b *InProcessBackend) Spawn(ctx context.Context, cfg SpawnConfig) (*Handle, error) {
	id := NewIdentity(cfg.Name, cfg.Team, cfg.ParentSessionID)
	b.registry.Add(id)

	h := &Handle{Identity: id, done: make(chan Result, 1)}
	b.wg.Go(func() {
		// Carry the child's spawn depth so nested agent tools can guard recursion.
		childCtx := WithDepth(ctx, cfg.Depth)

		// FIND-14: every in-process teammate used to check the same live
		// shared tracker against the daemon's full configured cap, so one
		// expensive teammate could push that shared total past the cap and
		// leave every sibling's next per-turn check with nothing. Attach this
		// spawn's own guaranteed floor share instead; the RunFunc honors it by
		// running against a local tracker capped at that share and folding the
		// actual spend back afterwards, so a sibling spawned later still sees
		// the updated total, but no teammate's live spend can starve another's
		// floor out from under it.
		if tracker, ok := CostTrackerFromContext(ctx).(costTracker); ok && (b.budgetUSD > 0 || b.maxTokensPerRun > 0) {
			var usd float64
			var toks int
			if b.budgetUSD > 0 {
				usd = remainingBudget(b.budgetUSD, tracker.TotalUSD())
			}
			if b.maxTokensPerRun > 0 {
				toks = remainingTokens(b.maxTokensPerRun, tracker.TotalTokens())
			}
			childCtx = WithBudgetOverride(childCtx, usd, toks)
		}

		output, err := b.run(childCtx, cfg)

		res := Result{AgentID: id.AgentID, Output: output}
		status := StatusDone
		if err != nil {
			res.Err = err.Error()
			status = StatusFailed
		}
		b.registry.Update(id.AgentID, status, summarize(output, res.Err))

		// Durable record of the result (basis for polling + subprocess parity).
		if mb, e := OpenMailbox(b.mailboxRoot, id); e == nil {
			_ = mb.Send(Message{
				Type:      MsgResult,
				Sender:    id.AgentID,
				Recipient: cfg.ParentSessionID,
				Text:      output,
				Payload:   map[string]any{"error": res.Err},
			})
		}

		if b.onStop != nil {
			b.onStop(id, res)
		}
		h.done <- res
	})
	return h, nil
}

// Shutdown waits for all in-flight teammates to finish or ctx to cancel.
func (b *InProcessBackend) Shutdown(ctx context.Context) {
	done := make(chan struct{})
	go func() { b.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// summarize produces a short one-line registry summary.
func summarize(output, errStr string) string {
	if errStr != "" {
		return "failed: " + truncateLine(errStr, 80)
	}
	return truncateLine(strings.TrimSpace(output), 80)
}

func truncateLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
