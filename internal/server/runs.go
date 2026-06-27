package server

import (
	"sort"
	"sync"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
)

// runRegistry tracks in-flight message runs so concurrent, user-driven parallel
// sessions are observable (via GET /runs, the TUI, and `aegis runs`). It is
// purely informational; it does not gate execution.
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
