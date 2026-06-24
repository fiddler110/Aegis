// Package task tracks long-running background jobs — backgrounded shell
// commands and detached sub-agents — so the model can launch work, keep
// going, and poll for the result later. Jobs are persisted in the shared
// session database so a listing survives a daemon restart (though a process
// restart necessarily orphans any still-running goroutine).
package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// State is a background job's lifecycle stage.
type State string

const (
	StateRunning State = "running" // still executing
	StateDone    State = "done"    // finished successfully
	StateFailed  State = "failed"  // finished with an error
	StateStopped State = "stopped" // cancelled by the user/model
)

// Terminal reports whether the state is final (no longer running).
func (s State) Terminal() bool { return s != StateRunning }

// Task is one background job.
type Task struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Kind      string    `json:"kind"`  // "shell" | "subagent" | ...
	Title     string    `json:"title"` // short human label
	State     State     `json:"state"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Spec describes a job to start.
type Spec struct {
	SessionID string
	Kind      string
	Title     string
}

// RunFunc executes a background job to completion. It may stream incremental
// output through emit; its returned string becomes the final output (appended
// to anything already emitted is the caller's choice — emit and the return are
// independent, so a RunFunc should use one or the other). ctx is cancelled when
// the task is stopped or the manager shuts down.
type RunFunc func(ctx context.Context, emit func(string)) (string, error)

// ErrNotFound is returned for an unknown task id.
var ErrNotFound = errors.New("task not found")

// Manager starts, tracks, and stops background jobs.
type Manager struct {
	store  *Store
	logger *slog.Logger

	mu   sync.Mutex
	live map[string]*liveTask
}

// liveTask holds the in-process state for a running job.
type liveTask struct {
	cancel  context.CancelFunc
	buf     outputBuffer
	done    chan struct{}
	stopped bool // Stop() was called; finish() should record StateStopped
}

// NewManager builds a Manager over the given store.
func NewManager(store *Store, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{store: store, logger: logger, live: map[string]*liveTask{}}
}

// Start launches fn as a background job and returns the persisted task record
// immediately. The job runs under a context detached from the caller's, so it
// survives the request that created it; it is cancelled by Stop or Shutdown.
func (m *Manager) Start(spec Spec, fn RunFunc) (*Task, error) {
	if spec.Kind == "" {
		spec.Kind = "generic"
	}
	now := time.Now()
	t := &Task{
		ID:        uuid.NewString(),
		SessionID: spec.SessionID,
		Kind:      spec.Kind,
		Title:     spec.Title,
		State:     StateRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := m.store.Save(context.Background(), t); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	lt := &liveTask{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	m.live[t.ID] = lt
	m.mu.Unlock()

	go func() {
		defer close(lt.done)
		out, err := safeRun(ctx, fn, lt.buf.append)
		m.finish(t.ID, lt, out, err)
	}()
	return t, nil
}

// safeRun runs fn, converting a panic into an error so a misbehaving job cannot
// take down the daemon.
func safeRun(ctx context.Context, fn RunFunc, emit func(string)) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("task panicked: %v", r)
		}
	}()
	return fn(ctx, emit)
}

// finish records a job's terminal state and persists its final output.
func (m *Manager) finish(id string, lt *liveTask, out string, runErr error) {
	m.mu.Lock()
	stopped := lt.stopped
	delete(m.live, id)
	m.mu.Unlock()

	t, err := m.store.Get(context.Background(), id)
	if err != nil {
		m.logger.Error("task finish: load", "task", id, "err", err)
		return
	}
	// Prefer streamed output; fall back to the returned string.
	if streamed := lt.buf.string(); streamed != "" {
		t.Output = streamed
	} else {
		t.Output = out
	}
	switch {
	case stopped:
		t.State = StateStopped
	case runErr != nil:
		t.State = StateFailed
		t.Error = runErr.Error()
	default:
		t.State = StateDone
	}
	t.UpdatedAt = time.Now()
	if err := m.store.Save(context.Background(), t); err != nil {
		m.logger.Error("task finish: save", "task", id, "err", err)
	}
}

// Get returns a task, merging any live streamed output for a running job.
func (m *Manager) Get(id string) (*Task, error) {
	t, err := m.store.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	lt, ok := m.live[id]
	m.mu.Unlock()
	if ok {
		t.Output = lt.buf.string()
	}
	return t, nil
}

// List returns tasks for a session (all sessions when sessionID is empty),
// newest first.
func (m *Manager) List(sessionID string) ([]*Task, error) {
	tasks, err := m.store.List(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	for _, t := range tasks {
		if lt, ok := m.live[t.ID]; ok {
			t.Output = lt.buf.string()
		}
	}
	m.mu.Unlock()
	return tasks, nil
}

// SetTitle renames a task.
func (m *Manager) SetTitle(id, title string) error {
	t, err := m.store.Get(context.Background(), id)
	if err != nil {
		return err
	}
	t.Title = title
	t.UpdatedAt = time.Now()
	return m.store.Save(context.Background(), t)
}

// Stop cancels a running task. It is a no-op (nil error) for a task that has
// already finished.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	lt, ok := m.live[id]
	if ok {
		lt.stopped = true
		lt.cancel()
	}
	m.mu.Unlock()
	if !ok {
		// Not live: either unknown or already finished.
		if _, err := m.store.Get(context.Background(), id); err != nil {
			return err
		}
		return nil
	}
	return nil
}

// Wait blocks until the task finishes (test/shutdown helper). Returns the final
// task record. ok is false if the id is unknown and not currently live.
func (m *Manager) Wait(id string) (*Task, bool) {
	m.mu.Lock()
	lt, ok := m.live[id]
	m.mu.Unlock()
	if ok {
		<-lt.done
	}
	t, err := m.store.Get(context.Background(), id)
	if err != nil {
		return nil, false
	}
	return t, true
}

// Shutdown cancels all live tasks and waits for them to finish (or ctx to
// cancel).
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	live := make([]*liveTask, 0, len(m.live))
	for _, lt := range m.live {
		lt.stopped = true
		lt.cancel()
		live = append(live, lt)
	}
	m.mu.Unlock()

	for _, lt := range live {
		select {
		case <-lt.done:
		case <-ctx.Done():
			return
		}
	}
}

// outputBuffer is a goroutine-safe append-only text buffer.
type outputBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *outputBuffer) append(s string) {
	b.mu.Lock()
	b.buf = append(b.buf, s...)
	b.mu.Unlock()
}

func (b *outputBuffer) string() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
