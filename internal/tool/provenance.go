package tool

import "context"

type callProvenanceKey struct{}

// WithCallProvenance annotates ctx with a short, human-readable note about
// where a tool call actually came from, when that differs from an ordinary
// model-emitted structured call — e.g. a call recovered by parsing the
// model's free-form text (P81.28/FIND-28). permission.Gate.Check appends it
// to the reason it shows an approver, so a human approving a write/execute
// call sees the call's provenance rather than treating it as an ordinary
// request. An empty note is a no-op.
func WithCallProvenance(ctx context.Context, note string) context.Context {
	if note == "" {
		return ctx
	}
	return context.WithValue(ctx, callProvenanceKey{}, note)
}

// CallProvenance returns the note set by WithCallProvenance, or "" if none
// was set.
func CallProvenance(ctx context.Context) string {
	note, _ := ctx.Value(callProvenanceKey{}).(string)
	return note
}
