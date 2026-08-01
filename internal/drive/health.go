package drive

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

// --- P50.1: backend liveness + resumable reset ---
//
// The 2026-07-30 FirewallRiskRater run's real stall was Ollama dying mid-phase,
// not a logic bug: the retry decorator only covers a *synchronous* Stream
// failure before tokens stream (provider/retry.go), so a mid-stream
// `model runner has unexpectedly stopped`, or a connection-refused outage that
// outlasts the ~4 capped backoffs, ends the engine Run with a terminal error
// the drive treated as fatal — the whole process exited with a half-built phase
// and no resume. The phased drive already has the recovery primitive: the
// P47.2/P47.7 fresh-context reset resumes any phase from its on-disk
// `<!-- PENDING -->` files. This adds the missing precondition — WAIT for the
// backend to come back (a liveness probe with backoff) — and then reuses that
// reset. A backend-down error is thus recovered exactly like a context
// overflow, but gated on the server being reachable again first.

// backendRecoverBudget bounds how long the drive waits for a dead backend to
// return before giving up with a resumable stop. Generous: a local model server
// (Ollama) that was killed can take tens of seconds to relaunch and reload a
// large model, and a human restarting it manually needs a window too. Past this
// the drive stops cleanly (the on-disk suite persists), so a re-run resumes.
// A var, not a const, so tests can shrink it to exercise the give-up path fast.
var backendRecoverBudget = 10 * time.Minute

// backendProbeInterval is the poll cadence of the wait-for-recovery loop. Each
// probe is a cheap GET /api/version (a few ms when up, ~3s timeout when down),
// so a few seconds between probes keeps the loop cheap while still resuming
// promptly once the server answers. A var so tests can shrink it.
var backendProbeInterval = 3 * time.Second

// backendAction is recoverBackendDown's verdict for a failed turn.
type backendAction int

const (
	// backendNotDown: the error is not a backend-unavailable error — the caller
	// proceeds to its existing overflow/terminal handling.
	backendNotDown backendAction = iota
	// backendRecovered: the backend was down but came back within budget — the
	// caller resets to a fresh context and resumes the phase from disk.
	backendRecovered
	// backendGaveUp: the backend stayed down past the budget (or ctx was
	// cancelled) — a resumable stop notice was printed and the caller ends the
	// drive cleanly (returns nil).
	backendGaveUp
)

// recoverBackendDown classifies a failed-turn error (P50.1). When it is a
// backend-unavailable error (server unreachable / model runner crashed) and the
// adapter exposes a liveness probe, it waits for the backend to recover and
// returns backendRecovered (the caller resets and resumes) or backendGaveUp
// (budget exhausted — resumable stop already announced). A non-backend error,
// or a backend with no liveness probe (e.g. a cloud adapter, where the retry
// decorator already handles transient outages), returns backendNotDown so the
// caller's existing paths handle it. `where` names the phase/step for notices.
func (st *State) recoverBackendDown(ctx context.Context, err error, where string) backendAction {
	if !provider.IsBackendUnavailableError(err) {
		return backendNotDown
	}
	// No liveness probe (non-Ollama, or the hook wasn't wired): we can't tell
	// when the backend is back, so don't spin — let the caller surface the
	// error through its normal terminal path.
	if st.CheckBackend == nil {
		return backendNotDown
	}
	if _, supported := st.CheckBackend(ctx); !supported {
		return backendNotDown
	}
	st.Logger.Warn("phased drive: model backend appears unavailable; waiting to resume from disk (P50.1)",
		"where", where, "err", err)
	fmt.Fprintf(st.ErrOut, "\n[notice: the model backend became unreachable during %s (the server may have crashed or been killed); waiting up to %s for it to come back, then resuming from disk — the suite on disk is intact]\n",
		where, backendRecoverBudget)
	st.tryAutostartBackend()
	if st.waitForBackend(ctx, where) {
		st.Logger.Info("phased drive: backend recovered; resetting to a fresh context and resuming", "where", where)
		fmt.Fprintf(st.ErrOut, "\n[notice: backend is reachable again — resetting to a fresh context and resuming %s from disk]\n", where)
		return backendRecovered
	}
	st.Logger.Warn("phased drive: backend did not recover within budget; stopping resumably", "where", where, "budget", backendRecoverBudget)
	fmt.Fprintf(st.ErrOut, "\n[notice: the model backend did not come back within %s during %s; stopping — the suite on disk is intact, so re-run once the backend is up to resume]\n",
		backendRecoverBudget, where)
	return backendGaveUp
}

