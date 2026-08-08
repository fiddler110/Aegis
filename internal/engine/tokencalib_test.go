package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
)

// densebackendAdapter is a fake backend that tokenizes at a known multiple of
// this repo's heuristic and reports it the way native Ollama does.
//
// It is the shape of P62.4's finding rather than a stand-in for it: the defect
// was never that the estimate is imprecise, it was that the engine had no way to
// notice a *systematic* bias it is told about every single turn. So the fake
// states the bias exactly — reported = ratio x (transcript + tool schemas) —
// which lets a test assert the engine converges on it instead of asserting some
// hand-picked threshold moved.
type denseBackendAdapter struct {
	ratio float64
	turns [][]provider.Event
	calls int
	// window, when > 0, clamps the reported count the way a real server does on
	// a truncated prompt (Ollama reports num_ctx-1).
	window int

	lastReported int
}

func (d *denseBackendAdapter) Name() string { return "dense" }

func (d *denseBackendAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	events := d.turns[d.calls]
	d.calls++

	real := int(float64(tokenest.Messages(req.System, req.Messages)+tokenest.Tools(req.Tools)) * d.ratio)
	if d.window > 0 && real >= d.window {
		real = d.window - 1
	}
	d.lastReported = real

	ch := make(chan provider.Event, len(events))
	for _, ev := range events {
		if ev.Type == provider.EventDone {
			ev.Usage = &provider.Usage{
				InputTokens:  real,
				OutputTokens: 5,
				// The native-Ollama marker afterTurn keys on. Without it the turn
				// is not evidence at all, which TestCalibrationIgnores... below
				// pins separately.
				PromptEvalDurationMS: 40,
			}
		}
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// bulkTool returns a large fixed payload, to grow a conversation by a known
// amount in one tool round.
type bulkTool struct{ chars int }

func (b *bulkTool) Name() string                 { return "bulk" }
func (b *bulkTool) Description() string          { return "return a large payload" }
func (b *bulkTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (b *bulkTool) Capability() tool.Capability  { return tool.CapRead }
func (b *bulkTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: strings.Repeat("payload text ", b.chars/13)}, nil
}

func toolCallTurn(id string) []provider.Event {
	return []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: id, Name: "bulk", Input: json.RawMessage(`{}`)}},
		{Type: provider.EventDone, Stop: provider.StopToolUse},
	}
}

func endTurn() []provider.Event {
	return []provider.Event{
		{Type: provider.EventTextDelta, Text: "done"},
		{Type: provider.EventDone, Stop: provider.StopEndTurn},
	}
}

