package server

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/api"
)

// runRegistry tracks in-flight message runs so concurrent, user-driven parallel
// sessions are observable (via GET /runs, the TUI, and `aegis runs`). It is
// purely informational for run listing; the one exception is cancel (P28.5),
// which exists solely so a resumable run — one whose lifetime was
// deliberately decoupled from its HTTP request context, see
// api.PostMessageRequest.Resumable — can still be stopped on request.
type runRegistry struct {
	mu   sync.Mutex
	runs map[string]*runState
}

type runState struct {
	sessionID string
	title     string
	startedAt time.Time
	tools     int
	lastKind  string
	// cancel stops this run out of band. Only set for resumable/background
	// runs (P28.5) — a normal run is instead stopped by the client tearing
	// down its HTTP request, which cancels the context the engine already
	// runs on, so it needs no separate cancel func here.
	cancel context.CancelFunc
}

func newRunRegistry() *runRegistry {
	return &runRegistry{runs: map[string]*runState{}}
}

// start records a new active run keyed by its unique run id.
func (r *runRegistry) start(runID, sessionID, title string) {
	r.mu.Lock()
	r.runs[runID] = &runState{sessionID: sessionID, title: title, startedAt: time.Now(), lastKind: "started"}
	r.mu.Unlock()
}

// observe updates a run's activity from an emitted event kind.
func (r *runRegistry) observe(runID string, kind api.EventKind) {
	r.mu.Lock()
	if st := r.runs[runID]; st != nil {
		st.lastKind = string(kind)
		if kind == api.KindToolCall {
			st.tools++
		}
	}
	r.mu.Unlock()
}

// finish removes a run from the active set.
func (r *runRegistry) finish(runID string) {
	r.mu.Lock()
	delete(r.runs, runID)
	r.mu.Unlock()
}

// setCancel attaches the out-of-band cancel func for a resumable run (P28.5).
// A no-op if the run has already finished by the time the caller gets here.
func (r *runRegistry) setCancel(runID string, cancel context.CancelFunc) {
	r.mu.Lock()
	if st := r.runs[runID]; st != nil {
		st.cancel = cancel
	}
	r.mu.Unlock()
}

// stopSession cancels the active resumable run for a session, if any. Returns
// false when no run is active for the session or the active run isn't
// resumable (no cancel registered) — e.g. a plain TUI/CLI run, which is
// stopped by the client disconnecting instead. Sessions serialize their own
// runs to at most one at a time, so "the" run for a session is unambiguous.
func (r *runRegistry) stopSession(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.runs {
		if st.sessionID == sessionID && st.cancel != nil {
			st.cancel()
			return true
		}
	}
	return false
}

// list returns active runs, newest first.
func (r *runRegistry) list() []api.RunInfo {
	r.mu.Lock()
	out := make([]api.RunInfo, 0, len(r.runs))
	for id, st := range r.runs {
		out = append(out, api.RunInfo{
			RunID:     id,
			SessionID: st.sessionID,
			Title:     st.title,
			StartedAt: st.startedAt,
			Tools:     st.tools,
			LastKind:  st.lastKind,
		})
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}
