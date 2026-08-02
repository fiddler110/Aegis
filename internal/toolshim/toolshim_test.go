package toolshim

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

func names(list ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for _, n := range list {
		m[n] = struct{}{}
	}
	return m
}

func TestModeParsing(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		valid   bool
		enabled bool
	}{
		{"", true, false},
		{"off", true, false},
		{"on", true, true},
		{"  ON  ", true, true},
		{"Off", true, false},
		// "auto" is reserved for the follow-up that engages the shim off a
		// measured conformance rate. Accepting it today would ship a value that
		// silently does nothing.
		{"auto", false, false},
		{"true", false, false},
		{"yes", false, false},
	} {
		if got := ValidMode(tc.raw); got != tc.valid {
			t.Errorf("ValidMode(%q) = %v, want %v", tc.raw, got, tc.valid)
		}
		if got := Enabled(tc.raw); got != tc.enabled {
			t.Errorf("Enabled(%q) = %v, want %v", tc.raw, got, tc.enabled)
		}
	}
}

func TestPrompt(t *testing.T) {
	schemas := []provider.ToolSchema{
		{Name: "read_file", Description: "Read a file", InputSchema: json.RawMessage("{\n  \"type\": \"object\"\n}")},
		{Name: "shell", Description: "Run a command"},
	}
	got := Prompt(schemas)
	for _, want := range []string{openTag, closeTag, "read_file", "shell", "Read a file"} {
		if !strings.Contains(got, want) {
			t.Errorf("Prompt() missing %q:\n%s", want, got)
		}
	}
	// Input schemas are compacted so a pretty-printed one doesn't spend the
	// prompt budget on indentation.
	if !strings.Contains(got, `{"type":"object"}`) {
		t.Errorf("Prompt() did not compact the input schema:\n%s", got)
	}
	if Prompt(nil) != "" {
		t.Error("Prompt(nil) should be empty — a session with no tools is not told how to call one")
	}
}

func TestParseAccepts(t *testing.T) {
	n := names("read_file", "shell")
	tests := []struct {
		name      string
		text      string
		wantCalls []provider.ToolUseBlock
	}{
		{
			name: "plain",
			text: "I'll look.\n" + openTag + "\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}\n" + closeTag,
			wantCalls: []provider.ToolUseBlock{
				{ID: "t-0", Name: "read_file", Input: json.RawMessage(`{"path": "a.go"}`)},
			},
		},
		{
			name: "two calls in one reply",
			text: openTag + `{"name":"read_file","arguments":{"path":"a"}}` + closeTag +
				"\nand\n" + openTag + `{"name":"shell","arguments":{"cmd":"ls"}}` + closeTag,
			wantCalls: []provider.ToolUseBlock{
				{ID: "t-0", Name: "read_file", Input: json.RawMessage(`{"path":"a"}`)},
				{ID: "t-1", Name: "shell", Input: json.RawMessage(`{"cmd":"ls"}`)},
			},
		},
		{
			// The one tolerated deviation: chat-tuned models fence JSON by
			// reflex, and unwrapping a fence cannot change what the call says.
			name: "fenced json inside the tags",
			text: openTag + "\n```json\n{\"name\":\"shell\",\"arguments\":{\"cmd\":\"ls\"}}\n```\n" + closeTag,
			wantCalls: []provider.ToolUseBlock{
				{ID: "t-0", Name: "shell", Input: json.RawMessage(`{"cmd":"ls"}`)},
			},
		},
		{
			// OpenAI encodes arguments as a JSON *string*; unwrapped once, and
			// only when what's inside is an object.
			name: "arguments as a json string",
			text: openTag + `{"name":"shell","arguments":"{\"cmd\":\"ls\"}"}` + closeTag,
			wantCalls: []provider.ToolUseBlock{
				{ID: "t-0", Name: "shell", Input: json.RawMessage(`{"cmd":"ls"}`)},
			},
		},
		{
			name: "parameters alias",
			text: openTag + `{"name":"shell","parameters":{"cmd":"ls"}}` + closeTag,
			wantCalls: []provider.ToolUseBlock{
				{ID: "t-0", Name: "shell", Input: json.RawMessage(`{"cmd":"ls"}`)},
			},
		},
		{
			name: "no arguments means no arguments",
			text: openTag + `{"name":"shell"}` + closeTag,
			wantCalls: []provider.ToolUseBlock{
				{ID: "t-0", Name: "shell", Input: json.RawMessage(`{}`)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.text, n, "t")
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(got) != len(tc.wantCalls) {
				t.Fatalf("Parse() returned %d calls, want %d: %+v", len(got), len(tc.wantCalls), got)
			}
			for i, w := range tc.wantCalls {
				if got[i].ID != w.ID || got[i].Name != w.Name {
					t.Errorf("call %d = {%s %s}, want {%s %s}", i, got[i].ID, got[i].Name, w.ID, w.Name)
				}
				if !jsonEqual(got[i].Input, w.Input) {
					t.Errorf("call %d input = %s, want %s", i, got[i].Input, w.Input)
				}
			}
		})
	}
}

