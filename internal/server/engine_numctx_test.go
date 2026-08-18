package server

import (
	"time"

	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// numCtxRecorder answers every turn with a bare end_turn and records what the
// engine actually asked the provider for.
type numCtxRecorder struct {
	mu   sync.Mutex
	last provider.Request
}

func (*numCtxRecorder) Name() string { return "recorder" }

func (a *numCtxRecorder) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.mu.Lock()
	a.last = req
	a.mu.Unlock()
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: "ok"}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	close(ch)
	return ch, nil
}

func (a *numCtxRecorder) request() provider.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last
}

// runOneTurn builds a turn's engine the way handlePostMessage does and runs it,
// returning the model newEngine resolved for the turn.
func runOneTurn(t *testing.T, s *Server, p persona.Persona) string {
	t.Helper()
	eng, model, err := s.newEngine("build", permission.AutoApprove{}, nil, p, false, nil, nil, "", t.TempDir(), "hello", nil, time.Time{})
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	conv := &engine.Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})
	if err := eng.Run(context.Background(), conv, func(engine.Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return model
}

// TestTurnRequestCarriesItsOwnModelsNumCtx is the P52.1 + P52.4 pair end to end:
// a persona pinning a smaller-context model must produce a request asking Ollama
// to serve *that* model's window, not the primary model's. Asking for the
// primary's num_ctx either over-allocates a KV cache the small model doesn't
// need or evicts the primary model to make room; enforcing the primary's window
// lets the prompt overrun what the small model is actually served, which Ollama
// truncates from the front (system prompt first) in silence.
func TestTurnRequestCarriesItsOwnModelsNumCtx(t *testing.T) {
	ts := fakeOllamaMultiModel(t, map[string]int{"gemma4:12b": 32768, "small:1b": 4096}, nil)
	rec := &numCtxRecorder{}
	s := ctxWinServer(32768, "ollama", ts.URL)
	s.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	s.adapter = rec
	s.tools = tool.NewRegistry()
	s.initContextWindow(context.Background())

	// A persona pinning the small model.
	if model := runOneTurn(t, s, persona.Persona{Name: "pinned", Model: "small:1b"}); model != "small:1b" {
		t.Fatalf("turn model = %q, want small:1b", model)
	}
	got := rec.request()
	if got.Model != "small:1b" {
		t.Fatalf("request model = %q, want small:1b", got.Model)
	}
	if got.NumCtx != 4096 {
		t.Errorf("request num_ctx = %d, want 4096 (the small model's served window, not the primary's 32768)", got.NumCtx)
	}

	// Nothing pinned: unchanged from before P52.4 — the configured window.
	if model := runOneTurn(t, s, persona.Persona{Name: "general"}); model != "gemma4:12b" {
		t.Fatalf("turn model = %q, want the global gemma4:12b", model)
	}
	if got := rec.request(); got.NumCtx != 32768 {
		t.Errorf("request num_ctx = %d, want the configured 32768", got.NumCtx)
	}
}

// TestTurnRequestNumCtxUnsetWithoutADetectedWindow: with no context_window
// configured and nothing detectable (a cloud provider), the request carries no
// num_ctx at all and the shared adapter is handed to the engine unwrapped —
// byte-for-byte the pre-P52.4 behavior for every non-Ollama deployment.
func TestTurnRequestNumCtxUnsetWithoutADetectedWindow(t *testing.T) {
	rec := &numCtxRecorder{}
	s := ctxWinServer(0, "anthropic", "")
	s.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	s.adapter = rec
	s.tools = tool.NewRegistry()
	s.initContextWindow(context.Background())

	runOneTurn(t, s, persona.Persona{Name: "general"})
	if got := rec.request(); got.NumCtx != 0 {
		t.Errorf("request num_ctx = %d, want 0 (nothing to say about the window)", got.NumCtx)
	}
	if s.modelAdapter(0) != provider.Adapter(rec) {
		t.Error("with no window the engine must get the shared adapter unwrapped")
	}
}
