// Package toolshim implements Aegis's opt-in non-native tool-calling fallback
// (P53.6): serialize the tool schemas into the system prompt, let the model
// write its calls as tagged JSON in its ordinary reply, and parse them back
// into structured tool calls.
//
// It exists because Aegis already *detects* the condition a fallback would
// handle — engine.go's P34.2 check spots a model writing a tool call into its
// prose — and then discards the signal with a notice. A model whose Ollama
// manifest claims tool support but whose weights cannot speak the protocol is
// the common case in the 14-27B class; goose (GOOSE_TOOLSHIM) and OpenHands
// (NonNativeToolCallingMixin) both recover such a model by moving the protocol
// into the prompt. This is the lighter of the two shapes: prompt + strict
// parser, no second interpreter model.
//
// Three rules shape it, and each is load-bearing:
//
//   - **Opt-in and explicit.** Nothing here engages on its own. A shim that
//     quietly starts turning prose into executable tool calls is a real safety
//     surface, so it is reached only through provider.tool_call_shim. Auto-
//     engagement on a low measured conformance rate (internal/modelcaps) is a
//     deliberate follow-up, not part of this package's contract.
//   - **No shortcut past the gate.** Parse produces ordinary
//     provider.ToolUseBlock values and nothing else. Every one of them travels
//     the same engine path a native tool call does — permission gate,
//     capability check, workspace confinement, hooks — because the engine
//     cannot tell them apart by the time they are dispatched, and this package
//     owns no execution path of its own.
//   - **Decline, never guess.** goose documents its own parse failures
//     (markdown where JSON was asked for, malformed JSON, inconsistent shapes)
//     and has an open request for JSON repair. A parser that repairs is a
//     parser that can fabricate a call the model did not make — with real
//     side effects behind it. So Parse is strict: one tagged form, one shape,
//     and any deviation fails the whole turn's calls with an instructive error
//     the caller can hand back to the model, rather than executing the subset
//     that happened to decode.
package toolshim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
)

// Modes accepted by provider.tool_call_shim. "" is treated as ModeOff, so an
// unset key leaves behavior exactly as it was.
const (
	ModeOff = "off"
	ModeOn  = "on"
)

// ValidMode reports whether raw is an accepted provider.tool_call_shim value.
// Note that "auto" is deliberately *not* accepted: engaging the shim off a
// measured conformance rate is only sound once that rate is trustworthy, and
// silently accepting the word today would ship an option that does nothing.
func ValidMode(raw string) bool {
	switch normalize(raw) {
	case "", ModeOff, ModeOn:
		return true
	}
	return false
}

// Enabled reports whether raw turns the shim on. Unknown values are off — the
// safe direction for a feature whose whole point is executing parsed prose.
func Enabled(raw string) bool { return normalize(raw) == ModeOn }

func normalize(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }

const (
	openTag  = "<tool_call>"
	closeTag = "</tool_call>"
)

// ErrNoCalls is returned by Parse when the text carries no tool-call attempt at
// all — the ordinary "this is a final answer" case, not a failure.
var ErrNoCalls = errors.New("toolshim: no tool call in reply")

// FormatReminder restates the contract for a model whose attempt failed to
// parse. It repeats Prompt's wording rather than paraphrasing it: a model that
// got the shape wrong once is not helped by a second, differently-worded
// description of the same shape. The caller supplies the specific reason and
// the framing around it.
const FormatReminder = "Emit a tool call as exactly this, with nothing between the tags but one JSON object:\n\n" +
	openTag + "\n" +
	`{"name": "<tool name>", "arguments": {<arguments as a JSON object>}}` + "\n" +
	closeTag + "\n\n" +
	"No markdown fences, no commentary inside the tags, one object per tag pair, and only tool names from the list you were given. " +
	"If you meant to answer rather than call a tool, reply with your answer and no tags at all."

