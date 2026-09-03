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
// beside it (numctx.go, retry.go): it needs the whole assembled *text* before
// it can tell a genuine answer from a mis-emitted call, so it holds text
// deltas and the terminal EventDone until it has decided, then either replays
// them unchanged or replaces the text with the parsed call plus whatever prose
// survives it.
//
// It holds nothing else. Thinking and every other event go out as they arrive,
// and the moment the turn emits a real structured call the decorator has
// already decided to keep out of the way, so it flushes and becomes a plain
// passthrough for the rest of the turn. That matters because two engine
// invariants are watching this channel: the stall heartbeat beats on each
// event received, and P67.7 dispatches a tool call the instant it is announced.
// Buffering the whole stream — which this used to do — silently disabled both
// for exactly the local-model population the decorator exists for (CRIT-4).
//
// The cost that remains is the intended one: for a text-only turn the answer
// is delivered when generation ends rather than token by token, which is what
// buys the ability to rewrite it. P74.17 is expected to gate this per model
// rather than leaving it a blanket default.
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

// maxSalvageTextBytes bounds the text one turn may accumulate before the
// decorator gives up on salvaging it and becomes a passthrough. A tool call
// written as prose is a few hundred bytes; a reply past this cap is a long
// answer, not a mis-emitted call, and holding it serves nothing (CRIT-4d).
const maxSalvageTextBytes = 1 << 20 // 1 MiB

func (a *proseToolCallAdapter) run(req Request, in <-chan Event, out chan<- Event) {
	defer close(out)

	// Nothing to salvage against if the turn offered the model no tools at
	// all — a text-only request is not a candidate, whatever it contains.
	if len(req.Tools) == 0 {
		passthrough(in, out)
		return
	}

	// CRIT-4: this used to run the whole loop to channel close before
	// forwarding a single event. Two invariants depended on that not happening
	// and nothing reconciled them: the engine's only in-turn liveness signal is
	// beat(ctx) inside `for ev := range stream`, so a turn under salvage looked
	// completely idle to stallWatch and a legitimate long local generation was
	// killed at MaxTurnStall as a run-fatal ErrTurnStalled; and P67.7's early
	// tool dispatch, whose own comment says it pays precisely on local models,
	// was inert because every EventToolUse arrived after generation finished.
	//
	// The decorator never needed to withhold *events* to decide — it needs to
	// withhold *text*, which is the only thing it might rewrite. So everything
	// else goes out the instant it arrives, and the buffer holds text deltas
	// and the terminal EventDone (whose Stop reason the rewrite branch changes)
	// and nothing more.
	var textEvents []Event
	var text strings.Builder
	var doneEv Event
	haveDone := false
	// flushed latches "the decision is already made, forward everything" — set
	// the moment the turn emits a real structured call (nothing left to
	// salvage), or the moment the text outgrows the cap.
	flushed := false
	flush := func() {
		if flushed {
			return
		}
		flushed = true
		replay(textEvents, out)
		textEvents = nil
	}

	for ev := range in {
		if flushed {
			out <- ev
			continue
		}
		switch ev.Type {
		case EventTextDelta:
			textEvents = append(textEvents, ev)
			text.WriteString(ev.Text)
			if text.Len() > maxSalvageTextBytes {
				flush()
			}
		case EventToolUseStart, EventToolUse:
			// The turn made a real call, so this decorator is done deciding:
			// a turn that made even one structured call is left completely
			// alone. Flush and get out of the way, which is what restores
			// P67.7 for every model good enough to emit structured calls.
			flush()
			out <- ev
		case EventDone:
			doneEv, haveDone = ev, true
		case EventError:
			// A failed turn is replayed as it happened; there is no reply to
			// salvage from.
			flush()
			out <- ev
		default:
			// Thinking and anything added later: liveness the engine beats on,
			// forwarded untouched and in order.
			out <- ev
		}
	}

	if flushed {
		if haveDone {
			out <- doneEv
		}
		return
	}
	if !haveDone {
		// The stream ended without a terminal event (a cancelled context, a
		// transport that closed early). Nothing to rewrite onto.
		replay(textEvents, out)
		return
	}

	call, remaining, ok := salvageToolCall(text.String(), req.Tools)
	if !ok {
		replay(textEvents, out)
		out <- doneEv
		return
	}

	// The text content is replaced by whatever prose survives the parsed-out
	// call, and a synthesized pair of tool-use events carries the call the
	// model actually meant to make. Everything that isn't the assistant's
	// plain text content has already been forwarded above, untouched.
	if strings.TrimSpace(remaining) != "" {
		out <- Event{Type: EventTextDelta, Text: remaining}
	}
	out <- Event{Type: EventToolUseStart, ToolUse: &ToolUseBlock{ID: call.ID, Name: call.Name}}
	out <- Event{Type: EventToolUse, ToolUse: call}

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
		if call, ok := parseCallBody(body, names); ok {
			remaining := reply[:loc[0]] + reply[loc[1]:]
			return call, remaining, true
		}
	}

	// EXEC-3: the XML form is also emitted *without* the <tool_call> wrapper,
	// so it gets a pass of its own rather than riding only on the tag above.
	if loc := functionCallXML.FindStringSubmatchIndex(reply); loc != nil {
		if call, ok := parseFunctionXML(reply[loc[0]:loc[1]], names); ok {
			remaining := reply[:loc[0]] + reply[loc[1]:]
			return call, remaining, true
		}
	}

	if loc := toolCallFence.FindStringSubmatchIndex(reply); loc != nil {
		body := reply[loc[2]:loc[3]]
		if call, ok := parseCallBody(body, names); ok {
			remaining := reply[:loc[0]] + reply[loc[1]:]
			return call, remaining, true
		}
	}

	// The bare-object branch is deliberately the narrowest of the three: it
	// only fires when the *entire* reply is the object.
	//
	// It used to accept an object anywhere in the prose, which made salvage an
	// injection amplifier (M2). A model that read a poisoned file and echoed
	// its contents back — quoting it, describing it, refusing it — handed this
	// branch a JSON object it never chose to call, and the object became a real
	// tool call. The gate still applies, but CapWrite and CapNetwork are
	// allowed silently in build mode, so the gate is not what stops it. A model
	// that genuinely means to call a tool and cannot emit a structured call
	// still has the two explicit spellings above; narrating a call inside prose
	// and having that narration executed is the case worth losing.
	if trimmed := strings.TrimSpace(reply); strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if call, ok := parseCallObject(trimmed, names); ok {
			return call, "", true
		}
	}

	return nil, reply, false
}

