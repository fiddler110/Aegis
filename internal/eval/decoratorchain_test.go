package eval

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/provider/providertest"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestScenario_RealDecoratorChainStreams is M10, and it is the test that would
// have caught CRIT-4 on its first run.
//
// Every other scenario in this package hands its scripted adapter straight to
// engine.New, so the provider decorator chain that ships — retry, admission
// control, the per-model harness profile and the prose-tool-call salvage inside
// it — was never exercised *in composition with the engine*. That is the only
// place its behavior is observable: the salvage decorator buffered the entire
// turn before forwarding a single event, which produces byte-identical output
// and an entirely different turn, since the engine's stall heartbeat beats on
// events received and P67.7 dispatches a tool call the moment it is announced.
//
// Two things make the assertion possible. Scenario.Decorators runs the scenario
// through providerfactory.Decorate — the same call Build makes, so this tests
// the composition that ships rather than a hand-assembled approximation. And
// providertest.Adapter's WithEventDelay spaces the script out, which turns
// "did the first event arrive before the turn ended" into a measurement.
func TestScenario_RealDecoratorChainStreams(t *testing.T) {
	const delay = 40 * time.Millisecond
	const events = 6

	adapter := providertest.New(
		providertest.Text("thinking about it", " — ", "here", " is", " the", " answer"),
	).WithEventDelay(delay)

	// A local profile is the population the harness decorators actually engage
	// for: profile.NewResolver turns on prose-tool-call salvage and argument
	// shape repair for every model served by a local provider.
	cfg := &config.Config{}
	cfg.Provider.Default = "ollama"
	cfg.Provider.Model = "qwen3.5:9b"

	s := Scenario{
		Name:   "real decorator chain streams",
		System: "sys",
		Options: engine.Options{
			Adapter: adapter, Tools: tool.NewRegistry(), Model: cfg.Provider.Model, MaxTokens: 100,
		},
		Decorators: []func(provider.Adapter) provider.Adapter{
			func(a provider.Adapter) provider.Adapter {
				return providerfactory.Decorate(a, cfg, slog.New(slog.DiscardHandler))
			},
		},
		Turns: []string{"hello"},
	}

	// The whole turn takes events*delay; a decorator that buffers it cannot
	// deliver anything before that. The limit sits well below it and well above
	// the first event's own delay, so this measures buffering, not latency.
	RunAndCheck(t, context.Background(), s,
		ExpectNoError(),
		ExpectFinalTextContains("here is the answer"),
		ExpectStreamsIncrementally(delay*(events-2)),
	)
}

// TestScenario_RealDecoratorChainForwardsToolCallsEarly is the P67.7 half: a
// structured tool call must reach the engine while the turn is still
// generating, not after it ends, because that is where the dispatch
// optimization's own comment says it pays — a local model, where generation
// latency dominates.
func TestScenario_RealDecoratorChainForwardsToolCallsEarly(t *testing.T) {
	const delay = 40 * time.Millisecond

	reg := tool.NewRegistry()
	et := &echoTool{}
	if err := reg.Register(et); err != nil {
		t.Fatal(err)
	}

	// A turn that narrates at length before its call: under a buffering
	// decorator the call is announced only after every one of those deltas.
	call := providertest.ToolCall("tu_1", "echo", `{"msg":"hi"}`)
	narrated := append([]provider.Event{
		{Type: provider.EventTextDelta, Text: "let"},
		{Type: provider.EventTextDelta, Text: " me"},
		{Type: provider.EventTextDelta, Text: " check"},
		{Type: provider.EventTextDelta, Text: " that"},
	}, call...)

	adapter := providertest.New(narrated, providertest.Text("done")).WithEventDelay(delay)

	cfg := &config.Config{}
	cfg.Provider.Default = "ollama"
	cfg.Provider.Model = "qwen3.5:9b"

	s := Scenario{
		Name:   "real decorator chain forwards tool calls early",
		System: "sys",
		Options: engine.Options{
			Adapter: adapter, Tools: reg, Model: cfg.Provider.Model, MaxTokens: 100,
		},
		Decorators: []func(provider.Adapter) provider.Adapter{
			func(a provider.Adapter) provider.Adapter {
				return providerfactory.Decorate(a, cfg, slog.New(slog.DiscardHandler))
			},
		},
		Turns: []string{"hello"},
	}
	RunAndCheck(t, context.Background(), s,
		ExpectNoError(),
		ExpectToolCalled("echo"),
		ExpectFinalTextContains("done"),
		ExpectStreamsIncrementally(delay*3),
	)
	if et.calls != 1 {
		t.Errorf("echo tool executed %d times, want 1", et.calls)
	}
}
