package webcache

import (
	"strconv"
	"testing"
)

func TestGetMissOnEmpty(t *testing.T) {
	c := New()
	if _, _, ok := c.Get("x"); ok {
		t.Fatal("expected miss on empty cache")
	}
}

func TestSetThenGetHits(t *testing.T) {
	c := New()
	c.Set("k", "v")
	content, age, ok := c.Get("k")
	if !ok || content != "v" {
		t.Fatalf("got (%q, %v, %v), want (%q, _, true)", content, age, ok, "v")
	}
	if age < 0 {
		t.Fatalf("negative age %v", age)
	}
}

func TestNilCacheIsANoOp(t *testing.T) {
	var c *Cache
	c.Set("k", "v") // must not panic
	if _, _, ok := c.Get("k"); ok {
		t.Fatal("nil cache must always miss")
	}
}

func TestEvictsOldestOnceFull(t *testing.T) {
	c := New()
	for i := range maxEntries {
		c.Set(keyN(i), "v")
	}
	if _, _, ok := c.Get(keyN(0)); !ok {
		t.Fatal("first key should still be present before overflow")
	}
	c.Set(keyN(maxEntries), "v") // one more, over the cap
	if _, _, ok := c.Get(keyN(0)); ok {
		t.Fatal("oldest key should have been evicted")
	}
	if _, _, ok := c.Get(keyN(maxEntries)); !ok {
		t.Fatal("newest key should be present")
	}
}

func keyN(n int) string {
	return "key-" + strconv.Itoa(n)
}
