package providerfactory

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

// idleBoundAdapters enumerates every provider buildOne can construct, together
// with whether provider.stream_idle_timeout (P59.2) is wired through to it yet.
//
// The guard this table exists for is structural (P61.6): the failure mode is
// "the next adapter forgets", not "an adapter is wrong". sse.Run now owns the
// watchdog *mechanism* for all three adapters, so wiring a new one is a single
// Options field — and forgetting it is invisible until a local runner wedges in
// production, which is exactly how the bound ended up on one adapter of three.
//
// All three are wired as of P61.1, so all three assert hard. wired=false stays
// in the table's shape as the way to record a *documented gap* rather than a
// silent one: a new adapter added without an idle bound skips with a reason
// instead of passing quietly, and flipping the flag once it is wired is the
// only change needed. TestIdleBound_TableCoversEveryBuildableProvider below is
// what stops a fourth adapter from being added without appearing here at all.
var idleBoundAdapters = []struct {
	name   string
	apiKey string
	wired  bool
}{
	{name: "ollama", wired: true},
	{name: "openai", apiKey: "fake-key", wired: true},
	{name: "anthropic", apiKey: "fake-key", wired: true},
}

// TestIdleBound_EveryAdapterBoundsAStalledStream drives each constructed
// adapter against a server that returns headers and then goes silent — the
// wedged-runner shape P59.2 was filed for, which nothing else bounds
// (Client.Timeout is deliberately zero, ResponseHeaderTimeout stopped applying
// when the headers arrived, and cost.max_wall_clock_per_run is polled between
// turns and off by default).
func TestIdleBound_EveryAdapterBoundsAStalledStream(t *testing.T) {
	for _, tc := range idleBoundAdapters {
		t.Run(tc.name, func(t *testing.T) {
			err, ok := probeStalledStream(t, tc.name, tc.apiKey)
			stalled := ok && strings.Contains(err.Error(), "stalled mid-generation")
			switch {
			case stalled:
				if !provider.IsBackendUnavailableError(err) {
					t.Errorf("idle abort on %s = %v, want a transport error so a wedged runner takes the "+
						"same waitForBackend/resume-from-disk path as a crashed one", tc.name, err)
				}
			case tc.wired:
				t.Errorf("%s did not bound a stalled stream (got %v, ok=%v); provider.stream_idle_timeout "+
					"must reach every adapter — see sse.Run's Options.IdleTimeout", tc.name, err, ok)
			default:
				t.Skipf("%s has no idle bound wired yet — pass sse.Options.IdleTimeout from its "+
					"WithStreamIdleTimeout option, then flip wired=true in idleBoundAdapters", tc.name)
			}
		})
	}
}

// TestIdleBound_TableCoversEveryBuildableProvider keeps the table above honest:
// buildOne names its supported providers in the error it returns for an unknown
// one, so a fourth adapter cannot be added without this test noticing.
func TestIdleBound_TableCoversEveryBuildableProvider(t *testing.T) {
	_, err := buildOne("definitely-not-a-provider", "", "", nil, nil, "", 0, 0, "", 0, 0, nil, nil)
	if err == nil {
		t.Fatal("buildOne accepted an unknown provider")
	}
	_, list, ok := strings.Cut(err.Error(), "(supported: ")
	if !ok {
		t.Fatalf("buildOne's unsupported-provider error no longer lists the supported set: %v", err)
	}
	list = strings.TrimSuffix(strings.TrimSpace(list), ")")

	covered := map[string]bool{}
	for _, a := range idleBoundAdapters {
		covered[a.name] = true
	}
	for _, name := range strings.Split(list, ",") {
		if name = strings.TrimSpace(name); name != "" && !covered[name] {
			t.Errorf("provider %q is buildable but missing from idleBoundAdapters — every adapter needs an "+
				"idle bound (P59.2/P61.6), so add it with wired=true or a wired=false gap note", name)
		}
	}
}

// probeStalledStream builds one adapter pointed at a server that flushes
// response headers and then sends nothing, and reports the error the stream
// ends with (ok=false if it never ended). The idle timeout is configured tiny;
// the handler is released as soon as the probe is done so httptest.Close does
// not wait on it.
func probeStalledStream(t *testing.T, name, apiKey string) (error, bool) {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // headers land; the body then never arrives
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(release)

	a, err := buildOne(name, apiKey, srv.URL, nil, nil, "", 0, 0, "", 0, 50*time.Millisecond, nil, nil)
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
		return err, true
	}

	var last error
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, open := <-stream:
			if !open {
				if last == nil {
					last = fmt.Errorf("stream closed with no error event")
				}
				return last, true
			}
			if ev.Type == provider.EventError {
				last = ev.Err
			}
		case <-deadline:
			// Nothing bounded the stall: the consumer is still blocked on a
			// read. Cancelling ctx is what unblocks it here — in production
			// there is no such caller.
			return last, false
		}
	}
}