// recoverPhase6Error is the phase-6 error classifier that folds the P50.1
// backend-down recovery in front of the existing P47.7 overflow recovery, so
// the three phase-6 turn sites (quality pass, verify fix, reopened content
// phase) get both with one call. A backend-down error is waited out and, on
// recovery, returns overflowRetry (the caller re-runs the turn against a fresh
// context — runPhase6Turn always rebuilds the conversation, so the reset is
// implicit), or overflowStop when the backend never returns. Anything else falls
// through to recoverPhase6Overflow and then to the P52.3 tool-failure recovery,
// both unchanged. It reuses the phase6OverflowAction verdict enum so the call
// sites' switch statements need no new case.
//
// toolFailResets is the P52.3 breaker's own reset budget, kept separate from
// overflowResets so the two failure modes cannot spend each other's allowance —
// see recoverToolFailureStall.
func (st *State) recoverPhase6Error(ctx context.Context, err error, where string, overflowResets, toolFailResets *int) phase6OverflowAction {
	switch st.recoverBackendDown(ctx, err, where) {
	case backendRecovered:
		return overflowRetry
	case backendGaveUp:
		return overflowStop
	}
	if a := st.recoverPhase6Overflow(err, where, overflowResets); a != overflowNotHandled {
		return a
	}
	return st.recoverToolFailureStall(err, where, toolFailResets)
}