// TestNativeToolSchemasCountTowardTheEstimate is P62.4's structural half at the
// engine seam. The schemas ride Request.Tools on every native turn and the
// backend prices them with the transcript, but conv holds neither — so before
// this the estimate driving compaction omitted them entirely.
//
// Asserted as a difference between two registries rather than against a fixed
// number, so it stays true as the builtin catalog changes.
func TestNativeToolSchemasCountTowardTheEstimate(t *testing.T) {
	conv := bigConversation()

	bare, err := New(Options{Adapter: endTurnAdapter(), Tools: tool.NewRegistry(), Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := reg.Register(&namedFakeTool{name: name}); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := New(Options{Adapter: endTurnAdapter(), Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	bareEst := bare.newCompactionGuard().estimate(conv)
	loadedEst := loaded.newCompactionGuard().estimate(conv)
	if loadedEst <= bareEst {
		t.Errorf("estimate with 3 tools (%d) is not above the estimate with none (%d) — "+
			"Request.Tools is not being counted", loadedEst, bareEst)
	}
	if want := bareEst + tokenest.Tools(reg.Schemas()); loadedEst != want {
		t.Errorf("estimate with tools = %d, want %d (bare + tokenest.Tools)", loadedEst, want)
	}
}

// TestCalibrationConvergesOnTheBackendsRatio: after a turn against a backend
// that tokenizes 1.5x denser than the heuristic, the engine's estimate for the
// same conversation must match what that backend actually reported. This is the
// property P62.4 asks for — not "the number went up".
func TestCalibrationConvergesOnTheBackendsRatio(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	adapter := &denseBackendAdapter{ratio: 1.5, turns: [][]provider.Event{endTurn()}}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test",
		MaxTokens: 100, ContextWindowTokens: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}

	conv := bigConversation()
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Re-price the conversation as it stood when the request went out. The
	// assistant reply landed after, so drop it.
	sent := &Conversation{System: conv.System, Messages: conv.Messages[:len(conv.Messages)-1]}
	g := eng.newCompactionGuard()
	g.calib.Observe(sent.estimatedTokens(), g.requestOverhead, adapter.lastReported, 1_000_000)

	got := g.estimate(sent)
	if diff := got - adapter.lastReported; diff < -2 || diff > 2 {
		t.Errorf("calibrated estimate = %d, backend reported %d — the correction did not converge",
			got, adapter.lastReported)
	}
	// And the uncorrected estimate must genuinely have been wrong, or the test
	// proves nothing about the correction.
	if raw := sent.estimatedTokens() + g.requestOverhead; raw >= adapter.lastReported {
		t.Errorf("raw estimate %d was not below the reported %d — the fixture is not exercising an undercount",
			raw, adapter.lastReported)
	}
}

// TestCalibrationIgnoresNonOllamaUsage: a provider that reports no prefill
// duration is not on the path where InputTokens is documented to be the full
// prompt every turn, and is also not a backend that truncates in silence.
// Learning from it would apply a correction derived from a different meaning of
// the same field.
func TestCalibrationIgnoresNonOllamaUsage(t *testing.T) {
	eng, err := New(Options{Adapter: endTurnAdapter(), Tools: tool.NewRegistry(), Model: "test",
		MaxTokens: 100, ContextWindowTokens: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()
	g.lastRaw = 1000

	for _, tc := range []struct {
		name  string
		usage provider.Usage
	}{
		{"no prefill duration", provider.Usage{InputTokens: 5000}},
		{"estimated usage", provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10, IsEstimated: true}},
		{"cache accounting present", provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10, CacheReadTokens: 400}},
		{"cache creation present", provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10, CacheCreationTokens: 400}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g.lastRaw = 1000
			u := tc.usage
			g.afterTurn(&u, 10_000)
			if _, samples := g.calib.Scale(); samples != 0 {
				t.Errorf("learned from %s usage (samples=%d), want it ignored", tc.name, samples)
			}
		})
	}
}

// TestCalibrationSkipsToolSuppressedTurns: on a step-limit or schema-retry turn
// the request carries no tool schemas, so its reported count has a different
// basis than requestOverhead assumes. Learning from it would teach the estimate
// that the schemas cost nothing.
func TestCalibrationSkipsToolSuppressedTurns(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: endTurnAdapter(), Tools: reg, Model: "test",
		MaxTokens: 100, ContextWindowTokens: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()

	g.beforeTurn(context.Background(), bigConversation(), func(Event) {}, true)
	if g.lastRaw != 0 {
		t.Fatalf("lastRaw = %d after a tools-suppressed turn, want 0 (not a usable sample)", g.lastRaw)
	}
	g.afterTurn(&provider.Usage{InputTokens: 9000, PromptEvalDurationMS: 10}, 10_000)
	if _, samples := g.calib.Scale(); samples != 0 {
		t.Errorf("learned from a tools-suppressed turn (samples=%d), want it ignored", samples)
	}

	// The same turn with schemas present is a usable sample, or the guard above
	// would be indistinguishable from calibration never working at all.
	g.beforeTurn(context.Background(), bigConversation(), func(Event) {}, false)
	if g.lastRaw <= 0 {
		t.Fatalf("lastRaw = %d after a normal turn, want > 0", g.lastRaw)
	}
	g.afterTurn(&provider.Usage{InputTokens: 9000, PromptEvalDurationMS: 10}, 10_000)
	if _, samples := g.calib.Scale(); samples != 1 {
		t.Errorf("samples = %d after a normal turn, want 1", samples)
	}
}

// recordingCalibratedCompactor is a Compactor that also records the corrections
// pushed to it.
type recordingCalibratedCompactor struct {
	noticeCompactor
	overheads []int
	scales    []float64
}

func (r *recordingCalibratedCompactor) SetEstimateCorrection(overhead int, scale float64) {
	r.overheads = append(r.overheads, overhead)
	r.scales = append(r.scales, scale)
}

// TestCalibrationReachesTheCompactor: the engine and the Compactor run two gates
// over the same messages, so a correction the engine keeps to itself re-creates
// P41.1 — the engine asks for compaction the summarizer then declines, and the
// engine reads that as "nothing left to compact".
func TestCalibrationReachesTheCompactor(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &recordingCalibratedCompactor{}
	adapter := &denseBackendAdapter{ratio: 1.5, turns: [][]provider.Event{endTurn()}}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Compactor: comp, Model: "test",
		MaxTokens: 100, ContextWindowTokens: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), bigConversation(), func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(comp.scales) == 0 {
		t.Fatal("no correction was pushed to the compactor")
	}
	if got := comp.scales[len(comp.scales)-1]; got < 1.4 || got > 1.6 {
		t.Errorf("pushed scale = %v, want ~1.5 (the backend's ratio)", got)
	}
	if got := comp.overheads[len(comp.overheads)-1]; got != tokenest.Tools(reg.Schemas()) {
		t.Errorf("pushed overhead = %d, want %d (the tool schemas)", got, tokenest.Tools(reg.Schemas()))
	}
}

