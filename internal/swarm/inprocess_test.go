package swarm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestInProcessSpawnReturnsResult(t *testing.T) {
	root := MailboxRoot(t.TempDir())
	reg := NewRegistry()
	run := func(ctx context.Context, cfg SpawnConfig) (string, error) {
		return "handled: " + cfg.Prompt, nil
	}
	b := NewInProcessBackend(run, reg, root)

	h, err := b.Spawn(context.Background(), SpawnConfig{Name: "w", Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "handled: do it" || res.Failed() {
		t.Errorf("result = %+v", res)
	}

	// Registry reflects completion.
	m, ok := reg.Get(res.AgentID)
	if !ok || m.Status != StatusDone {
		t.Errorf("registry member = %+v ok=%v", m, ok)
	}

	// Result is durably recorded in the mailbox.
	mb, _ := OpenMailbox(root, h.Identity)
	msgs, _ := mb.ReadAll(false)
	if len(msgs) != 1 || msgs[0].Type != MsgResult || msgs[0].Text != "handled: do it" {
		t.Errorf("mailbox = %+v", msgs)
	}
}

func TestInProcessSpawnPropagatesDepth(t *testing.T) {
	reg := NewRegistry()
	run := func(ctx context.Context, cfg SpawnConfig) (string, error) {
		return fmt.Sprintf("depth=%d", DepthFromContext(ctx)), nil
	}
	b := NewInProcessBackend(run, reg, MailboxRoot(t.TempDir()))

	h, _ := b.Spawn(context.Background(), SpawnConfig{Name: "w", Depth: 2})
	res, _ := h.Wait(context.Background())
	if res.Output != "depth=2" {
		t.Errorf("expected depth 2 in child ctx, got %q", res.Output)
	}
}

func TestInProcessSpawnFailure(t *testing.T) {
	reg := NewRegistry()
	run := func(ctx context.Context, cfg SpawnConfig) (string, error) {
		return "", errors.New("boom")
	}
	b := NewInProcessBackend(run, reg, MailboxRoot(t.TempDir()))

	h, _ := b.Spawn(context.Background(), SpawnConfig{Name: "w"})
	res, _ := h.Wait(context.Background())
	if !res.Failed() || res.Err != "boom" {
		t.Errorf("expected failure, got %+v", res)
	}
	m, _ := reg.Get(res.AgentID)
	if m.Status != StatusFailed {
		t.Errorf("status = %q, want failed", m.Status)
	}
}

func TestInProcessShutdownWaits(t *testing.T) {
	reg := NewRegistry()
	released := make(chan struct{})
	run := func(ctx context.Context, cfg SpawnConfig) (string, error) {
		<-released
		return "ok", nil
	}
	b := NewInProcessBackend(run, reg, MailboxRoot(t.TempDir()))
	if _, err := b.Spawn(context.Background(), SpawnConfig{Name: "w"}); err != nil {
		t.Fatal(err)
	}

	// Shutdown returns promptly when its ctx is cancelled even if work is stuck.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { b.Shutdown(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return on ctx cancel")
	}

	// Let the teammate finish and wait for it, so its mailbox write completes
	// before the temp dir is cleaned up.
	close(released)
	b.Shutdown(context.Background())
}
