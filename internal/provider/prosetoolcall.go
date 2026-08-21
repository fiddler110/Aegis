package provider

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// proseToolCallAdapter salvages a tool call a local model emitted as text
// instead of a structured tool_calls entry (P74.8). Small local models
// frequently answer a tool-enabled turn with a fenced JSON object, a
// <tool_call> tag, or a bare JSON object narrated in prose, none of which the
// engine's tool dispatch sees — the turn reads as a plain answer and either
// stalls waiting for a call that already "happened" in text, or the engine
// retries blind against a model that already answered.
//
// It only ever acts when the base adapter's stream produced *no* structured
// tool_use event at all — a turn that made even one real call is left
// completely alone, salvage or not.
type proseToolCallAdapter struct {
	base Adapter
}

// WithProseToolCallSalvage wraps base so a turn that requested tools but
// received a text-only reply is scanned for a tool call written as prose
// before being handed to the caller. Returns base unchanged when base is nil.
//
// This is a response-side decorator, the mirror of the request-side ones
// beside it (numctx.go, retry.go): it needs the whole assembled reply before
// it can tell a genuine text answer from a mis-emitted call, so — unlike
// every other decorator in this package — it does not forward events as they
// arrive. It buffers one turn's stream, decides, and then either replays it
// unchanged or replaces the buffered text with the parsed call plus whatever
// prose survives it. That trades live token-by-token display for correctness
// on the turns it actually rewrites, which is acceptable here because P74.17
// is expected to gate this per model rather than leaving it a blanket default.
func WithProseToolCallSalvage(base Adapter) Adapter {
	if base == nil {
		return base
	}
	return &proseToolCallAdapter{base: base}
}

func (a *proseToolCallAdapter) Name() string    { return a.base.Name() }
func (a *proseToolCallAdapter) Unwrap() Adapter { return a.base }

func (a *proseToolCallAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	in, err := a.base.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 4)
	go a.run(req, in, out)
	return out, nil
}

func (a *proseToolCallAdapter) run(req Request, in <-chan Event, out chan<- Event) {
	defer close(out)

	// Nothing to salvage against if the turn offered the model no tools at
	// all — a text-only request is not a candidate, whatever it contains.
	if len(req.Tools) == 0 {
		passthrough(in, out)
		return
	}

	var buffered []Event
	var text strings.Builder
	sawToolUse := false
	doneIdx := -1
	for ev := range in {
		buffered = append(buffered, ev)
		switch ev.Type {
		case EventTextDelta:
			text.WriteString(ev.Text)
		case EventToolUseStart, EventToolUse:
			sawToolUse = true
		case EventDone, EventError:
			doneIdx = len(buffered) - 1
		}
	}

	if sawToolUse || doneIdx == -1 || buffered[doneIdx].Type == EventError {
		replay(buffered, out)
		return
	}

	call, remaining, ok := salvageToolCall(text.String(), req.Tools)
	if !ok {
		replay(buffered, out)
		return
	}

	// Everything that isn't the assistant's plain text content (thinking,
	// notably) replays untouched; the text content itself is replaced by
	// whatever prose survives the parsed-out call, and a synthesized pair of
	// tool-use events carries the call the model actually meant to make.
	for _, ev := range buffered {
		if ev.Type == EventTextDelta || ev.Type == EventDone {
			continue
		}
		out <- ev
	}
	if strings.TrimSpace(remaining) != "" {
		out <- Event{Type: EventTextDelta, Text: remaining}
	}
	out <- Event{Type: EventToolUseStart, ToolUse: &ToolUseBlock{ID: call.ID, Name: call.Name}}
	out <- Event{Type: EventToolUse, ToolUse: call}

	doneEv := buffered[doneIdx]
	doneEv.Stop = StopToolUse
	out <- doneEv
}

func passthrough(in <-chan Event, out chan<- Event) {
	for ev := range in {
		out <- ev
	}
}

func replay(buffered []Event, out chan<- Event) {
	for _, ev := range buffered {
		out <- ev
	}
}

