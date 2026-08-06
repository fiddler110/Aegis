package providerfactory

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

// --- P61.3 (closed by P61.6): a backend that DIES mid-stream ---
//
// This is the death, not the stall: idlebound_test.go's server delivers headers
// and then goes silent, so nothing arrives and the P59.2 watchdog is what ends
// it. Here the server delivers real chunks and then drops the connection with
// the response half-written, so the consumer's read fails outright and
// scanner.Err() is non-nil — the `ollama serve` killed mid-response shape P50.1
// was filed for.
//
// The two failures are classified on different branches of sse.Run (the
// idleFired branch vs the trailing NewTransportError), and P61.3's defect lived
// on this one: openai.go and anthropic.go wrapped scanner.Err() with a bare
// fmt.Errorf, so both IsBackendUnavailableError and the retry decorator's
// retryable() — which each begin with errors.As(err, &APIError) — classified a
// dead backend as nothing at all. No retry, no waitForBackend, no
// resume-from-disk; the drive just aborted.
//
// It is also distinct from the adapters' own P61.2 tests (openai's
// TestStreamWithoutTerminatorIsAnError, anthropic's
// TestStreamWithoutTerminalEventIsAnError), which close the body *cleanly* mid
// generation: those take the MissingTerminal branch with scanner.Err() == nil
// and never touch the code P61.3 is about. Hence the "the read itself failed"
// assertion below — without it this test would pass on the wrong branch.
//
// It runs at the factory layer for the same reason the idle-bound guard does:
// buildOne constructs all three adapters uniformly, so the table stays the one
// place a fourth adapter has to appear (TestIdleBound_TableCoversEveryBuildableProvider
// enforces that), and the assertion is made against the adapter a real session
// gets rather than against sse.Run in isolation.

// deadStreamBodies is the partial response each adapter's decoder is fed before
// the connection dies: well-formed content chunks in that adapter's own wire
// format, deliberately with no terminator, because a server that was killed
// never sent one. The bytes matter only in that the decoder must accept them —
// what is under test is what happens to the read failure that follows.
var deadStreamBodies = map[string]string{
	"ollama":    "{\"message\":{\"role\":\"assistant\",\"content\":\"partial \"}}\n",
	"openai":    "data: {\"choices\":[{\"delta\":{\"content\":\"partial \"}}]}\n\n",
	"anthropic": "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial \"}}\n\n",
}

// TestBackendDeathMidStreamIsRecoverable is the P61.3 end-to-end regression: on
// every adapter, a backend that dies mid-stream must surface as an error that
// BOTH recovery mechanisms can see — provider.IsBackendUnavailableError (which
// gates drive.recoverBackendDown's waitForBackend/resume-from-disk, P50.1) and
// the retry decorator's retryable() predicate.
func TestBackendDeathMidStreamIsRecoverable(t *testing.T) {
	for _, tc := range idleBoundAdapters {
		t.Run(tc.name, func(t *testing.T) {
			body, ok := deadStreamBodies[tc.name]
			if !ok {
				t.Fatalf("no partial-response fixture for %q — add one in that adapter's wire format", tc.name)
			}
			err := probeKilledStream(t, tc.name, tc.apiKey, body)
			if err == nil {
				t.Fatal("the stream ended without an error event; a connection that died mid-response is not a finished turn")
			}

			// The branch check. A clean close with no terminator (P61.2) also
			// produces a transport error, and would satisfy everything below
			// while proving nothing about P61.3 — so require the underlying
			// read failure to have survived into the message. The exact text
			// belongs to net/http and the OS, not to us, hence the set.
			if !looksLikeReadFailure(err) {
				t.Fatalf("err = %v, want the mid-stream read failure preserved — this must be sse.Run's "+
					"scanner.Err() branch (P50.1/P61.3), not the clean-EOF missing-terminator branch (P61.2)", err)
			}

			// P61.3's defect, stated directly: a bare fmt.Errorf here is
			// unclassifiable, because every classifier starts with errors.As.
			var apiErr *provider.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v (%T), want a *provider.APIError — a bare error is classified by nothing "+
					"(P61.3); construct it with provider.NewTransportError", err, err)
			}
			if !provider.IsBackendUnavailableError(err) {
				t.Errorf("IsBackendUnavailableError(%v) = false, want true so drive.recoverBackendDown waits "+
					"for the backend and resumes the phase from disk (P50.1)", err)
			}
			// provider.retryable() is unexported; APIError.Retryable is the
			// predicate it delegates to, so asserting it here is the same
			// question the retry decorator asks.
			if !apiErr.Retryable() {
				t.Errorf("Retryable() = false for %v, want true so provider.WithRetry re-attempts a "+
					"transport failure instead of surfacing it as terminal", err)
			}
		})
	}
}

