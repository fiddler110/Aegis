package swarm

import (
	"sort"
	"sync"
	"time"
)

// Status is a teammate's lifecycle state.
type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Member is a registry entry for one teammate.
type Member struct {
	Identity  Identity
	Status    Status
	Summary   string
	StartedAt time.Time
	EndedAt   time.Time
}

// Registry tracks active and finished teammates. It is safe for concurrent use.
type Registry struct {
	mu      sync.Mutex
	members map[string]*Member // keyed by AgentID
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{members: map[string]*Member{}}
}

// Add records a newly spawned teammate as running.
func (r *Registry) Add(id Identity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.members[id.AgentID] = &Member{Identity: id, Status: StatusRunning, StartedAt: time.Now()}
}

// Update sets a teammate's terminal status and summary.
func (r *Registry) Update(agentID string, status Status, summary string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[agentID]
	if !ok {
		return
	}
	m.Status = status
	m.Summary = summary
	m.EndedAt = time.Now()
}

// Get returns a copy of the member with the given id.
func (r *Registry) Get(agentID string) (Member, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[agentID]
	if !ok {
		return Member{}, false
	}
	return *m, true
}

// List returns all members, most recently started first.
func (r *Registry) List() []Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Member, 0, len(r.members))
	for _, m := range r.members {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}
