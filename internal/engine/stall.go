package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/heartbeat"
)

// stallWatch is P39.17's per-turn stall detector: it ends a run that has stopped
// producing *any* observable activity — no provider stream event, no tool call
// starting or finishing — for longer than its limit.
//
// It exists because every other guard in this package is progress-shaped and
// therefore blind to a hang. The no-progress nudge counts turns, the loop
// detector compares tool calls, and the tool-failure breaker counts failed
// rounds; all three require turns to keep *completing*. A turn that never
// returns advances no counter at all, so the 2026-08-09 drive that went silent
// at 18:12:51 and was still "running" fourteen minutes later — 0.5s of process
// CPU since launch, log unchanged, Ollama idle, one HTTP request outstanding —
// looked exactly like a slow run to everything in here.
//
// It is deliberately *not* the wall-clock budget with a smaller number.
// runBudget.wallClock bounds the whole run, so any value large enough to be safe
// for a legitimate multi-hour drive is far too large to catch a hang. "Nothing
// at all happened for fifteen minutes" is unambiguous in a way "this run has
// been going a while" can never be, which is why this one can be on by default
// and that one cannot.
//
// Relationship to provider.stream_idle_timeout (P59.2/P61.1): that key bounds
// the gap between two streamed chunks at the HTTP transport, and it is the first
// line of defence for the model phase. This is the backstop for when it does not
// fire — a request wedged before the response headers, a non-HTTP adapter, a
// sandbox exec that never returns — and it is the *only* bound that covers the
// tool-execution phase of a turn. Its default is therefore chosen to sit above
// every layer-specific timeout rather than to race them (see
// DefaultMaxTurnStall).
type stallWatch struct {
	limit time.Duration

	mu    sync.Mutex
	last  time.Time
	fired bool
	idle  time.Duration

	stop     chan struct{}
	stopOnce sync.Once
}

// stallPollDivisor sets how often the watchdog samples relative to its limit.
// Sampling is what bounds detection latency: a stall is reported somewhere
// between limit and limit*(1+1/divisor) after the last beat. Four is generous
// enough that the goroutine is free (one wake every 3m45s at the default) and
// tight enough that the reported figure is honest.
const stallPollDivisor = 4

// newStallWatch captures the engine's configured stall bound.
//
// It reads the real clock rather than e.now on purpose. e.now exists so a test
// can put a run *over* a budget without sleeping, and the fake it is given
// advances on every read — a background goroutine sampling the same fake would
// consume those reads and change what the wall-clock gates see. The stall tests
// use short real durations instead, exactly as the P59.2 mid-turn deadline tests
// already do.
func (e *Engine) newStallWatch() *stallWatch {
	return &stallWatch{limit: e.maxTurnStall, last: time.Now(), stop: make(chan struct{})}
}

// watch attaches the detector to ctx, returning a context that is cancelled if
// the run goes silent, and a stop function the caller must defer.
//
// Cancellation is the mechanism for the same reason the wall-clock budget grew
// one in P59.2: the thing being detected is a call that is not returning, so
// nothing polled between turns can end it. The classification is then re-derived
// by stalled/override, so the abort surfaces as a stall rather than as whatever
// the cancelled call happened to report.
//
// A non-positive limit disables the detector entirely: ctx is returned
// unchanged, no goroutine starts, and beat becomes a no-op.
func (s *stallWatch) watch(ctx context.Context) (context.Context, context.CancelFunc) {
	if s == nil || s.limit <= 0 {
		return ctx, func() {}
	}
	ctx = withStallBeat(ctx, s)
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
	go s.loop(ctx, cancel)
	return ctx, func() {
		s.stopOnce.Do(func() { close(s.stop) })
		cancel()
	}
}

// loop samples the idle gap until the run ends or the gap crosses the limit.
func (s *stallWatch) loop(ctx context.Context, cancel context.CancelFunc) {
	interval := s.limit / stallPollDivisor
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			idle := time.Since(s.last)
			if idle < s.limit || s.fired {
				s.mu.Unlock()
				continue
			}
			s.fired, s.idle = true, idle
			s.mu.Unlock()
			cancel()
			return
		}
	}
}

// beat records observable activity. Called on every provider stream event and on
// both edges of every tool execution — the two phases a turn is made of, and the
// two places the 2026-08-09 hang could have been sitting.
func (s *stallWatch) beat() {
	if s == nil || s.limit <= 0 {
		return
	}
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
}

// stalled reports the stall abort, or nil if the detector never fired.
func (s *stallWatch) stalled() error {
	if s == nil || s.limit <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.fired {
		return nil
	}
	// Rounded to seconds at production scale, to milliseconds below a minute:
	// a real stall reports "15m2s" rather than "15m2.317s", but the short bounds
	// the tests use must not all render as "0s".
	idle := s.idle.Round(time.Millisecond)
	if idle >= time.Minute {
		idle = idle.Round(time.Second)
	}
	return fmt.Errorf("%w: no model output and no tool activity for %s (limit %s) — "+
		"the turn is hung, not slow; raise cost.max_turn_stall for legitimately long silent work, or set it to 0 to disable the check",
		ErrTurnStalled, idle, s.limit)
}

// override re-attributes an error produced by this detector's own cancellation,
// mirroring runBudget.override. Without it the abort would surface as whatever
// the cancelled call returned — a bare ErrInterrupted, or an adapter transport
// error a caller classifies as backend-unavailable and would then wait for and
// resume, turning a detected hang into a retry loop against a wedged backend.
func (s *stallWatch) override(err error) error {
	if stallErr := s.stalled(); stallErr != nil {
		return stallErr
	}
	return err
}

// withStallBeat carries the run's detector down to turn() and the tool
// executors. It travels in the context rather than in five signatures because it
// is run-scoped state that must also reach the goroutines of a parallel tool
// round — the same reason tool.WithRegistry/WithWorkdir already ride there. The
// value is never read for behaviour, only beaten on.
//
// It delegates to internal/heartbeat, which *chains* rather than shadows
// (P66.8 / ARCH-04). Before that, this function was a bare context.WithValue on
// a key of its own, so a sub-agent's engine — running under a context derived
// from its parent's tool context — installed its detector over the same key and
// hid the parent's. Every child beat landed on the child, the parent's watch saw
// nothing for the whole fan-out, and a healthy multi-agent round was aborted as
// ErrTurnStalled, which is fatal to a drive. The chain also lets the agent tool
// and the provider admission queue beat, which is why the plumbing had to move
// into a leaf package: internal/tool imports internal/provider, so no home
// inside those three is reachable from the other two.
func withStallBeat(ctx context.Context, s *stallWatch) context.Context {
	if s == nil || s.limit <= 0 {
		return ctx
	}
	return heartbeat.With(ctx, s.beat)
}

// beat is the call site's view of the detector: a no-op when none is attached,
// and a beat on *every* watch in the chain when several are.
func beat(ctx context.Context) {
	heartbeat.Beat(ctx)
}
