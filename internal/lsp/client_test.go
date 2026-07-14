package lsp

import (
	"context"
	"io"
	"testing"
	"time"
)

// nopWriteCloser adapts an io.Writer that never blocks (io.Discard) into the
// io.WriteCloser the Client.stdin field expects, so call()'s request write
// doesn't need a real reader draining the other end.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// TestCallFailsPromptlyWhenTransportDies simulates the P30.1 bug scenario:
// an LSP server process dies (or its stdout pipe otherwise breaks) while a
// call() is in flight. Before the failPending fix, readLoop returned
// silently on EOF/read-error/oversized-line without ever touching c.pending,
// so the blocked call() would hang forever absent the caller's own context
// deadline. This test does not rely on a context deadline as the safety
// net for the real fix — it only uses one to bound how long the test itself
// waits before failing.
func TestCallFailsPromptlyWhenTransportDies(t *testing.T) {
	pr, pw := io.Pipe()
	c := &Client{
		name:    "test-server",
		stdin:   nopWriteCloser{io.Discard},
		stdout:  io.NopCloser(pr),
		pending: make(map[int]chan callResult),
	}
	go c.readLoop()

	callDone := make(chan error, 1)
	go func() {
		// Long deadline: we want the *transport death*, not this context,
		// to be what unblocks call().
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := c.call(ctx, "textDocument/hover", nil)
		callDone <- err
	}()

	// Give call() a moment to register itself in c.pending before killing
	// the transport, so the request is genuinely in flight.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		n := len(c.pending)
		c.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("call() never registered a pending request")
		}
		time.Sleep(time.Millisecond)
	}

	// Simulate the server process dying / stdio pipe breaking: closing the
	// write end delivers EOF to readLoop's blocked read on stdout.
	if err := pw.Close(); err != nil {
		t.Fatalf("closing pipe: %v", err)
	}

	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("expected call() to return an error after transport death, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("call() did not return after transport died — readLoop failed to drain c.pending (P30.1 regression)")
	}

	// A closed client should fail new calls immediately too, instead of
	// enqueuing into a pending map nothing will ever drain again.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.call(ctx, "textDocument/hover", nil); err == nil {
		t.Fatal("expected call() on a closed client to fail immediately, got nil error")
	}
}
