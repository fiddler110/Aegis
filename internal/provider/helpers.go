package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// HealthProbeTimeout is the whole-request bound on a Healthy() liveness probe.
// A probe must fail fast when the server is down, not hang — which is why it
// cannot simply reuse the streaming client, whose Client.Timeout is
// deliberately zero so a long prefill isn't cut off (P59.2). Shared by every
// adapter's Healthy(), since they all answer the same question about the same
// class of server: is there a server on the other end again.
const HealthProbeTimeout = 3 * time.Second

// HealthClient builds a Healthy() probe's HTTP client from base: the
// *transport* is the streaming client's, so any proxy, TLS or dialer
// configuration the adapter was given applies to the probe too and the two
// share a connection pool — but the timeout is the probe's own, since
// inheriting the streaming client's unbounded one would defeat the point of a
// liveness check (P61.5). A package-level client with its own default
// transport would quietly mean the probe gating P50.1 recovery ignored the
// user's transport configuration entirely.
//
// Built per call rather than cached: an http.Client is a small value, the
// probe is not a hot path, and the transport — the part that holds state
// worth reusing — is shared, not rebuilt.
func HealthClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{Timeout: HealthProbeTimeout}
	}
	return &http.Client{
		Transport:     base.Transport,
		CheckRedirect: base.CheckRedirect,
		Jar:           base.Jar,
		Timeout:       HealthProbeTimeout,
	}
}

// DrainAndClose discards up to a small bound of resp.Body and closes it — the
// common cleanup after a Healthy() probe reads a status code and nothing else.
func DrainAndClose(resp *http.Response) {
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<12))
}

// TranslateTools converts the harness's tool schemas into the OpenAI-style
// function-calling wire shape ({"type":"function","function":{...}}) shared
// verbatim by the OpenAI-compat and native Ollama adapters.
func TranslateTools[T any](tools []ToolSchema, newTool func(name, description string, parameters json.RawMessage) T) []T {
	out := make([]T, 0, len(tools))
	for _, t := range tools {
		out = append(out, newTool(t.Name, t.Description, t.InputSchema))
	}
	return out
}

// ErrorMessage extracts a human-readable message from the "error" field of a
// streamed chunk, which servers spell either as a bare string (Ollama:
// {"error":"model runner has unexpectedly stopped"}) or as an object with a
// message-shaped field (OpenAI/vLLM: {"error":{"message":"..."}}); altFields
// names additional string fields to try on the object shape before falling
// back to a compacted rendering of the whole object (Ollama's native decoder
// also tries "error" and "detail"). An absent, null, or otherwise-shaped
// field returns "" so the caller can treat the chunk as ordinary.
func ErrorMessage(raw json.RawMessage, altFields ...string) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	fields := append([]string{"message"}, altFields...)
	for _, f := range fields {
		v, ok := obj[f]
		if !ok {
			continue
		}
		var fs string
		if json.Unmarshal(v, &fs) != nil {
			continue
		}
		if fs = strings.TrimSpace(fs); fs != "" {
			return fs
		}
	}
	if len(altFields) == 0 {
		// Matches the OpenAI-compat shape: no alternate fields were offered, so
		// an object with no "message" degrades to its raw JSON rather than
		// being lost, exactly as the object had none of the tried fields.
		return trimmed
	}
	// Matches the native Ollama shape: an object carrying none of the known
	// string fields must still surface, but never as raw multi-line JSON — a
	// present error envelope must never be swallowed into "".
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return trimmed
	}
	return buf.String()
}
