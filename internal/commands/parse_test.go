package commands

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input string
		want  *ParsedCommand
	}{
		{"hello world", nil},
		{"", nil},
		{"/", nil},
		{"/  ", nil},
		{"/help", &ParsedCommand{Name: "help", Args: []string{}, Raw: "/help"}},
		{"/Help", &ParsedCommand{Name: "help", Args: []string{}, Raw: "/Help"}},
		{"/persona security", &ParsedCommand{Name: "persona", Args: []string{"security"}, Raw: "/persona security"}},
		{"/review main.go security", &ParsedCommand{Name: "review", Args: []string{"main.go", "security"}, Raw: "/review main.go security"}},
		{"  /quit  ", &ParsedCommand{Name: "quit", Args: []string{}, Raw: "/quit"}},
		{"/remember this is a long note", &ParsedCommand{Name: "remember", Args: []string{"this", "is", "a", "long", "note"}, Raw: "/remember this is a long note"}},
	}
	for _, tt := range tests {
		got := Parse(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("Parse(%q) = %+v, want nil", tt.input, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("Parse(%q) = nil, want %+v", tt.input, tt.want)
			continue
		}
		if got.Name != tt.want.Name {
			t.Errorf("Parse(%q).Name = %q, want %q", tt.input, got.Name, tt.want.Name)
		}
		if !reflect.DeepEqual(got.Args, tt.want.Args) {
			t.Errorf("Parse(%q).Args = %v, want %v", tt.input, got.Args, tt.want.Args)
		}
		if got.Raw != tt.want.Raw {
			t.Errorf("Parse(%q).Raw = %q, want %q", tt.input, got.Raw, tt.want.Raw)
		}
	}
}