// TestParseNoAttempt covers the replies that must read as "this is a final
// answer", not as a failed call — a false positive here would inject a
// corrective into an ordinary answering turn.
func TestParseNoAttempt(t *testing.T) {
	n := names("read_file", "shell")
	for _, text := range []string{
		"Here is the answer.",
		// Bare JSON that happens to be call-shaped is NOT a call: without the
		// tags there is no way to tell it from a model quoting an example.
		`The payload looks like {"name": "shell", "arguments": {"cmd": "ls"}} in the docs.`,
		// Talking about the format must not derail a turn.
		"You would write a " + closeTag + " to close it.",
	} {
		got, err := Parse(text, n, "t")
		if !errors.Is(err, ErrNoCalls) {
			t.Errorf("Parse(%q) err = %v, want ErrNoCalls (calls: %+v)", text, err, got)
		}
	}
}

// TestParseDeclines is the core safety property: an attempt that doesn't meet
// the contract yields zero calls and a reason, never a repaired call. Every
// case here is one a lenient parser would have "fixed" into something
// executable.
func TestParseDeclines(t *testing.T) {
	n := names("read_file", "shell")
	tests := []struct {
		name string
		text string
		want string // substring of the reason
	}{
		{"unterminated tag", openTag + `{"name":"shell","arguments":{}}`, "never closed"},
		{"empty tag", openTag + "  " + closeTag, "empty"},
		{"malformed json", openTag + `{"name":"shell","arguments":{` + closeTag, "not valid JSON"},
		{"trailing prose in tag", openTag + `{"name":"shell","arguments":{}} then I'll check` + closeTag, "more than one JSON value"},
		{"two objects in one tag", openTag + `{"name":"shell","arguments":{}}{"name":"read_file","arguments":{}}` + closeTag, "more than one JSON value"},
		{"unknown tool", openTag + `{"name":"rm_rf","arguments":{}}` + closeTag, "not one of the available tools"},
		{"missing name", openTag + `{"arguments":{"cmd":"ls"}}` + closeTag, `no "name"`},
		{"arguments as array", openTag + `{"name":"shell","arguments":["ls"]}` + closeTag, "must be a JSON object"},
		{"arguments as bare string", openTag + `{"name":"shell","arguments":"ls"}` + closeTag, "does not contain a JSON object"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.text, n, "t")
			if err == nil {
				t.Fatalf("Parse() accepted a malformed attempt: %+v", got)
			}
			if errors.Is(err, ErrNoCalls) {
				t.Fatalf("Parse() reported no attempt, want a parse error: %v", err)
			}
			if got != nil {
				t.Errorf("Parse() returned %d calls alongside an error — nothing may run", len(got))
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse() error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestParseIsAllOrNothing pins the deliberate refusal to execute the good half
// of a bad turn: a model that got one call wrong has lost the shape, and the
// rest are no longer trustworthy inputs to a permission prompt.
func TestParseIsAllOrNothing(t *testing.T) {
	text := openTag + `{"name":"read_file","arguments":{"path":"a"}}` + closeTag +
		openTag + `{"name":"rm_rf","arguments":{}}` + closeTag
	got, err := Parse(text, names("read_file"), "t")
	if err == nil {
		t.Fatalf("Parse() accepted a turn with one bad call: %+v", got)
	}
	if len(got) != 0 {
		t.Errorf("Parse() returned %d calls from a turn that must yield none", len(got))
	}
}

func TestRenderResults(t *testing.T) {
	calls := []provider.ToolUseBlock{{Name: "read_file"}, {Name: "shell"}}
	results := []provider.Block{
		provider.ToolResultBlock{Content: "package main"},
		provider.ToolResultBlock{Content: "exit status 1", IsError: true},
	}
	got := RenderResults(calls, results)
	if !strings.Contains(got, `<tool_result tool="read_file">`) {
		t.Errorf("RenderResults() did not label the first result:\n%s", got)
	}
	if !strings.Contains(got, `<tool_result tool="shell" error="true">`) {
		t.Errorf("RenderResults() did not mark the failing result:\n%s", got)
	}
	if !strings.Contains(got, "package main") || !strings.Contains(got, "exit status 1") {
		t.Errorf("RenderResults() dropped content:\n%s", got)
	}
	if strings.Count(got, "</tool_result>") != 2 {
		t.Errorf("RenderResults() should close both blocks:\n%s", got)
	}
	if RenderResults(nil, nil) == "" {
		t.Error("RenderResults() must never render an empty user message")
	}
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