// Prompt renders the tool contract and the tool catalog for the system prompt.
// Returns "" when there are no tools, so a session with nothing exposed is not
// told how to call nothing.
func Prompt(schemas []provider.ToolSchema) string {
	if len(schemas) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Tool calling\n\n")
	b.WriteString("This model server is not being given native tool definitions, so tools are called by writing them into your reply.\n\n")
	b.WriteString("To call a tool, emit exactly this, with nothing between the tags but one JSON object:\n\n")
	b.WriteString(openTag + "\n")
	b.WriteString(`{"name": "<tool name>", "arguments": {<arguments as a JSON object>}}` + "\n")
	b.WriteString(closeTag + "\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Use the tag pair literally. No markdown fences around it, no commentary inside it.\n")
	b.WriteString("- One JSON object per tag pair. To make several calls in one reply, emit several tag pairs.\n")
	b.WriteString("- \"name\" must be one of the tools listed below, spelled exactly. \"arguments\" is a JSON object; use {} when the tool takes none.\n")
	b.WriteString("- Stop after your tool calls and wait. The results come back in the next message. Never write, guess, or summarize a result you have not been given.\n")
	b.WriteString("- When you are answering rather than calling a tool, write the answer with no tags at all.\n\n")
	b.WriteString("## Available tools\n")
	for _, s := range schemas {
		b.WriteString("\n### " + s.Name + "\n")
		if d := strings.TrimSpace(s.Description); d != "" {
			b.WriteString(d + "\n")
		}
		if len(s.InputSchema) > 0 {
			b.WriteString("Arguments (JSON Schema): ")
			b.Write(compactJSON(s.InputSchema))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// compactJSON strips insignificant whitespace from raw so a pretty-printed
// input schema doesn't spend hundreds of tokens on indentation. Returns raw
// unchanged if it isn't valid JSON — the schema is the tool's own, so a
// malformed one is a bug worth showing rather than swallowing.
func compactJSON(raw json.RawMessage) []byte {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return raw
	}
	return out.Bytes()
}

// call is the one accepted on-the-wire shape. Arguments and Parameters are both
// read because models trained on either dialect reach for the name they know;
// the value stays raw because an OpenAI-shaped reply encodes it as a JSON
// *string* rather than an object, which decodeArguments unwraps.
type call struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
	Parameters json.RawMessage `json:"parameters"`
}

// Parse extracts tool calls from an assistant reply.
//
// It returns (nil, ErrNoCalls) when the reply contains no attempt — no tag, no
// call, an ordinary final answer. It returns a descriptive error when the reply
// *did* attempt a call that does not meet the contract; the caller is expected
// to hand that message back to the model via FormatReminder rather than execute
// anything. Partial success is not a result this can return: if any tag in the
// reply is malformed, none of the calls in it are returned, because a model that
// got one call wrong has demonstrably lost the shape and the remaining calls are
// no longer trustworthy inputs to a permission prompt.
//
// names is the set of tools actually exposed this turn. A call naming anything
// else is an error, not a silent drop: the model asked for something real that
// it cannot have, and telling it so is what gets the next turn right.
func Parse(text string, names map[string]struct{}, idPrefix string) ([]provider.ToolUseBlock, error) {
	if !strings.Contains(text, openTag) {
		return nil, ErrNoCalls
	}
	var out []provider.ToolUseBlock
	rest := text
	for i := 0; ; i++ {
		start := strings.Index(rest, openTag)
		if start < 0 {
			break
		}
		rest = rest[start+len(openTag):]
		end := strings.Index(rest, closeTag)
		if end < 0 {
			return nil, fmt.Errorf("a %s tag was never closed with %s", openTag, closeTag)
		}
		body := rest[:end]
		rest = rest[end+len(closeTag):]

		tu, err := parseBody(body, names, fmt.Sprintf("%s-%d", idPrefix, i))
		if err != nil {
			return nil, err
		}
		out = append(out, tu)
	}
	if len(out) == 0 {
		// The opening tag was present but produced nothing — a stray mention of
		// the tag in prose, not a call. Treated as no attempt rather than an
		// error so talking *about* the format can't derail a turn.
		return nil, ErrNoCalls
	}
	return out, nil
}

// parseBody decodes one tag pair's contents into a tool call.
func parseBody(body string, names map[string]struct{}, id string) (provider.ToolUseBlock, error) {
	trimmed := stripFence(strings.TrimSpace(body))
	if trimmed == "" {
		return provider.ToolUseBlock{}, errors.New("a tool-call tag pair was empty")
	}
	var c call
	dec := json.NewDecoder(strings.NewReader(trimmed))
	if err := dec.Decode(&c); err != nil {
		return provider.ToolUseBlock{}, fmt.Errorf("the tag contents are not valid JSON (%v)", err)
	}
	// Anything after the first complete value means the tag held more than one
	// object (or JSON plus prose). Rejected rather than taking the first: a
	// second object is a second call the model expects to happen.
	if rest := remainder(dec, trimmed); rest != "" {
		return provider.ToolUseBlock{}, fmt.Errorf("the tag contains more than one JSON value (trailing %q)", truncate(rest, 40))
	}
	if c.Name == "" {
		return provider.ToolUseBlock{}, errors.New(`the call has no "name"`)
	}
	if _, ok := names[c.Name]; !ok {
		return provider.ToolUseBlock{}, fmt.Errorf("%q is not one of the available tools", c.Name)
	}
	args, err := decodeArguments(c)
	if err != nil {
		return provider.ToolUseBlock{}, fmt.Errorf("%q: %w", c.Name, err)
	}
	return provider.ToolUseBlock{ID: id, Name: c.Name, Input: args}, nil
}

// decodeArguments normalizes the arguments value to a JSON object. Absent
// arguments mean a no-argument call ({}); an OpenAI-shaped JSON *string* is
// unwrapped once and must itself hold an object. Every other shape — an array,
// a number, a string that isn't JSON — is refused, because the tools' own
// schemas all take objects and coercing anything else would be inventing input.
func decodeArguments(c call) (json.RawMessage, error) {
	raw := c.Arguments
	if len(raw) == 0 {
		raw = c.Parameters
	}
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if isJSONObject(raw) {
		return raw, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		inner := json.RawMessage(strings.TrimSpace(s))
		if len(inner) == 0 {
			return json.RawMessage(`{}`), nil
		}
		if isJSONObject(inner) {
			return inner, nil
		}
		return nil, errors.New(`"arguments" was a string that does not contain a JSON object`)
	}
	return nil, errors.New(`"arguments" must be a JSON object`)
}

// isJSONObject reports whether raw is a syntactically valid JSON object.
func isJSONObject(raw json.RawMessage) bool {
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw))[0] != '{' {
		return false
	}
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil
}

