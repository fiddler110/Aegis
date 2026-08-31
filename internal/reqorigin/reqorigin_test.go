package reqorigin

import (
	"context"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		TUI: TUI, Web: Web, ACP: ACP, MCP: MCP, CLI: CLI,
		"":      Web,
		"bogus": Web,
		"TUI":   Web, // case-sensitive: not a free-form match
		" tui ": Web,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValid(t *testing.T) {
	for _, v := range []string{TUI, Web, ACP, MCP, CLI} {
		if !Valid(v) {
			t.Errorf("Valid(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "bogus", "TUI"} {
		if Valid(v) {
			t.Errorf("Valid(%q) = true, want false", v)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := FromContext(ctx); got != "" {
		t.Errorf("FromContext on bare context = %q, want empty", got)
	}
	ctx = WithOrigin(ctx, MCP)
	if got := FromContext(ctx); got != MCP {
		t.Errorf("FromContext = %q, want %q", got, MCP)
	}
}