// toolCallFence matches a fenced code block, optionally tagged "json", whose
// body is meant to be a single JSON tool-call object.
var toolCallFence = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*?\\})\\s*\\n?```")

// toolCallTag matches a <tool_call>...</tool_call> or <function_call>...
// </function_call> block, the shape local models trained on Hermes/Qwen-style
// tool-calling prompts tend to emit.
var toolCallTag = regexp.MustCompile(`(?is)<(tool_call|function_call)>(.*?)</(?:tool_call|function_call)>`)

// salvageToolCall looks for a single tool call written as text in reply,
// preferring the most explicit signal (a tagged block), then a fenced JSON
// block, then a bare JSON object anywhere in the text. It returns the parsed
// call, the surviving text with the matched span removed, and whether a call
// was found at all.
//
// A candidate is only accepted when its name matches one of tools exactly —
// the request actually sent, not the tool registry at large — so a reply that
// merely mentions a tool by name in a sentence never becomes a call.
func salvageToolCall(reply string, tools []ToolSchema) (*ToolUseBlock, string, bool) {
	if strings.TrimSpace(reply) == "" {
		return nil, reply, false
	}
	names := make(map[string]bool, len(tools))
	for _, t := range tools {
		names[t.Name] = true
	}

	if loc := toolCallTag.FindStringSubmatchIndex(reply); loc != nil {
		body := reply[loc[4]:loc[5]]
		if call, ok := parseCallObject(body, names); ok {
			remaining := reply[:loc[0]] + reply[loc[1]:]
			return call, remaining, true
		}
	}

	if loc := toolCallFence.FindStringSubmatchIndex(reply); loc != nil {
		body := reply[loc[2]:loc[3]]
		if call, ok := parseCallObject(body, names); ok {
			remaining := reply[:loc[0]] + reply[loc[1]:]
			return call, remaining, true
		}
	}

	if call, span, ok := scanBareCallObject(reply, names); ok {
		remaining := reply[:span[0]] + reply[span[1]:]
		return call, remaining, true
	}

	return nil, reply, false
}

// scanBareCallObject walks every '{' in reply looking for the first balanced
// JSON object that parses as a call naming one of names, returning its byte
// span so the caller can strip exactly that substring.
func scanBareCallObject(reply string, names map[string]bool) (*ToolUseBlock, [2]int, bool) {
	for i := 0; i < len(reply); i++ {
		if reply[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(reply[i:]))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			continue
		}
		if call, ok := parseCallObject(string(raw), names); ok {
			return call, [2]int{i, i + int(dec.InputOffset())}, true
		}
	}
	return nil, [2]int{}, false
}

// callSalvageID is the synthesized tool-call ID for a salvaged call — there is
// never more than one per reply, so a fixed ID (matching the "tu_" + index
// pattern the OpenAI adapter's own synthesis uses) is enough to correlate the
// EventToolUseStart and EventToolUse this decorator emits for it.
const callSalvageID = "tu_salvaged"

// parseCallObject decodes body as {"name"|"tool"|"function": ..., "arguments"
// |"parameters"|"input": ...}, accepting either an object or a JSON-string-
// encoded object for the argument field (models emit both), and requires the
// resolved name to be one of names.
func parseCallObject(body string, names map[string]bool) (*ToolUseBlock, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		return nil, false
	}

	name := firstString(fields, "name", "tool", "function")
	if name == "" || !names[name] {
		return nil, false
	}

	args := firstRaw(fields, "arguments", "parameters", "input")
	args = normalizeArguments(args)

	return &ToolUseBlock{ID: callSalvageID, Name: name, Input: args}, true
}

func firstString(fields map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		raw, ok := fields[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" {
			return s
		}
	}
	return ""
}

func firstRaw(fields map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, k := range keys {
		if raw, ok := fields[k]; ok {
			return raw
		}
	}
	return nil
}

// normalizeArguments accepts either a JSON object or a JSON-string containing
// one (both appear in the wild for local-model prose calls) and returns a
// well-formed object, defaulting to "{}" when the field was absent or
// unparsable rather than sending a tool a nil Input.
func normalizeArguments(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage("{}")
	}
	if strings.HasPrefix(trimmed, "\"") {
		var inner string
		if err := json.Unmarshal(raw, &inner); err == nil {
			inner = strings.TrimSpace(inner)
			if json.Valid([]byte(inner)) {
				return json.RawMessage(inner)
			}
		}
		return json.RawMessage("{}")
	}
	if json.Valid(raw) {
		return raw
	}
	return json.RawMessage("{}")
}
