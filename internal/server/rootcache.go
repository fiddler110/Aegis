package server

import "sync"

// rootCache lazily creates and caches one T per root directory (P25.9). It
// exists so knowledge stores, repo-map blocks, and command registries — each
// otherwise a daemon-wide singleton fixed to the daemon's own workspace —
// can be re-resolved per session Workdir without rebuilding the same
// lock/lazy-init logic for each of them. One mutex per cache (not a global
// one shared across resource types) serializes creation for that resource
// only; session creation and first-tool-call are not hot paths, so this
// favors correctness (no duplicate creation for the same root) over
// micro-concurrency. The zero value is ready to use.
type rootCache[T any] struct {
	mu     sync.Mutex
	byRoot map[string]T
}

// getOrCreate returns the cached value for root, calling create to populate
// it on a cache miss. A failing create is not cached, so the next call
// retries instead of permanently sticking with an error.
func (c *rootCache[T]) getOrCreate(root string, create func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.byRoot[root]; ok {
		return v, nil
	}
	v, err := create()
	if err != nil {
		var zero T
		return zero, err
	}
	if c.byRoot == nil {
		c.byRoot = map[string]T{}
	}
	c.byRoot[root] = v
	return v, nil
}

// values returns every cached value, for shutdown cleanup.
func (c *rootCache[T]) values() []T {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]T, 0, len(c.byRoot))
	for _, v := range c.byRoot {
		out = append(out, v)
	}
	return out
}
