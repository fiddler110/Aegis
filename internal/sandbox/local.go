package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LocalBackend runs commands directly on the host OS. This is the default
// backend and preserves the harness's existing behavior.
type LocalBackend struct {
	// stripEnv lists env var names excluded from the spawned command's
	// environment (P7.2). Always includes DefaultStripEnv.
	stripEnv []string

	// envAllow lists the only env var names a spawned command's environment
	// may carry (P81.26/FIND-26) — start from nothing, not the daemon's own
	// environment. Always includes DefaultEnvAllow; extended via WithEnvAllow
	// (sandbox.env_allow).
	envAllow []string

	// limits caps the resources (currently memory + process count; see
	// joblimits_windows.go) a spawned command may consume on platforms that
	// support it (P81.22/FIND-22) — set via WithLimits (sandbox.limits),
	// mirroring the container backend's per-runtime enforcement.
	limits ResourceLimits

	// maxOutput bounds the bytes Exec buffers from a command (VULN-05). Zero
	// means maxCapturedOutput; it is a field only so a test can bind the cap
	// with a command that finishes in milliseconds instead of one that has to
	// produce 4 MiB first.
	maxOutput int
}

// NewLocalBackend returns a local backend that strips DefaultStripEnv (the
// provider API keys) from commands it runs, and otherwise only forwards
// DefaultEnvAllow.
func NewLocalBackend() *LocalBackend {
	return &LocalBackend{stripEnv: DefaultStripEnv, envAllow: DefaultEnvAllow}
}

// NewLocalBackendWithEnv returns a local backend that also strips the given
// env var names in addition to DefaultStripEnv (P7.2) — e.g. secrets loaded
// from .aegis/.env for MCP server authentication that the shell tool has no
// business reading.
func NewLocalBackendWithEnv(strip []string) *LocalBackend {
	return &LocalBackend{stripEnv: mergeStripEnv(strip), envAllow: DefaultEnvAllow}
}

// WithEnvAllow adds operator-configured additional allowlisted env var names
// (sandbox.env_allow) on top of DefaultEnvAllow. Returns the receiver so it
// chains onto a constructor call; mutates in place, so it is meant to be
// called once, right after construction, before the backend is shared.
func (l *LocalBackend) WithEnvAllow(extra []string) *LocalBackend {
	l.envAllow = mergeEnvAllow(DefaultEnvAllow, extra)
	return l
}

// WithLimits sets the resource caps (P81.22/FIND-22) applied to every command
// this backend runs, on platforms that support enforcing them (currently
// Windows job objects; see joblimits_windows.go/joblimits_other.go). Returns
// the receiver so it chains onto a constructor call.
func (l *LocalBackend) WithLimits(lim ResourceLimits) *LocalBackend {
	l.limits = lim
	return l
}

func (l *LocalBackend) Name() string { return "local" }

// ioCloseGrace is the extra time we wait for I/O pipes to drain after the
// command's context expires. Without this, CombinedOutput/Run can hang
// indefinitely on Windows when PowerShell spawns child processes that keep the
// inherited pipes open after the parent shell is killed.
const ioCloseGrace = 5 * time.Second

func (l *LocalBackend) Exec(ctx context.Context, command string, opts ExecOpts) (string, error) {
	runCtx, cancel := execWithTimeout(ctx, opts)
	defer cancel()

	name, args := ShellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = opts.Dir
	cmd.Env = allowlistedEnv(os.Environ(), l.envAllow, l.stripEnv)
	cmd.WaitDelay = ioCloseGrace

	// Bound the capture at the pipe rather than after it (VULN-05). The result
	// caps in internal/tool/builtin/truncate.go are applied to the string this
	// returns, i.e. only once the whole output is already resident in the
	// daemon's heap — so `cat /dev/urandom` with the 600s ceiling could buffer
	// tens of GB and OOM the daemon, taking every concurrent session with it.
	limit := l.maxOutput
	if limit <= 0 {
		limit = maxCapturedOutput
	}
	w := &capWriter{limit: limit}
	cmd.Stdout = w
	cmd.Stderr = w

	err := l.run(cmd)
	text := w.text()
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

	name, args := ShellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = opts.Dir
	cmd.Env = allowlistedEnv(os.Environ(), l.envAllow, l.stripEnv)
	cmd.WaitDelay = ioCloseGrace
	w := emitWriter{emit: emit}
	cmd.Stdout = w
	cmd.Stderr = w

	err := l.run(cmd)
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

// run starts cmd and, when l.limits names a real cap, assigns the spawned
// process to a resource-limited job object before waiting on it (P81.22/
// FIND-22 — see joblimits_windows.go). Split from a bare cmd.Run() because
// job-object assignment has to happen between Start and Wait: the process
// needs a handle before it can be placed under the job, and placing it after
// Wait would be placing it after the fact.
//
// A limiter that fails to construct or assign is logged nowhere here (this
// type carries no logger) and simply runs the command unlimited — the
// warning that a configured cap is not in force on this platform/runtime
// belongs at sandbox selection time (see internal/server.SelectSandbox),
// once per daemon start, not once per command.
func (l *LocalBackend) run(cmd *exec.Cmd) error {
	limiter, _ := newResourceLimiter(l.limits)
	if limiter != nil {
		defer limiter.Close()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if limiter != nil {
		_ = limiter.assign(cmd.Process)
	}
	return cmd.Wait()
}

func (l *LocalBackend) Close() error { return nil }

// maxCapturedOutput bounds how much of a command's combined output Exec will
// hold in memory.
//
// Deliberately far above every result cap in internal/tool/builtin's posture
// table (24 KiB for shell, 32 KiB for git), so no realistic result is touched
// here and the truncation semantics a caller sees are still that table's:
// which end survives and what the notice says is decided downstream, on a
// string this cap has not altered. Only pathological output — the runaway
// producer this bound exists for — ever reaches it.
const maxCapturedOutput = 4 << 20 // 4 MiB

// capWriter buffers at most limit bytes and counts (but discards) the rest.
//
// It keeps the *head*, which is the one place this differs from the shell
// tool's tail-keeping posture, and the choice is forced: keeping the tail of an
// unbounded stream means either buffering it all (the defect) or a ring buffer
// that throws away the beginning of every over-cap output. The head is what a
// runaway command's diagnosis needs anyway — the command that started printing
// is at the top — and the appended notice is what the tail-keeping consumer
// downstream will actually preserve, so the truncation stays visible either way.
type capWriter struct {
	buf       []byte
	limit     int
	discarded int64
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.limit - len(w.buf); room > 0 {
		if len(p) > room {
			w.buf = append(w.buf, p[:room]...)
			w.discarded += int64(len(p) - room)
		} else {
			w.buf = append(w.buf, p...)
		}
	} else {
		w.discarded += int64(len(p))
	}
	// Always claim the full write succeeded: returning a short count makes
	// exec close the pipe, which SIGPIPEs the child instead of letting it run
	// to completion (or to its timeout) with its output merely dropped.
	return len(p), nil
}

// text returns the captured output, with a notice appended when the cap bound.
// The notice goes last so it survives TruncateTail downstream.
func (w *capWriter) text() string {
	if w.discarded == 0 {
		return string(w.buf)
	}
	return string(w.buf) + fmt.Sprintf(
		"\n[output capped at %d bytes while the command was running; %d further bytes were discarded unread]",
		w.limit, w.discarded)
}

type emitWriter struct{ emit func(string) }

func (w emitWriter) Write(p []byte) (int, error) {
	w.emit(string(p))
	return len(p), nil
}