// remainder returns whatever non-whitespace text followed the first decoded
// JSON value in src.
func remainder(dec *json.Decoder, src string) string {
	off := dec.InputOffset()
	if off < 0 || int(off) >= len(src) {
		return ""
	}
	return strings.TrimSpace(src[off:])
}

// stripFence removes a single surrounding markdown code fence, the one
// deviation tolerated: fencing JSON is so ingrained in chat-tuned models that
// refusing it would fail turns whose call is otherwise exactly right, and
// unwrapping a fence cannot change what the call *says* the way repairing
// malformed JSON could.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else {
		return s
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RenderResults formats a tool round's results as the text of one user message.
//
// It exists because a shimmed session has no native tool_use blocks in its
// transcript, so the matching provider.ToolResultBlock values would be orphaned
// and rejected by the provider. Rendering them as plain text keeps the
// transcript well-formed for exactly the models this shim serves — the ones
// that never spoke the structured protocol in the first place.
//
// calls is the round's tool calls, positionally aligned with results (which is
// how the engine builds them), and is used only to label each result with the
// tool that produced it — without a tool_use_id to correlate on, a round of
// three calls would otherwise come back as three anonymous blobs.
func RenderResults(calls []provider.ToolUseBlock, results []provider.Block) string {
	var b strings.Builder
	for i, blk := range results {
		r, ok := blk.(provider.ToolResultBlock)
		if !ok {
			continue
		}
		name := ""
		if i < len(calls) {
			name = calls[i].Name
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("<tool_result tool=" + strconv.Quote(name))
		if r.IsError {
			b.WriteString(` error="true"`)
		}
		b.WriteString(">\n")
		b.WriteString(r.Content)
		if !strings.HasSuffix(r.Content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("</tool_result>\n")
	}
	if b.Len() == 0 {
		return "<tool_result>\n(no output)\n</tool_result>\n"
	}
	return b.String()
}