// functionCallXML matches Qwen3's own documented tool-call body — a non-JSON
// XML form its chat template instructs it to emit:
//
//	<function=shell>
//	<parameter=command>
//	ls -la
//	</parameter>
//	</function>
//
// EXEC-3: the <tool_call> wrapper matched, but its body was handed straight to
// parseCallObject, which does json.Unmarshal — and this body is XML. It failed,
// fell through to the fenced and bare branches, and they found nothing. The
// most common local tool-call syntax outside JSON was the one shape this
// salvage net could not catch. Ollama's Jinja renderer normally parses it back
// into structured tool_calls before Aegis sees it, which is why the probe still
// passed; salvage matters exactly when that parse does *not* happen — a
// truncated call, a malformed parameter block, a template change — which is the
// circumstance this decorator exists for.
var functionCallXML = regexp.MustCompile(`(?is)<function\s*=\s*([A-Za-z0-9_.-]+)\s*>(.*?)</function\s*>`)

// xmlParameter matches one <parameter=key>value</parameter> inside a
// functionCallXML body.
var xmlParameter = regexp.MustCompile(`(?is)<parameter\s*=\s*([A-Za-z0-9_.-]+)\s*>(.*?)</parameter\s*>`)

// parseCallBody decodes a tool-call body in whichever of the two spellings a
// model used: the JSON object, or the XML function form above.
func parseCallBody(body string, names map[string]bool) (*ToolUseBlock, bool) {
	if call, ok := parseCallObject(body, names); ok {
		return call, true
	}
	return parseFunctionXML(body, names)
}

// parseFunctionXML decodes the <function=NAME><parameter=KEY>value</parameter>
// form, requiring the resolved name to be one of names exactly, as every other
// branch does.
//
// Parameter values are carried as JSON strings. The XML form has no types, so
// the string is what the model wrote, trimmed of the newlines the template puts
// around it; a schema-aware coercion belongs in the argument-shape repair
// decorator, which already owns that question, not here.
func parseFunctionXML(body string, names map[string]bool) (*ToolUseBlock, bool) {
	loc := functionCallXML.FindStringSubmatch(body)
	if loc == nil {
		return nil, false
	}
	name := loc[1]
	if !names[name] {
		return nil, false
	}
	args := map[string]string{}
	for _, m := range xmlParameter.FindAllStringSubmatch(loc[2], -1) {
		args[m[1]] = strings.Trim(m[2], "\r\n")
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return &ToolUseBlock{ID: callSalvageID, Name: name, Input: encoded}, true
}

// callSalvageID is the synthesized tool-call ID for a salvaged call — there is
// never more than one per reply, so a fixed ID (matching the "tu_" + index
// pattern the OpenAI adapter's own synthesis uses) is enough to correlate the
// EventToolUseStart and EventToolUse this decorator emits for it.
const callSalvageID = "tu_salvaged"

// IsProseSalvagedCallID reports whether id names a tool call
// WithProseToolCallSalvage recovered from a model's free-form text rather
// than a structured tool_calls entry (P81.28/FIND-28). The engine uses this
// to label the call's provenance in the approval prompt, since untrusted
// content that reached model context is sometimes quoted back verbatim and
// this salvage path cannot yet tell an intended call from a quoted one.
func IsProseSalvagedCallID(id string) bool {
	return id == callSalvageID
}

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
