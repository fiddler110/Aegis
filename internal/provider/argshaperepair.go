package provider

import (
	"context"
	"encoding/json"
	"strings"
)

// argShapeAdapter repairs a structured tool call's arguments when their JSON
// shape doesn't match what the schema declared (P74.9's second half, folded
// into P74.17 as the harness mechanism's second piece of cargo).
//
// Unlike proseToolCallAdapter this never buffers a whole turn: EventToolUse
// already carries the complete Input by the time it is emitted (the OpenAI
// adapter's chunkDecoder.Finish accumulates the streamed "arguments" string
// and only emits once it is valid JSON — see internal/provider/openai), so
// each event can be inspected and, if needed, rewritten in place as it
// passes through.
//
// It only ever *repairs a shape*, never validates one: an object whose keys
// don't match the schema at all is left exactly as it arrived; the tool call
// still fails downstream, the same as it does today, rather than guessing
// wrong and failing differently. See repairArgumentShape for the three shapes
// it recognizes.
type argShapeAdapter struct {
	base Adapter
}

// WithArgumentShapeRepair wraps base so a tool call's arguments are repaired
// in place when their JSON shape doesn't match the tool's schema — a local
// model emitting its arguments double-encoded as a JSON string, wrapped
// under a redundant "arguments"/"parameters" key, or as a bare scalar where
// the schema declares exactly one property. Returns base unchanged when base
// is nil.
func WithArgumentShapeRepair(base Adapter) Adapter {
	if base == nil {
		return base
	}
	return &argShapeAdapter{base: base}
}

func (a *argShapeAdapter) Name() string    { return a.base.Name() }
func (a *argShapeAdapter) Unwrap() Adapter { return a.base }

func (a *argShapeAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	in, err := a.base.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	// Nothing to repair a shape against without a schema to compare it to.
	if len(req.Tools) == 0 {
		return in, nil
	}
	props := make(map[string][]string, len(req.Tools))
	for _, t := range req.Tools {
		props[t.Name] = schemaPropertyNames(t.InputSchema)
	}
	out := make(chan Event, 4)
	go func() {
		defer close(out)
		for ev := range in {
			if ev.Type == EventToolUse && ev.ToolUse != nil {
				repaired := *ev.ToolUse
				repaired.Input = repairArgumentShape(repaired.Input, props[repaired.Name])
				ev.ToolUse = &repaired
			}
			out <- ev
		}
	}()
	return out, nil
}

// argWrapperKeys are the field names a model reaches for when it nests
// arguments under one more layer than the schema asked for — the same
// vocabulary parseCallObject already accepts for a prose-salvaged call, kept
// consistent so the two repair paths recognize the same mistake the same
// way.
var argWrapperKeys = []string{"arguments", "parameters", "input", "args", "params"}

// repairArgumentShape returns input with its JSON shape corrected against
// propNames (the tool's top-level schema properties), or input unchanged
// when no recognized mistake applies or a repair would itself be invalid
// JSON. It never fabricates content: every branch either unwraps something
// the model already sent or wraps a bare value in the one key a
// single-property schema makes unambiguous — it does not invent field names
// for a multi-property schema, where a wrong guess would fail silently
// instead of failing loud.
func repairArgumentShape(input json.RawMessage, propNames []string) json.RawMessage {
	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" {
		return json.RawMessage("{}")
	}
	if !json.Valid([]byte(trimmed)) {
		// Not this decorator's problem: a syntactically invalid payload never
		// reaches here in practice (the OpenAI adapter reports it as a
		// malformed-call error before emitting EventToolUse at all), and if it
		// somehow did, guessing at a syntax fix is out of scope for a shape
		// repair. Leave it for the existing malformed-call handling.
		return input
	}

	// Double-encoded: the whole payload is a JSON string whose *contents* are
	// themselves a JSON object. Unwrap one layer and keep going, so a
	// wrapped-and-double-encoded call still gets both repairs. A JSON string
	// whose contents are *not* further JSON (the ordinary case — the model
	// meant a plain string value) falls through to the bare-scalar wrap
	// branch below instead of being returned unchanged.
	if strings.HasPrefix(trimmed, "\"") {
		var inner string
		if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
			innerTrimmed := strings.TrimSpace(inner)
			if strings.HasPrefix(innerTrimmed, "{") && json.Valid([]byte(innerTrimmed)) {
				return repairArgumentShape(json.RawMessage(innerTrimmed), propNames)
			}
		}
	} else if strings.HasPrefix(trimmed, "{") {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
			return input
		}
		if objectMatchesSchema(obj, propNames) {
			return input
		}
		// A single extra layer of nesting under a recognized wrapper key: the
		// object has exactly one field and that field is both a known wrapper
		// name and itself an object.
		if len(obj) == 1 {
			for _, key := range argWrapperKeys {
				raw, ok := obj[key]
				if !ok {
					continue
				}
				innerTrimmed := strings.TrimSpace(string(raw))
				if strings.HasPrefix(innerTrimmed, "{") && json.Valid([]byte(innerTrimmed)) {
					return json.RawMessage(innerTrimmed)
				}
			}
		}
		// Doesn't match the schema and isn't a recognized wrapper — leave it
		// alone rather than guess further.
		return input
	}

	// A bare scalar or array where an object was expected. Only wrappable
	// when the schema is unambiguous about which single field it belongs to.
	if len(propNames) == 1 {
		wrapped, err := json.Marshal(map[string]json.RawMessage{propNames[0]: json.RawMessage(trimmed)})
		if err == nil {
			return wrapped
		}
	}
	return input
}

// objectMatchesSchema reports whether obj already names at least one of the
// tool's declared properties, which is treated as "this call is already
// shaped correctly" — good enough to stop repairing, without requiring every
// field to be present (the model may simply have omitted an optional one).
// A schema with no declared properties (a no-argument tool) always matches.
func objectMatchesSchema(obj map[string]json.RawMessage, propNames []string) bool {
	if len(propNames) == 0 {
		return true
	}
	for _, name := range propNames {
		if _, ok := obj[name]; ok {
			return true
		}
	}
	return false
}

// schemaPropertyNames extracts the top-level property names from a JSON
// Schema object (ToolSchema.InputSchema), or nil when it declares none or
// doesn't parse. Only the shape needed to recognize the wrapper/scalar
// mistakes above — it does not resolve $ref, oneOf, or nested schemas.
func schemaPropertyNames(schema json.RawMessage) []string {
	if len(schema) == 0 {
		return nil
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil || len(s.Properties) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	return names
}