// oversizedLinePrefixes is what each adapter's wire format needs in front of a
// single enormous payload for the line to be one its decoder would have parsed.
// It never gets that far — the scanner fails before the decoder is called —
// which is exactly the point: the failure is the scanner's, so every adapter
// must report it the same way.
var oversizedLinePrefixes = map[string]string{
	"ollama":    `{"message":{"role":"assistant","content":"`,
	"openai":    `data: {"choices":[{"delta":{"content":"`,
	"anthropic": `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`,
}

// TestOversizedLineIsNamedOnEveryAdapter is P61.3's second half (P35.12): a
// single stream line past sse's 4MiB scanner cap — realistically a tool-call
// argument payload the model over-generated — must name that cause rather than
// surfacing bufio's opaque "token too long". Only the ollama adapter did this
// before P61.6 lifted the check into sse.Run.
//
// It is deliberately checked through the built adapters, not just sse.Run: the
// claim P61.3 makes is about what the openai path reports, and "the shared
// helper handles it" is only true while every adapter still routes through the
// shared helper. The 4MiB fixture costs milliseconds over loopback, so this
// stays in the default suite.
func TestOversizedLineIsNamedOnEveryAdapter(t *testing.T) {
	for _, tc := range idleBoundAdapters {
		t.Run(tc.name, func(t *testing.T) {
			prefix, ok := oversizedLinePrefixes[tc.name]
			if !ok {
				t.Fatalf("no oversized-line fixture for %q", tc.name)
			}
			// 4MiB is sse's maxBufSize (unexported there); one byte over the
			// cap on a single line is all that is needed.
			body := prefix + strings.Repeat("x", 4*1024*1024) + "\"}}\n\n"
			err := probeKilledStream(t, tc.name, tc.apiKey, body)
			if err == nil {
				t.Fatal("no error event for a line past the scanner's 4MiB cap")
			}
			if !errors.Is(err, bufio.ErrTooLong) {
				t.Fatalf("err = %v, want it to wrap bufio.ErrTooLong", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "4MiB line limit") || !strings.Contains(msg, "tool-call argument payload") {
				t.Errorf("err = %v, want the cause named instead of bufio's opaque %q (P35.12)",
					msg, bufio.ErrTooLong)
			}
			if !strings.HasPrefix(msg, tc.name+": ") {
				t.Errorf("err = %v, want the provider named so a multi-adapter session says which one", msg)
			}
			// Deliberately NOT a transport APIError, unlike the mid-stream read
			// failure above: the backend is alive and re-running would produce
			// the same oversized payload, so neither the retry decorator nor
			// P50.1's wait-and-resume should engage.
			if provider.IsBackendUnavailableError(err) {
				t.Errorf("err = %v was classified backend-unavailable; an oversized line is a payload "+
					"problem, and waiting for a healthy backend to return would spin", err)
			}
		})
	}
}

// livenessProbeWired records which adapters answer provider.CheckBackendHealth,
// which is the *second* precondition P50.1's recovery has and the one P61.3 did
// not mention. Correct classification only gets the error as far as
// drive.recoverBackendDown; that function then returns backendNotDown unless the
// adapter can say "the server is back" (drive/health.go:75-80), because there is
// nothing to wait *on* otherwise.
//
// Both local-capable adapters now answer: ollama.Adapter with a GET
// /api/version, and openai.Adapter — the path P61.3 names, Ollama's
// OpenAI-compat /v1 — with a GET <base>/models, the OpenAI-shaped equivalent
// (/api/version is not part of that API, and this adapter also serves real
// OpenAI, LM Studio and gateways). Until that was added the /v1 path was the
// residual gap this map recorded: classified, but never waited out.
var livenessProbeWired = map[string]bool{
	"ollama": true,
	"openai": true,
	// By design, and the reason is about the *backend*, not about which adapter
	// is "native": there is no local Anthropic server to wait for. A managed
	// remote service is either having a transient outage — already the retry
	// decorator's job — or an outage no bounded wait-and-resume would outlast,
	// and probing it is a billable round-trip against someone else's quota. The
	// distinction P50.1 draws is exactly this: supported==false means "don't
	// wait on this backend", which is the honest answer here.
	"anthropic": false,
}

// TestLivenessProbeReachIsRecorded keeps the map above honest against the real
// adapters, so the boundary of P50.1's recovery is a checked fact rather than a
// belief. It uses the same table as the tests above, so a fourth adapter has to
// declare where it stands.
func TestLivenessProbeReachIsRecorded(t *testing.T) {
	for _, tc := range idleBoundAdapters {
		t.Run(tc.name, func(t *testing.T) {
			want, ok := livenessProbeWired[tc.name]
			if !ok {
				t.Fatalf("provider %q is buildable but not in livenessProbeWired — record whether a "+
					"mid-stream death on it can actually reach drive.waitForBackend (P50.1)", tc.name)
			}
			a, err := buildOne(tc.name, tc.apiKey, "http://127.0.0.1:1", nil, nil, "", 0, 0, "", 0, 0, nil, nil)
			if err != nil {
				t.Fatalf("buildOne(%s): %v", tc.name, err)
			}
			// The probe's verdict is irrelevant (nothing is listening); only
			// whether any adapter in the chain could answer at all.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, supported := provider.CheckBackendHealth(ctx, a); supported != want {
				t.Errorf("CheckBackendHealth supported = %v for %s, want %v — if a liveness probe was just "+
					"added, flip livenessProbeWired; if one was lost, P50.1's wait-and-resume went inert",
					supported, tc.name, want)
			}
		})
	}
}

