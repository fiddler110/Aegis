package share

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
)

const (
	leakedToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	leakedKey   = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"
)

// leakySession is a transcript with a credential in every place one can realistically
// end up: a user message that pasted it, the model's reasoning, a tool call's
// arguments, a tool result that read a file, and the system prompt.
func leakySession() *session.Session {
	return &session.Session{
		ID:     "leaky",
		Title:  "debug the " + leakedToken + " problem",
		System: "You are an agent. The deploy key is " + leakedKey,
		Mode:   "build",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{
				provider.TextBlock{Text: "my token is " + leakedToken + ", why is auth failing?"},
			}},
			{Role: provider.RoleAssistant, Content: []provider.Block{
				provider.ThinkingBlock{Text: "the user pasted " + leakedToken + " which looks valid"},
				provider.TextBlock{Text: "Let me check the config."},
				provider.ToolUseBlock{ID: "t1", Name: "shell", Input: json.RawMessage(
					`{"command":"curl -H 'Authorization: Bearer ` + leakedToken + `' https://api.example.com"}`)},
			}},
			{Role: provider.RoleUser, Content: []provider.Block{
				provider.ToolResultBlock{ToolUseID: "t1", Content: "GITHUB_TOKEN=" + leakedToken + "\nOK"},
			}},
		},
	}
}

// TestEveryFormatRedacts is SEC-08: the export is the one artifact in this system
// built to leave the machine, and before P66.11 it carried the transcript verbatim.
// All three formats are asserted, because the pass runs over the session rather
// than inside a renderer precisely so none of them can be the one that forgot.
func TestEveryFormatRedacts(t *testing.T) {
	for _, f := range []Format{FormatHTML, FormatMarkdown, FormatJSON} {
		t.Run(string(f), func(t *testing.T) {
			data, n, err := Render(leakySession(), f)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			out := string(data)
			for _, secret := range []string{leakedToken, leakedKey} {
				if strings.Contains(out, secret) {
					t.Errorf("%s export contains the credential verbatim", f)
				}
			}
			if n == 0 {
				t.Errorf("%s export reported 0 redactions for a transcript full of them", f)
			}
			if !strings.Contains(out, "[redacted:") {
				t.Errorf("%s export shows no placeholder, so the content was dropped rather than marked", f)
			}
		})
	}
}

// TestRedactionCoversEveryBlockKind: each block type is a separate case in the
// walk, so a missed one is a silent hole. Asserted per site rather than over the
// whole document, so a failure names which one leaked.
func TestRedactionCoversEveryBlockKind(t *testing.T) {
	red, n := redactSession(leakySession())
	if n == 0 {
		t.Fatal("nothing was redacted")
	}
	if strings.Contains(red.Title, leakedToken) {
		t.Error("title leaked")
	}
	if strings.Contains(red.System, leakedKey) {
		t.Error("system prompt leaked")
	}
	for i, m := range red.Messages {
		for j, blk := range m.Content {
			var text string
			switch v := blk.(type) {
			case provider.TextBlock:
				text = v.Text
			case provider.ThinkingBlock:
				text = v.Text
			case provider.ToolUseBlock:
				text = string(v.Input)
			case provider.ToolResultBlock:
				text = v.Content
			}
			if strings.Contains(text, leakedToken) || strings.Contains(text, leakedKey) {
				t.Errorf("message %d block %d (%T) leaked: %s", i, j, blk, text)
			}
		}
	}
}

// TestRedactionDoesNotMutateTheStoredSession: the session handed to Render is the
// live object from the store, and an export must not rewrite the conversation it
// is exporting. This is the assertion that a shallow copy would fail.
func TestRedactionDoesNotMutateTheStoredSession(t *testing.T) {
	sess := leakySession()
	if _, _, err := Render(sess, FormatJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sess.Title, leakedToken) {
		t.Error("Render rewrote the caller's title")
	}
	tr, ok := sess.Messages[2].Content[0].(provider.ToolResultBlock)
	if !ok {
		t.Fatalf("fixture changed: %T", sess.Messages[2].Content[0])
	}
	if !strings.Contains(tr.Content, leakedToken) {
		t.Error("Render rewrote the caller's tool result in place")
	}
}

// TestJSONExportStaysParseableAndReportsTheCount: the JSON format is the one most
// likely to be fed to another program, so it must still unmarshal into a Session —
// the redaction count is an additive key, not a wrapper — and it must carry the
// count, since a machine reader has no header to read.
func TestJSONExportStaysParseableAndReportsTheCount(t *testing.T) {
	data, n, err := Render(leakySession(), FormatJSON)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var round session.Session
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("the JSON export no longer unmarshals into a Session: %v", err)
	}
	if round.ID != "leaky" {
		t.Errorf("session id = %q after round-tripping, want the fields still inline", round.ID)
	}
	var envelope struct {
		Redactions int `json:"redactions"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Redactions != n {
		t.Errorf("JSON reports %d redactions, Render returned %d", envelope.Redactions, n)
	}
}

// TestCleanTranscriptReportsZeroExplicitly: zero is a real answer and has to be
// stated, because "nothing matched" and "the filter was never wired up" were
// byte-for-byte identical before this — which is how a package with no redaction
// at all went unnoticed.
func TestCleanTranscriptReportsZeroExplicitly(t *testing.T) {
	sess := &session.Session{ID: "clean", Mode: "build", Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}},
	}}
	for _, f := range []Format{FormatHTML, FormatMarkdown} {
		data, n, err := Render(sess, f)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: redacted %d times on a clean transcript", f, n)
		}
		if !strings.Contains(string(data), "No credential-shaped content matched") {
			t.Errorf("%s export does not state that the filter ran and found nothing", f)
		}
	}
}

// TestRedactedToolInputStaysValidJSON: a redaction can span the quote and colon of
// `"api_key":"value"`, which would leave the field invalid and fail the whole JSON
// export rather than one block. The fallback re-encodes it as a string.
func TestRedactedToolInputStaysValidJSON(t *testing.T) {
	sess := &session.Session{ID: "x", Mode: "build", Messages: []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "t1", Name: "shell", Input: json.RawMessage(`{"api_key":"0123456789abcdefghij"}`)},
		}},
	}}
	data, n, err := Render(sess, FormatJSON)
	if err != nil {
		t.Fatalf("the JSON export failed on a redacted tool input: %v", err)
	}
	if n == 0 {
		t.Fatal("nothing was redacted")
	}
	if strings.Contains(string(data), "0123456789abcdefghij") {
		t.Error("the secret survived in the JSON export")
	}
	var round session.Session
	if err := json.Unmarshal(data, &round); err != nil {
		t.Errorf("export does not parse: %v", err)
	}
}
