package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/fiddler110/aegis/internal/termsafe"
)

// The sanitizers themselves live in internal/termsafe: the plain CLI renderer
// (internal/cli/chat_render.go) writes the same two classes of text — model
// prose and raw tool output — to the same kind of terminal, and a second copy
// of this logic is the shape of bug this codebase has already paid for once
// (the duplicated OCI hardening-flag list, P55.7). These thin aliases keep the
// TUI's call sites unchanged; the behavior tests moved with the code, to
// internal/termsafe/termsafe_test.go.

func stripControlSeqs(s string) string { return termsafe.StripControlSeqs(s) }

func stripDangerousSeqs(s string) string { return termsafe.StripDangerousSeqs(s) }

// sanitizeToolInputJSON strips terminal control sequences out of a pending
// tool call's argument JSON before the approval dialog ever sees it (P66.6,
// SEC-14). The approval prompt is the last line of defence a human answers, so
// a call whose arguments carry ESC[2J ESC[H — or an OSC title/clipboard
// sequence — must not be able to blank or redraw the very question being
// asked. Sanitizing here rather than inside each preview renderer is
// deliberate: approval.go dispatches four tools to renderToolCall and
// everything else to renderApprovalPreview, and the write/edit previews reach
// file *content* through diffLines, so a per-renderer fix leaves the next new
// branch to reintroduce the hole.
//
// StripControlSeqs (not StripDangerousSeqs) is the right policy for this path:
// the dialog styles the diff, the JSON excerpt and its own chrome with lipgloss
// *after* ingestion, so model-supplied SGR carries no legitimate meaning here —
// it can only fight the TUI's own colours.
//
// Two passes, because there are two ways an escape reaches the terminal:
//   - raw ESC bytes sitting in the wire text (which encoding/json would reject
//     as an invalid string literal, dropping the preview into the generic
//     "compact JSON excerpt" branch that prints the bytes verbatim); and
//   - the six-character JSON escape for ESC (backslash-u-0-0-1-b), which is
//     plain ASCII on the wire and only becomes a control byte when a renderer
//     unmarshals the arguments. This is the form a real provider actually
//     delivers, and the raw-byte pass alone would sail straight past it.
//
// Re-encoding normalizes key order and whitespace. Nothing downstream depends
// on either: the previews unmarshal named fields, suggestRulePattern does too,
// and the generic fallback already collapses whitespace itself. Input that
// doesn't parse as JSON keeps the raw-byte pass and nothing else.
func sanitizeToolInputJSON(raw string) string {
	s := stripControlSeqs(raw)
	// JSON only spells non-printables as \u00XX (lowercase u, per RFC 8259),
	// and every control codepoint — C0, DEL and the C1 range — lives under
	// that prefix.
	if !strings.Contains(s, `\u00`) {
		// No JSON-escaped control codepoint anywhere in the text, so the
		// raw-byte pass already covered it — skip the decode/re-encode round
		// trip and leave the arguments byte-identical.
		return s
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber() // keep integers/large floats as written rather than reformatting them
	var v any
	if dec.Decode(&v) != nil {
		return s
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // leave <, > and & alone; this preview is read by a human, not a browser
	if enc.Encode(sanitizeJSONValue(v)) != nil {
		return s
	}
	return strings.TrimRight(buf.String(), "\n")
}

// sanitizeJSONValue walks a decoded JSON value and runs stripControlSeqs over
// every string it contains, object keys included — a key is rendered by the
// generic JSON-excerpt preview just like a value is.
func sanitizeJSONValue(v any) any {
	switch t := v.(type) {
	case string:
		return stripControlSeqs(t)
	case []any:
		for i, e := range t {
			t[i] = sanitizeJSONValue(e)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[stripControlSeqs(k)] = sanitizeJSONValue(e)
		}
		return out
	default:
		return v
	}
}
