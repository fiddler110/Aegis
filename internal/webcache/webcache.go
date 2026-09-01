// Package webcache memoizes web_fetch/web_search results within one session
// (P71.6), so a re-fetch of a URL or re-issue of a query already seen this
// session costs neither a network round-trip nor the token spend of a fresh
// result. Session-scoped rather than daemon-wide: content served to one
// session should never leak into another's context just because both asked
// for the same URL, and a cache with no owner would grow forever in a daemon
// serving many sessions. The deep-research skill's audit trail already
// claims this behavior ("prevents re-fetching the same dead ends when a
// topic gets revisited") — that claim lived entirely in the model's own
// context before this, which compaction deletes, so a re-fetch after
// compaction was both likely and silently expensive. This cache makes the
// claim true independent of what the model remembers.
package webcache

import (
	"sync"
	"time"
)

// maxEntries bounds one session's cache so an open-ended research session
// cannot grow it without limit. Eviction is oldest-first once the cap is
// hit — the same posture internal/checkpoint takes on snapshot count, not a
// byte budget, since a cached page is already capped in size by web_fetch's
// own truncation before it ever reaches here.
const maxEntries = 200

type entry struct {
	content   string
	fetchedAt time.Time
}

// Cache is a session-scoped, thread-safe key/value store from a normalized
// fetch or search key to the content last retrieved for it. A nil *Cache is
// a valid no-op receiver on every method, matching egress.Tracker's
// convention, so a call site with no session-scoped cache to offer (a
// one-shot CLI run, a sub-agent with none wired) can pass nil rather than
// special-casing it.
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
	order   []string // insertion order, oldest first, for eviction
}

// New returns an empty Cache.
func New() *Cache {
	return &Cache{entries: make(map[string]entry)}
}

// Get returns the content cached under key and how long ago it was stored.
// ok is false on a miss or on a nil Cache.
func (c *Cache) Get(key string) (content string, age time.Duration, ok bool) {
	if c == nil {
		return "", 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.entries[key]
	if !found {
		return "", 0, false
	}
	return e.content, time.Since(e.fetchedAt), true
}

// Set stores content under key, evicting the oldest entry first if the
// cache is already at maxEntries. A nil Cache is a no-op.
func (c *Cache) Set(key, content string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		if len(c.order) >= maxEntries {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = entry{content: content, fetchedAt: time.Now()}
}
