package share

import (
	"encoding/json"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/redact"
	"github.com/fiddler110/aegis/internal/session"
)

// P66.11 (SEC-08): before this, an exported transcript carried whatever the
// session held. That is the whole session — every tool result, every shell
// command's output, every file the run read — and the export is the one artifact
// in this system explicitly built to leave the machine. A `cat .env`, an `env`
// dump, a `git remote -v` with a token in the URL, or a credential the model
// happened to echo back all travelled verbatim into a file the user then hands to
// someone else.
//
// # Redact the session, not the three renderers
//
// The pass runs over the *session* ahead of rendering rather than inside each
// renderer, which is the difference between one filter and three. HTML, Markdown
// and JSON read the same fields; a renderer-level pass would have to be repeated
// per format and per block type, and the JSON export — which marshals the session
// object directly and is the format most likely to be fed to another program —
// would be the easiest of the three to forget.
//
// # What is filtered
//
// Every string a transcript displays: message text, thinking text, tool-call
// inputs, tool results, the system prompt and the title. Not image bytes (base64
// is not a shape these patterns can read, and a pattern that scanned it would
// spend its time on megabytes of noise), and not identifiers — ids, tool names,
// model names, token counts — where a match would be a false positive with no
// upside.
//
// # The count is part of the artifact
//
// Render reports how many redactions it made and both renderers state it in the
// exported document, because a redaction pass that silently finds nothing is
// indistinguishable from one that was never wired up — which is precisely the
// state this package was in. Zero is a real answer and is stated as one.
//
// # What this is not
//
// It is a filter over a small, low-false-positive pattern set (internal/redact),
// not detection. A secret it does not recognize still leaves in the export, so
// the honest claim is "the shapes we know are removed and the count is stated",
// never "this transcript is clean".

// redactSession returns a copy of sess with credential-shaped substrings replaced
// and the number of replacements made. The input is never mutated: the caller's
// session is usually the live object from the store, and an export must not
// rewrite the conversation it is exporting.
//
// The copy is deep exactly as far as it needs to be — the message slice, each
// message's block slice, and the blocks themselves, which are values — and shares
// everything a redaction cannot touch.
func redactSession(sess *session.Session) (*session.Session, int) {
	if sess == nil {
		return nil, 0
	}
	total := 0
	str := func(s string) string {
		out, n := redact.Text(s)
		total += n
		return out
	}

	out := *sess
	out.Title = str(sess.Title)
	out.System = str(sess.System)
	out.Messages = make([]provider.Message, len(sess.Messages))
	for i, m := range sess.Messages {
		msg := m
		msg.Content = make([]provider.Block, len(m.Content))
		for j, blk := range m.Content {
			switch v := blk.(type) {
			case provider.TextBlock:
				v.Text = str(v.Text)
				msg.Content[j] = v
			case provider.ThinkingBlock:
				// The reasoning channel is transcript like any other: a model
				// that read a credential can restate it while thinking about it.
				v.Text = str(v.Text)
				msg.Content[j] = v
			case provider.ToolUseBlock:
				// Inputs are the model's own construction and are where an
				// exfiltration attempt would put a credential — the same reason
				// internal/mcp scans them on the way out.
				v.Input = redactRawJSON(str, v.Input)
				msg.Content[j] = v
			case provider.ToolResultBlock:
				v.Content = str(v.Content)
				msg.Content[j] = v
			default:
				// ImageBlock and anything added later pass through untouched.
				msg.Content[j] = blk
			}
		}
		out.Messages[i] = msg
	}
	return &out, total
}

// redactRawJSON redacts a tool call's arguments while keeping the field a valid
// JSON value, which the JSON export depends on: json.RawMessage is marshalled
// verbatim, so invalid bytes here do not produce a slightly-wrong artifact — they
// fail the whole export with "json: error calling MarshalJSON".
//
// Substituting inside a string literal is the ordinary case and stays structural.
// One pattern can reach past a literal's edge: the assignment shape matches
// `api_key":"0123456789abcdef` across the quote and colon of an object like
// {"api_key":"…"}, and replacing that span takes the delimiters with it. Rather
// than narrow a pattern that exists to catch exactly that shape, the broken result
// is re-encoded as a JSON *string* — the arguments then render as one redacted
// line instead of an object, which is a visible degradation of a call whose
// arguments contained a credential, and the honest trade against either shipping
// invalid JSON or leaving the secret in.
func redactRawJSON(str func(string) string, in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return in
	}
	red := str(string(in))
	if red == string(in) {
		return in // nothing matched; keep the caller's bytes exactly
	}
	if json.Valid([]byte(red)) {
		return json.RawMessage(red)
	}
	if b, err := json.Marshal(red); err == nil {
		return json.RawMessage(b)
	}
	// Last resort: a valid JSON string that says nothing survived. Reached only
	// if marshalling a plain string fails, which it cannot for valid UTF-8.
	return json.RawMessage(`"[redacted]"`)
}
