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
	wg          sync.WaitGroup
}

// NewInProcessBackend builds an in-process backend. run executes a teammate to
// completion; registry tracks lifecycle; mailboxRoot is MailboxRoot(dataDir).
func NewInProcessBackend(run RunFunc, registry *Registry, mailboxRoot string) *InProcessBackend {
	return &InProcessBackend{run: run, registry: registry, mailboxRoot: mailboxRoot}
}

// Spawn launches a teammate goroutine and returns a handle to await its result.
func (b *InProcessBackend) Spawn(ctx context.Context, cfg SpawnConfig) (*Handle, error) {
	id := NewIdentity(cfg.Name, cfg.Team, cfg.ParentSessionID)
	b.registry.Add(id)

	h := &Handle{Identity: id, done: make(chan Result, 1)}
	b.wg.Go(func() {
		// Carry the child's spawn depth so nested agent tools can guard recursion.
		childCtx := WithDepth(ctx, cfg.Depth)
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
