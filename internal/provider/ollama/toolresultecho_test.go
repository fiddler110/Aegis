package ollama

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

func toolUse(id, name, args string) provider.ToolUseBlock {
	return provider.ToolUseBlock{ID: id, Name: name, Input: json.RawMessage(args)}
}

func toolResults(blocks ...provider.Block) provider.Message {
	return provider.Message{Role: provider.RoleUser, Content: blocks}
}

// TestP5216EchoOnlyOnAmbiguousRound is the P52.16 regression: a round that calls
// each tool once is unambiguous under Ollama's name-only correlation, and must
// keep today's exact bytes — the echo costs tokens and, if it were applied
// unconditionally, would rewrite the encoding of every historical result and
// break the prefix cache translate is careful to preserve.
func TestP5216EchoOnlyOnAmbiguousRound(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "look"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			toolUse("tu_0", "read_file", `{"path":"a.go"}`),
			toolUse("tu_1", "grep", `{"pattern":"func"}`),
		}},
		toolResults(
			provider.ToolResultBlock{ToolUseID: "tu_0", Content: "package a"},
			provider.ToolResultBlock{ToolUseID: "tu_1", Content: "3 matches"},
		),
	}
	out := translate("", msgs, false)

	var results []wireMessage
	for _, m := range out {
		if m.Role == "tool" {
			results = append(results, m)
		}
	}
	if len(results) != 2 {
		t.Fatalf("got %d tool messages, want 2", len(results))
	}
	if results[0].Content != "package a" || results[1].Content != "3 matches" {
		t.Errorf("unambiguous round was rewritten: %q / %q — must stay byte-identical",
			results[0].Content, results[1].Content)
	}
}

// TestP5216EchoDisambiguatesSameToolParallelCalls covers the case the echo
// exists for: three parallel read_file calls, whose results are indistinguishable
// in wire metadata (all role:"tool", tool_name:"read_file", no ID).
func TestP5216EchoDisambiguatesSameToolParallelCalls(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "read them"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			toolUse("tu_0", "read_file", `{"path":"internal/engine/engine.go"}`),
			toolUse("tu_1", "read_file", `{"path":"internal/server/server.go"}`),
			toolUse("tu_2", "read_file", `{"path":"cmd/aegis/main.go"}`),
		}},
		toolResults(
			provider.ToolResultBlock{ToolUseID: "tu_0", Content: "engine body"},
			provider.ToolResultBlock{ToolUseID: "tu_1", Content: "server body"},
			provider.ToolResultBlock{ToolUseID: "tu_2", Content: "main body"},
		),
	}
	out := translate("", msgs, false)

	want := []struct{ path, body string }{
		{"internal/engine/engine.go", "engine body"},
		{"internal/server/server.go", "server body"},
		{"cmd/aegis/main.go", "main body"},
	}
	var i int
	for _, m := range out {
		if m.Role != "tool" {
			continue
		}
		if m.ToolName != "read_file" {
			t.Errorf("tool_name = %q, want read_file", m.ToolName)
		}
		prefix := "[read_file path=" + want[i].path + "]"
		if !strings.HasPrefix(m.Content, prefix) {
			t.Errorf("result %d content = %q, want prefix %q", i, m.Content, prefix)
		}
		if !strings.HasSuffix(m.Content, want[i].body) {
			t.Errorf("result %d lost its payload: %q", i, m.Content)
		}
		i++
	}
	if i != 3 {
		t.Fatalf("got %d tool messages, want 3", i)
	}
}

// TestP5216EchoIsDeterministic guards the prefix cache: the echo must render
// identically for the same call every turn, so argument key order in the JSON
// cannot leak into the wire bytes.
func TestP5216EchoIsDeterministic(t *testing.T) {
	a := toolResultEcho("search", json.RawMessage(`{"query":"foo","limit":10,"deep":true}`))
	b := toolResultEcho("search", json.RawMessage(`{"limit":10,"deep":true,"query":"foo"}`))
	if a != b {
		t.Errorf("echo is order-dependent: %q vs %q", a, b)
	}
	if want := "[search deep=true limit=10 query=foo]"; a != want {
		t.Errorf("echo = %q, want %q", a, want)
	}
}

// TestP5216EchoBoundsValueLength keeps a bulky inline argument from duplicating
// the payload the result already carries.
func TestP5216EchoBoundsValueLength(t *testing.T) {
	big := strings.Repeat("x", 500)
	got := toolResultEcho("write_file", json.RawMessage(`{"content":"`+big+`","path":"a.go"}`))
	if len(got) > 2*maxEchoValueLen {
		t.Errorf("echo not bounded: %d chars", len(got))
	}
	if !strings.Contains(got, "path=a.go") {
		t.Errorf("echo dropped the disambiguating argument: %q", got)
	}
	if !strings.Contains(got, "...") {
		t.Errorf("expected the long value to be truncated: %q", got)
	}
}

// TestP5216EchoSkipsNonScalarArgs verifies structured arguments are omitted
// rather than serialized — they add bulk without disambiguating.
func TestP5216EchoSkipsNonScalarArgs(t *testing.T) {
	got := toolResultEcho("apply", json.RawMessage(`{"edits":[{"a":1}],"file":"x.go"}`))
	if want := "[apply file=x.go]"; got != want {
		t.Errorf("echo = %q, want %q", got, want)
	}
}
