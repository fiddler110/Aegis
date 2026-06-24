package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// LocalBackend runs commands directly on the host OS. This is the default
// backend and preserves the harness's existing behavior.
type LocalBackend struct{}

func NewLocalBackend() *LocalBackend { return &LocalBackend{} }

func (l *LocalBackend) Name() string { return "local" }

func (l *LocalBackend) Exec(ctx context.Context, command string, opts ExecOpts) (string, error) {
	runCtx, cancel := execWithTimeout(ctx, opts)
	defer cancel()

	name, args := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = opts.Dir

	out, err := cmd.CombinedOutput()
	text := string(out)
	if runCtx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("command timed out after %s", opts.Timeout)
	}
	if err != nil {
		return text, fmt.Errorf("exit error: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return "(no output)", nil
	}
	return text, nil
}

func (l *LocalBackend) ExecStreaming(ctx context.Context, command string, opts ExecOpts, emit func(string)) error {
	runCtx, cancel := execWithTimeout(ctx, opts)
	defer cancel()

	name, args := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = opts.Dir
	w := emitWriter{emit: emit}
	cmd.Stdout = w
	cmd.Stderr = w

	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command timed out after %s", opts.Timeout)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("exit error: %w", err)
	}
	return nil
}

func (l *LocalBackend) Close() error { return nil }

type emitWriter struct{ emit func(string) }

func (w emitWriter) Write(p []byte) (int, error) {
	w.emit(string(p))
	return len(p), nil
}
