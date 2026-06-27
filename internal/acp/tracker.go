package acp

import (
	"fmt"
	"sync"
)

// toolTracker assigns stable ACP tool-call IDs to engine tool events, which
// carry only the tool name. Calls and results are paired FIFO per tool name,
// which is correct for sequential calls and stable (if approximate) for the
// rare parallel case.
type toolTracker struct {
	mu   sync.Mutex
	seq  int
	open map[string][]string // tool name -> queue of open call IDs
}

func newToolTracker() *toolTracker {
	return &toolTracker{open: map[string][]string{}}
}

// start registers a new tool call and returns its synthesized ID.
func (t *toolTracker) start(name string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	id := fmt.Sprintf("call_%d", t.seq)
	t.open[name] = append(t.open[name], id)
	return id
}

// current returns the ID of the oldest still-open call for name without
// removing it (used to reference a call awaiting permission).
func (t *toolTracker) current(name string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	q := t.open[name]
	if len(q) == 0 {
		return ""
	}
	return q[0]
}

// finish removes and returns the oldest open call ID for name.
func (t *toolTracker) finish(name string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	q := t.open[name]
	if len(q) == 0 {
		return ""
	}
	id := q[0]
	t.open[name] = q[1:]
	return id
}
