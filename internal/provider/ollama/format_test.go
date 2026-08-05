package ollama

import (
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// TestStreamPassesFormat is the P59.8 wire guard: a Request.Format has to
// arrive as Ollama's top-level `format`, which is what makes the schema a
// decode-time grammar rather than a suggestion. Verbatim, because Ollama —
// not this adapter — owns the JSON Schema dialect.
func TestStreamPassesFormat(t *testing.T) {
	srv, last := numCtxEcho(t)
	a := New(WithBaseURL(srv.URL))

	schema := json.RawMessage(`{"type":"object","properties":{"summary":{}},"required":["summary"]}`)
	drain(t, a, provider.Request{Model: "m", Format: schema})
	got := last()
	if string(got.Format) != string(schema) {
		t.Errorf("wire format = %s, want %s", got.Format, schema)
	}
}

// TestStreamOmitsEmptyFormat: every ordinary turn must leave `format` off the
// wire entirely. A present-but-empty value is not a no-op to Ollama, and this
// is the path all but one request in a run takes.
func TestStreamOmitsEmptyFormat(t *testing.T) {
	srv, last := numCtxEcho(t)
	a := New(WithBaseURL(srv.URL))

	drain(t, a, provider.Request{Model: "m"})
	if got := last(); got.Format != nil {
		t.Errorf("unconstrained request carried format = %s, want absent", got.Format)
	}

	// And the serialized body must not carry the key at all — the decoded
	// struct above cannot tell an omitted key from a JSON null.
	body, err := json.Marshal(wireRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["format"]; present {
		t.Errorf("wire body carries a format key with no schema set: %s", body)
	}
}