// summaryAdapter answers every request with a fixed one-line summary, for
// driving a real compaction.Summarizer.
type summaryAdapter struct{}

func (summaryAdapter) Name() string { return "summary" }

func (summaryAdapter) Stream(context.Context, provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: "earlier turns summarized"}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

// TestCalibratedEngineAndRealSummarizerAgree is the integration form of the
// test above, and the one that would have caught the desync as a *symptom*
// rather than as a missing call: a real compaction.Summarizer, an engine whose
// estimate has been corrected, and a conversation sitting in the band where the
// two gates disagree unless the correction is shared.
//
// The band is real, not contrived. The engine fires at 85% of the window and the
// summarizer at 80%, so uncorrected the engine is the stricter of the two and
// they never disagree — which is exactly why applying a correction to only one
// of them is an easy mistake to make and a quiet one to ship.
func TestCalibratedEngineAndRealSummarizerAgree(t *testing.T) {
	const window = 20_000
	const scale = 1.5

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	summarizer := compaction.New(compaction.Options{
		Adapter: summaryAdapter{}, Model: "test", ContextWindow: window, KeepRecent: 2,
	})
	eng, err := New(Options{Adapter: endTurnAdapter(), Tools: reg, Compactor: summarizer,
		Model: "test", MaxTokens: 100, ContextWindowTokens: window})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()

	// A conversation whose raw estimate lands between the summarizer's
	// uncorrected gate (80% of 20,000 = 16,000) and the engine's corrected one
	// (85% / 1.5 = ~11,333 raw). ~13,000 raw sits inside it.
	// Roles alternate because the summarizer only ever cuts before an assistant
	// message (it will not split a tool_use/tool_result pair). A transcript of
	// nothing but user turns has no legal boundary, so it reports "nothing to
	// compact" for a reason that has nothing to do with the gate under test.
	conv := &Conversation{System: "sys"}
	for i := 0; i < 10; i++ {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		conv.Append(provider.Message{Role: role, Content: []provider.Block{
			provider.TextBlock{Text: strings.Repeat("filler words here ", 290)},
		}})
	}
	raw := conv.estimatedTokens()
	engineGate := float64(window*85/100) / scale // raw tokens the engine fires at, once corrected
	summarizerGate := window * 80 / 100          // raw tokens the summarizer fires at, uncorrected
	if float64(raw) <= engineGate || raw >= summarizerGate {
		t.Fatalf("fixture conversation is %d raw tokens, outside the disagreement band "+
			"(%.0f..%d) — adjust the filler", raw, engineGate, summarizerGate)
	}

	// Teach the guard the backend's ratio, exactly as afterTurn would.
	g.calib.Observe(raw, g.requestOverhead, int(float64(raw+g.requestOverhead)*scale), window)
	if cc, ok := g.compactor.(CalibratedCompactor); ok {
		s, _ := g.calib.Scale()
		cc.SetEstimateCorrection(g.requestOverhead, s)
	} else {
		t.Fatal("compaction.Summarizer no longer implements CalibratedCompactor")
	}

	if est := g.estimate(conv); est <= compactionTrigger(window, 100) {
		t.Fatalf("corrected estimate %d did not cross the engine's trigger %d — fixture is not in the band",
			est, compactionTrigger(window, 100))
	}

	before := len(conv.Messages)
	var notices []string
	g.beforeTurn(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}, false)

	if len(conv.Messages) >= before {
		t.Errorf("the summarizer declined a compaction the engine asked for: %d messages before, %d after — "+
			"the two gates are pricing the same conversation differently", before, len(conv.Messages))
	}
	for _, n := range notices {
		if strings.Contains(n, "nothing left to compact") {
			t.Errorf("emitted the context-full notice instead of compacting: %s", n)
		}
	}
}
