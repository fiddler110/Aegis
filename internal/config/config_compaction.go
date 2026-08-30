package config

import (
	"strings"
	"time"
)

// CompactionConfig tunes context compaction. Everything here is an override of
// an auto-detected default, not a switch that has to be set.
type CompactionConfig struct {
	// PreservePrefixCache overrides the headroom gate on the deterministic prune
	// pre-pass. Unset (nil) auto-detects: on a local backend the gate is on,
	// because rewriting the middle of a conversation discards the
	// llama.cpp/Ollama prefix KV cache and costs a full prefill recompute;
	// against a cloud provider there is no such cache and the gate is off.
	//
	// It is settable at all because the gate is an *optimization*, and an
	// optimization that cannot be switched off cannot be A/B'd or reverted
	// without a rebuild — which is the position P62.2 was stuck in.
	//
	// # The measurement, and why it took two passes to read correctly
	//
	// Measured 2026-08-08, the gate lost badly: 3m19s against 1m32s on the same
	// fixture, with the deferred turns costing ~23.7s each. That reading stood
	// long enough to be acted on, and it was an artifact of a broken instrument.
	// The engine's compaction trigger ran on an estimate that undercounted the
	// real prompt by 20-33% (P62.4), so compaction fired so late that *both*
	// arms were already inside the regime where Ollama context-shifts — where
	// the prefix cache is gone regardless and the gate has nothing left to
	// protect.
	//
	// Re-measured after P62.4 corrected the estimate, on the same fixture and
	// model, twice:
	//
	//	gate on:  1m16s / 1m27s wall, ~54,287ms prefill
	//	gate off: 2m7s  / 2m7s  wall, ~98,481ms prefill
	//
	// The gate now wins ~1.7x on wall clock with no overflows in either arm, and
	// the per-turn trace shows why: once past the trigger, gate-off prunes on
	// *every* turn for a small yield (message counts unchanged, 11->11, 13->13)
	// and pays a full ~9s prefill each time, while gate-on stays append-only at
	// ~2.5s and takes that hit three times.
	//
	// The durable lesson is about method rather than about caching: a
	// measurement of an optimization is only as good as the instrument the
	// system was running on, and this one was measured in a regime that existed
	// only because a different component was broken.
	PreservePrefixCache *bool `koanf:"preserve_prefix_cache"`

	// ColdCacheAfter is the idle gap after which a resumed conversation has its
	// stale, re-fetchable tool results cleared before the next request (P67.6) —
	// a trigger on cache *temperature*, orthogonal to the context-pressure
	// trigger everything else here tunes. "0" or "off" disables it.
	//
	// The default is ColdCacheAfterDefault. It is a duration rather than a bool
	// because the right answer is a property of the backend's cache TTL, and
	// those differ: Ollama unloads an idle model after 5 minutes by default,
	// Anthropic's prompt cache expires after 5 minutes or an hour depending on
	// the tier. Past any of them the prefix this request re-sends has to be
	// recomputed anyway, which is exactly when clearing it becomes free.
	//
	// It is a string so "off" and "20m" are both sayable; use ColdCacheAfterOr.
	ColdCacheAfter string `koanf:"cold_cache_after"`

	// ColdCacheKeep is how many of the most recent clearable tool results the
	// cold-cache pass leaves verbatim. 0 takes the package default (3); the pass
	// itself floors it at 1, because clearing every result leaves the model with
	// no working context at all.
	ColdCacheKeep int `koanf:"cold_cache_keep"`
}

// ColdCacheAfterDefault is the idle gap at which the P67.6 cold-cache pass fires
// when compaction.cold_cache_after is unset.
//
// Twenty minutes, chosen to sit clear of every cache TTL this ships against
// rather than to split the difference between them: Ollama's default keep-alive
// is 5 minutes, Anthropic's default prompt-cache TTL is 5 minutes and its
// extended tier is 1 hour. Below ~5 minutes the pass would fire on a cache that
// is still warm and throw away context for nothing; at 20 minutes the two
// 5-minute TTLs have certainly expired, and the 1-hour tier is a cloud backend
// where the pass costs little either way. It is a default, not a finding — no
// live measurement has been taken at any value, and the config knob exists so
// one can be.
const ColdCacheAfterDefault = 20 * time.Minute

// ColdCacheAfterOr resolves compaction.cold_cache_after to a duration: unset
// means ColdCacheAfterDefault, "off"/"0"/"none" means disabled, anything else is
// parsed as a Go duration. An unparseable value returns the default and false,
// so the caller can warn rather than silently disabling a feature the user was
// trying to tune.
func (c CompactionConfig) ColdCacheAfterOr() (time.Duration, bool) {
	v := strings.TrimSpace(strings.ToLower(c.ColdCacheAfter))
	switch v {
	case "":
		return ColdCacheAfterDefault, true
	case "off", "none", "false", "0":
		return 0, true
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return ColdCacheAfterDefault, false
	}
	return d, true
}

// PreservePrefixCacheOr resolves the tri-state against an auto-detected
// default, which is what the caller would have used before the override
// existed.
func (c CompactionConfig) PreservePrefixCacheOr(auto bool) bool {
	if c.PreservePrefixCache == nil {
		return auto
	}
	return *c.PreservePrefixCache
}