// waitForBackend polls the liveness probe until the backend answers or the
// recovery budget expires (or ctx is cancelled). It returns true only when the
// backend became reachable. A single immediate probe short-circuits the common
// case where the server is already back by the time the error surfaced.
func (st *State) waitForBackend(ctx context.Context, where string) bool {
	deadline := time.Now().Add(backendRecoverBudget)
	ticker := time.NewTicker(backendProbeInterval)
	defer ticker.Stop()
	announced := false
	for {
		if healthy, supported := st.CheckBackend(ctx); supported && healthy {
			return true
		}
		if !announced {
			// One heartbeat so a human tailing the run sees the drive is
			// alive and waiting, not hung.
			st.Logger.Info("phased drive: waiting for model backend to return", "where", where)
			announced = true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// stopBackendUnavailable prints the resumable stop notice for a content phase
// whose backend never recovered, mirroring stopMaxTurns' shape so a re-run is
// the obvious next step.
func (st *State) stopBackendUnavailable(ph Phase, pending []string) {
	msg := fmt.Sprintf("backend unavailable during the %s phase with %d file(s) still PENDING: %s",
		ph.label(), len(pending), strings.Join(pending, ", "))
	st.Logger.Warn("chat: " + msg)
}

// tryAutostartBackend makes a best-effort `ollama serve` relaunch when
// AEGIS_OLLAMA_AUTOSTART=1 is set and `ollama` is on PATH. It is OPT-IN because
// spawning a server process is an environmental side effect a driven build
// should not take by default — the safe default is to wait for the operator (or
// an external supervisor like `brew services`/systemd) to bring it back. The
// spawn is detached and its output discarded; failure is logged and ignored,
// since waitForBackend is the real recovery signal either way.
func (st *State) tryAutostartBackend() {
	if !boolEnv("AEGIS_OLLAMA_AUTOSTART") {
		return
	}
	path, err := exec.LookPath("ollama")
	if err != nil {
		st.Logger.Warn("phased drive: AEGIS_OLLAMA_AUTOSTART set but `ollama` not found on PATH", "err", err)
		return
	}
	cmd := exec.Command(path, "serve")
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		st.Logger.Warn("phased drive: best-effort `ollama serve` relaunch failed", "err", err)
		return
	}
	st.Logger.Info("phased drive: launched `ollama serve` (AEGIS_OLLAMA_AUTOSTART); waiting for it to become reachable")
	fmt.Fprintf(st.ErrOut, "\n[notice: AEGIS_OLLAMA_AUTOSTART is set — launched `ollama serve`; waiting for it to become reachable]\n")
	// Let go of the process handle; we only care that the port answers.
	go func() { _ = cmd.Wait() }()
}

// boolEnv reports whether an env var is set to a truthy value (1/true/yes/on).
func boolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// --- P50.4: live per-turn progress heartbeat ---
//
// audit.jsonl is not flushed live and the drive logs only at phase boundaries,
// so a phase that hangs (or a backend that died, before P50.1's wait kicks in)
// is invisible until the whole run ends. Progress carries the current
// phase/turn/elapsed so a background ticker can emit a "still working"
// heartbeat during a long single turn, and each turn boundary logs a structured
// progress line. Together they make a stall observable — the precondition for
// any external supervision.

// heartbeatInterval is how often the background ticker logs progress during a
// long-running turn. Sized so a slow local-model turn (which can run minutes on
// a large context) still produces a periodic sign of life without spamming.
const heartbeatInterval = 30 * time.Second

// Progress is the shared, mutable progress snapshot the heartbeat ticker
// reads and the drive updates. Guarded by a mutex because the ticker goroutine
// reads it concurrently with the drive's writes.
type Progress struct {
	mu      sync.Mutex
	phase   string
	turn    int
	pending int
	started time.Time
}

// enter marks the start of a new phase, resetting the turn counter and clock.
func (p *Progress) enter(phase string) {
	p.mu.Lock()
	p.phase, p.turn, p.pending, p.started = phase, 0, 0, time.Now()
	p.mu.Unlock()
}

// tick records that a turn is starting in the current phase, with the count of
// files still PENDING, and returns the new turn index and phase-elapsed.
func (p *Progress) tick(pending int) (turn int, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turn++
	p.pending = pending
	return p.turn, time.Since(p.started)
}

// snapshot returns the current progress for the heartbeat ticker.
func (p *Progress) snapshot() (phase string, turn, pending int, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phase, p.turn, p.pending, time.Since(p.started)
}

// startHeartbeat launches a background ticker that logs the current phase's
// progress every heartbeatInterval until the returned stop func is called
// (deferred by the caller for the whole drive). It is a pure observability
// signal — it never touches the drive's state beyond reading the snapshot — so
// it is safe to run alongside everything else. A zero/empty phase (before the
// first phase starts) is skipped so we don't log a meaningless heartbeat.
func (st *State) startHeartbeat() func() {
	if st.Progress == nil {
		return func() {}
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				phase, turn, pending, elapsed := st.Progress.snapshot()
				if phase == "" {
					continue
				}
				st.Logger.Info("phased drive: heartbeat",
					"phase", phase, "turn", turn, "pending", pending,
					"phase_elapsed", elapsed.Round(time.Second).String())
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// logTurn records a structured per-turn progress line at a turn boundary — the
// always-on companion to the periodic heartbeat, so even a fast run leaves a
// legible progress trail (phase, turn, elapsed, pending) instead of only
// phase-start/phase-complete bookends.
func (st *State) logTurn(phase string, pending int) {
	if st.Progress == nil {
		return
	}
	turn, elapsed := st.Progress.tick(pending)
	st.Logger.Info("phased drive: turn",
		"phase", phase, "turn", turn, "pending", pending,
		"phase_elapsed", elapsed.Round(time.Second).String())
}
