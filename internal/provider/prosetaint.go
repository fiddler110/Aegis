package provider

import (
	"hash/fnv"

	"github.com/fiddler110/aegis/internal/trust"
)

// taintWindowBytes is the shingle length used to detect a prose tool-call
// candidate that reproduces untrusted content, rather than one the model
// composed itself (P81.28/FIND-28, remediation #1). Long enough that a
// coincidental match on ordinary JSON tokens (`{"name":`, `"arguments":`) is
// vanishingly unlikely; short enough to still catch a call quoted back out of
// a tool result with only minor surrounding reformatting.
const taintWindowBytes = 32

// proseTaintIndex is a set of content-hash shingles drawn from every
// untrusted-content-wrapped tool result in a turn's message history
// (trust.Wrap/trust.IsWrapped — P81.1's provenance marker, reused here for
// containment rather than the approval-gate purpose it was built for).
type proseTaintIndex struct {
	shingles map[uint64]struct{}
}

// buildProseTaintIndex scans msgs for tool results carrying the untrusted-
// content marker and indexes each one by fixed-length shingle hash. A turn
// with no untrusted content in its history (the common case — no MCP or web
// tool used) returns nil, so reproducesUntrustedContent short-circuits to
// false without hashing anything.
func buildProseTaintIndex(msgs []Message) *proseTaintIndex {
	var idx *proseTaintIndex
	for _, m := range msgs {
		for _, b := range m.Content {
			tr, ok := b.(ToolResultBlock)
			if !ok || !trust.IsWrapped(tr.Content) {
				continue
			}
			if idx == nil {
				idx = &proseTaintIndex{shingles: make(map[uint64]struct{})}
			}
			idx.index(tr.Content)
		}
	}
	return idx
}

func (t *proseTaintIndex) index(content string) {
	if len(content) < taintWindowBytes {
		return
	}
	h := fnv.New64a()
	for i := 0; i+taintWindowBytes <= len(content); i++ {
		h.Reset()
		h.Write([]byte(content[i : i+taintWindowBytes]))
		t.shingles[h.Sum64()] = struct{}{}
	}
}

// reproducesUntrustedContent reports whether span shares a taintWindowBytes
// shingle with any untrusted content indexed into t — i.e. whether span was
// very likely copied out of a tool result the harness marked untrusted,
// rather than composed fresh by the model.
//
// A nil index (no untrusted content reached this turn) or a span shorter than
// the shingle window (nothing to hash) both report false: there is nothing
// span could have reproduced.
func (t *proseTaintIndex) reproducesUntrustedContent(span string) bool {
	if t == nil || len(span) < taintWindowBytes {
		return false
	}
	h := fnv.New64a()
	for i := 0; i+taintWindowBytes <= len(span); i++ {
		h.Reset()
		h.Write([]byte(span[i : i+taintWindowBytes]))
		if _, ok := t.shingles[h.Sum64()]; ok {
			return true
		}
	}
	return false
}