// looksLikeReadFailure reports whether err carries the transport's own
// description of a connection that broke mid-body. Which one lands depends on
// the platform and on how far the response got before the peer went away, so
// the check is a set rather than one string.
func looksLikeReadFailure(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"unexpected eof", "connection reset", "broken pipe", "forcibly closed"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// probeKilledStream builds one adapter pointed at a server that writes part of
// a chunked response and then drops the connection without the terminating
// chunk, and returns the error the stream ends with (nil if it ended without
// one). Hijacking is what makes the kill real: net/http would otherwise finish
// the response framing for us on handler return, which the client would read as
// a clean EOF — the P61.2 case, not this one.
func probeKilledStream(t *testing.T, name, apiKey, partial string) error {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("httptest ResponseWriter is not a Hijacker; cannot kill the connection mid-response")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
		// One chunk of real body, then nothing: no zero-length chunk, no
		// trailer. The client's chunked reader reports the truncation instead
		// of EOF, which is precisely the distinction this test rests on.
		_, _ = fmt.Fprintf(buf, "%x\r\n%s\r\n", len(partial), partial)
		_ = buf.Flush()
	}))
	defer srv.Close()

	// The idle bound is left off (0 -> the adapter's default, minutes long) so
	// the watchdog cannot be what ends this stream: the read failure must.
	a, err := buildOne(name, apiKey, srv.URL, nil, nil, "", 0, 0, "", 0, 0, nil, nil)
	if err != nil {
		t.Fatalf("buildOne(%s): %v", name, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := a.Stream(ctx, provider.Request{
		Model:    "test-model",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		// A synchronous failure is already a NewTransportError on all three
		// adapters; the assertions apply to it unchanged.
		return err
	}

	var last error
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, open := <-stream:
			if !open {
				return last
			}
			switch ev.Type {
			case provider.EventError:
				last = ev.Err
			case provider.EventDone:
				t.Errorf("%s emitted EventDone for a stream whose connection died mid-response", name)
			}
		case <-deadline:
			t.Fatalf("%s never ended the stream after the connection died", name)
			return nil
		}
	}
}
