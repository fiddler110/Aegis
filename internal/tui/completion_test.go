package tui

import "testing"

func names(items []cmdEntry) []string {
	out := make([]string, len(items))
	for i, e := range items {
		out[i] = e.name
	}
	return out
}

func TestComputeCompletion(t *testing.T) {
	all := builtinCommands

	tests := []struct {
		name       string
		value      string
		wantActive bool
		wantFirst  string // first match name when active
	}{
		{"plain text inactive", "hello", false, ""},
		{"empty inactive", "", false, ""},
		{"bare slash shows all", "/", true, "help"},
		{"prefix match", "/mo", true, "mode"},
		{"exact name", "/clear", true, "clear"},
		{"space closes popup", "/mode ", false, ""},
		{"newline closes popup", "/mode\n", false, ""},
		{"no match inactive", "/zzzzz", false, ""},
		{"substring matches description", "/permission", true, "mode"}, // "permission" is in /mode's description
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCompletion(tt.value, all, nil)
			if got.active != tt.wantActive {
				t.Fatalf("active = %v, want %v (items=%v)", got.active, tt.wantActive, names(got.items))
			}
			if tt.wantActive {
				if len(got.items) == 0 {
					t.Fatalf("active but no items")
				}
				if got.items[0].name != tt.wantFirst {
					t.Errorf("first match = %q, want %q (all=%v)", got.items[0].name, tt.wantFirst, names(got.items))
				}
			}
		})
	}
}

func TestComputeCompletionDescSubstring(t *testing.T) {
	// "transcript" appears in the description of /clear but not its name.
	got := computeCompletion("/transcript", builtinCommands, nil)
	if !got.active {
		t.Fatalf("expected active for description substring match")
	}
	if got.items[0].name != "clear" {
		t.Errorf("first match = %q, want clear", got.items[0].name)
	}
}

func TestFileCompletion(t *testing.T) {
	files := []string{"internal/tui/tui.go", "internal/tui/theme.go", "README.md"}

	tests := []struct {
		name       string
		value      string
		wantActive bool
		wantKind   completionKind
		wantFirst  string
		wantStart  int
	}{
		{"bare at lists ref types first", "@", true, compFile, "image:", 0},
		{"ref-type prefix", "@diag", true, compFile, "diagnostics", 0},
		{"base-name prefix", "@theme", true, compFile, "internal/tui/theme.go", 0},
		{"path prefix", "@internal/tui/tu", true, compFile, "internal/tui/tui.go", 0},
		{"mid-sentence mention", "look at @README", true, compFile, "README.md", 8},
		{"at without whitespace before is inactive", "foo@bar", false, compSlash, "", 0},
		{"completed mention closes", "@README.md ", false, compSlash, "", 0},
		{"ref value being typed closes", "@image:src/x", false, compSlash, "", 0},
		{"no file match inactive", "@zzzz", false, compSlash, "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCompletion(tt.value, builtinCommands, files)
			if got.active != tt.wantActive {
				t.Fatalf("active = %v, want %v", got.active, tt.wantActive)
			}
			if !tt.wantActive {
				return
			}
			if got.kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", got.kind, tt.wantKind)
			}
			if got.items[0].name != tt.wantFirst {
				t.Errorf("first = %q, want %q (all=%v)", got.items[0].name, tt.wantFirst, names(got.items))
			}
			if got.tokenStart != tt.wantStart {
				t.Errorf("tokenStart = %d, want %d", got.tokenStart, tt.wantStart)
			}
		})
	}
}

func TestCompletionMoveWraps(t *testing.T) {
	c := computeCompletion("/", builtinCommands, nil)
	n := len(c.items)
	c.move(-1)
	if c.selected != n-1 {
		t.Errorf("move(-1) from 0 = %d, want %d", c.selected, n-1)
	}
	c.move(1)
	if c.selected != 0 {
		t.Errorf("move(1) wrap = %d, want 0", c.selected)
	}
}
