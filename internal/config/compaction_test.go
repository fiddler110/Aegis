package config

import "testing"

// TestPreservePrefixCacheTriState pins the resolution rule for the P62.2 gate.
//
// The gate's default has now been argued in both directions on live evidence, so
// what this guards is not "which way is faster" but that the three states stay
// distinguishable: unset must defer to the caller's auto-detection, and an
// explicit value must beat it in both directions. A two-state bool would have
// made the 2026-08-08 A/B impossible to run — an optimization that cannot be
// switched off cannot be measured — and collapsing unset into either explicit
// value silently re-creates that.
func TestPreservePrefixCacheTriState(t *testing.T) {
	var unset CompactionConfig
	if !unset.PreservePrefixCacheOr(true) {
		t.Error("unset must defer to the auto-detected default (true), not override it")
	}
	if unset.PreservePrefixCacheOr(false) {
		t.Error("unset must defer to the auto-detected default (false), not override it")
	}

	on, off := true, false
	// An explicit value has to win over auto-detection in both directions, or
	// only one arm of the A/B is reachable.
	if !(CompactionConfig{PreservePrefixCache: &on}).PreservePrefixCacheOr(false) {
		t.Error("explicit true did not override an auto-detected false")
	}
	if (CompactionConfig{PreservePrefixCache: &off}).PreservePrefixCacheOr(true) {
		t.Error("explicit false did not override an auto-detected true — this is the escape hatch, " +
			"and the only way to revert the gate without a rebuild")
	}
}
