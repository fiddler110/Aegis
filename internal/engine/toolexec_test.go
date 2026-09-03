package engine

import "testing"

// TestRecoveredCallProvenance pins the P81.28/FIND-28 labeling: a call id
// from either recovery path (the always-on prose-salvage decorator's fixed
// id, or one of the tool_call_shim's per-call "shim-"/"shim-mixed-" ids)
// reports a provenance note; an ordinary native call id reports none.
func TestRecoveredCallProvenance(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"tu_salvaged", "recovered from prose"},
		{"shim-3-0", "recovered from prose (tool-call shim)"},
		{"shim-mixed-3-0", "recovered from prose (tool-call shim)"},
		{"toolu_01abc", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := recoveredCallProvenance(tt.id); got != tt.want {
			t.Errorf("recoveredCallProvenance(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
