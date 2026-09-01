package builtin

import (
	"path/filepath"
	"testing"
)

// TestShellCommandPaths pins shellCommandPaths (P81.30 / FIND-30): the
// engine's parallel-round dependency graph needs to know what a shell
// command's argv touches, reusing the same splitting/candidate-extraction
// classifyShellCommand and argvStaysInRoot already trust rather than a second
// parser.
func TestShellCommandPaths(t *testing.T) {
	clean := func(s string) string { return filepath.Clean(s) }

	cases := []struct {
		name       string
		command    string
		powershell bool
		wantPaths  []string
		wantOK     bool
	}{
		{
			name:      "bare cat of a known path",
			command:   "cat somefile",
			wantPaths: []string{clean("somefile")},
			wantOK:    true,
		},
		{
			name:      "dot-prefixed path cleans equal",
			command:   "cat ./somefile",
			wantPaths: []string{clean("somefile")},
			wantOK:    true,
		},
		{
			name:      "git diff names both operands, skips the subcommand",
			command:   "git diff a.txt b.txt",
			wantPaths: []string{clean("a.txt"), clean("b.txt")},
			wantOK:    true,
		},
		{
			name:      "git alone names nothing",
			command:   "git",
			wantPaths: nil,
			wantOK:    true,
		},
		{
			name:      "bare command with no operand resolves to no paths",
			command:   "pwd",
			wantPaths: nil,
			wantOK:    true,
		},
		{
			name:      "attached flag value is a path in a flag's clothing",
			command:   "grep -lfoo.txt bar",
			wantPaths: []string{clean("foo.txt"), clean("bar")},
			wantOK:    true,
		},
		{
			name:      "chaining metacharacter is unresolved",
			command:   "cat somefile; rm somefile",
			wantPaths: nil,
			wantOK:    false,
		},
		{
			name:      "redirection is unresolved",
			command:   "cat somefile > other",
			wantPaths: nil,
			wantOK:    false,
		},
		{
			name:      "unrecognized binary still yields its literal operand",
			command:   "totallyunknownbinary somefile",
			wantPaths: []string{clean("somefile")},
			wantOK:    true,
		},
		{
			name:      "path-qualified argv0 is unresolved",
			command:   "./scripts/cat somefile",
			wantPaths: nil,
			wantOK:    false,
		},
		{
			name:      "tilde expansion is unresolved",
			command:   "cat ~/.ssh/id_rsa",
			wantPaths: nil,
			wantOK:    false,
		},
		{
			name:      "glob is unresolved",
			command:   "cat *.txt",
			wantPaths: nil,
			wantOK:    false,
		},
		{
			name:      "empty command is unresolved",
			command:   "",
			wantPaths: nil,
			wantOK:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotPaths, gotOK := shellCommandPaths(c.command, c.powershell)
			if gotOK != c.wantOK {
				t.Fatalf("shellCommandPaths(%q) resolved = %v, want %v", c.command, gotOK, c.wantOK)
			}
			if len(gotPaths) != len(c.wantPaths) {
				t.Fatalf("shellCommandPaths(%q) = %q, want %q", c.command, gotPaths, c.wantPaths)
			}
			for i := range gotPaths {
				if gotPaths[i] != c.wantPaths[i] {
					t.Fatalf("shellCommandPaths(%q) = %q, want %q", c.command, gotPaths, c.wantPaths)
				}
			}
		})
	}
}

// TestShellToolTouchedPathsIntegratesWithClassifier exercises the real
// tool.PathToucher implementation on shellTool end to end, rather than only
// the underlying helper, so a wiring mistake between shell.go and
// shell_readonly.go (wrong field name, wrong powershell flag) would fail this
// even if shellCommandPaths itself were correct.
func TestShellToolTouchedPathsIntegratesWithClassifier(t *testing.T) {
	st := newShellTool(t.TempDir(), 5, nil, nil)
	paths, resolved := st.TouchedPaths(nil, []byte(`{"command":"cat somefile"}`))
	if !resolved {
		t.Fatalf("TouchedPaths resolved = false, want true")
	}
	want := filepath.Clean("somefile")
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("TouchedPaths = %q, want [%q]", paths, want)
	}

	if _, resolved := st.TouchedPaths(nil, []byte(`{"command":"cat a; rm a"}`)); resolved {
		t.Fatalf("TouchedPaths resolved a chained command, want unresolved")
	}

	if _, resolved := st.TouchedPaths(nil, []byte(`not json`)); resolved {
		t.Fatalf("TouchedPaths resolved malformed input, want unresolved")
	}
}
