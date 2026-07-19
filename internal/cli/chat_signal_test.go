package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"
)

// watchSignal must log which signal fired and cancel the run context. This is
// the P35.8 exit-trace seam: a signal-driven exit has to be distinguishable
// from a silent mid-run death in aegis.log.
func TestWatchSignalLogsAndCancels(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGTERM

	watchSignal(ctx, cancel, sigCh, logger)

	if ctx.Err() == nil {
		t.Fatal("watchSignal did not cancel the context after a signal")
	}
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("received signal")) {
		t.Errorf("log missing signal-cause line:\n%s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte(syscall.SIGTERM.String())) {
		t.Errorf("log missing signal name %q:\n%s", syscall.SIGTERM.String(), out)
	}
}

// watchSignal must return (not block, not log) when the context is cancelled
// with no signal — the normal path where the run completes and cleanup cancels.
// This is what lets the watcher goroutine exit instead of leaking.
func TestWatchSignalReturnsOnContextDone(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)

	cancel() // simulate run completion / cleanup

	done := make(chan struct{})
	go func() {
		watchSignal(ctx, cancel, sigCh, logger)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchSignal did not return after context was cancelled (goroutine leak)")
	}
	if buf.Len() != 0 {
		t.Errorf("watchSignal logged on the no-signal path:\n%s", buf.String())
	}
}

// installSignalCancel's cleanup must cancel the derived context (and unblock
// the watcher goroutine) without needing a real signal.
func TestInstallSignalCancelCleanupCancels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	ctx, stop := installSignalCancel(context.Background(), logger)
	if ctx.Err() != nil {
		t.Fatal("context cancelled before cleanup")
	}
	stop()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not cancel the derived context")
	}
}
