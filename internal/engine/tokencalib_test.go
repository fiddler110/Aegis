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
				// Native-Ollama telemetry, kept because a real Ollama turn
				// carries it — but no longer what admits the sample. Since
				// P66.14/LLM-03 that is Options.SharedContextWindow, a positive
				// identification of the backend rather than one adapter's
				// telemetry; TestCalibrationIgnores... below pins it.
				PromptEvalDurationMS: 40,
			}
		}
		ch <- ev
	}
	close(ch)
	return ch, nil
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
		MaxTokens: 100, ContextWindowTokens: 1_000_000, SharedContextWindow: true})
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
	g.calib.Observe(sent.estimatedTokens(), g.overhead(), adapter.lastReported, 1_000_000)

	got := g.estimate(sent)
	if diff := got - adapter.lastReported; diff < -2 || diff > 2 {
		t.Errorf("calibrated estimate = %d, backend reported %d — the correction did not converge",
			got, adapter.lastReported)
	}
	// And the uncorrected estimate must genuinely have been wrong, or the test
	// proves nothing about the correction.
	if raw := sent.estimatedTokens() + g.overhead(); raw >= adapter.lastReported {
		t.Errorf("raw estimate %d was not below the reported %d — the fixture is not exercising an undercount",
			raw, adapter.lastReported)
	}
}

// TestCalibrationAdmissibility pins what makes a turn a calibration sample.
//
// The gate used to be `PromptEvalDurationMS > 0` — a *telemetry* field only the
// native Ollama adapter populates — which meant the correction was inert on the
// OpenAI-compat path, i.e. on the `provider.default: openai` + `:11434/v1`
// configuration docs/providers.md recommends (P66.14/LLM-03). The subject of the
// gate is the backend, so the test's subject is the identification: an
// unidentified backend is not evidence however rich its usage block, and an
// identified one is evidence with no prefill duration at all — which is exactly
// the compat-path turn that used to be discarded.
func TestCalibrationAdmissibility(t *testing.T) {
	guardFor := func(t *testing.T, shared bool) *compactionGuard {
		t.Helper()
		eng, err := New(Options{Adapter: endTurnAdapter(), Tools: tool.NewRegistry(), Model: "test",
			MaxTokens: 100, ContextWindowTokens: 10_000, SharedContextWindow: shared})
		if err != nil {
			t.Fatal(err)
		}
		return eng.newCompactionGuard()
	}

	for _, tc := range []struct {
		name   string
		shared bool
		usage  provider.Usage
		want   int // expected sample count
	}{
		// The LLM-03 regression, stated as the case that must now be learned
		// from: an identified backend reporting a plain prompt count and no
		// native telemetry. This is the compat path.
		{"identified backend without prefill telemetry", true,
			provider.Usage{InputTokens: 5000}, 1},
		{"identified backend with native telemetry", true,
			provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10}, 1},
		// And the cases that must still be refused. The first is the one the old
		// gate got right by accident: a cloud provider is not identified.
		{"unidentified backend", false,
			provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10}, 0},
		{"estimated usage", true,
			provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10, IsEstimated: true}, 0},
		{"cache accounting present", true,
			provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10, CacheReadTokens: 400}, 0},
		{"cache creation present", true,
			provider.Usage{InputTokens: 5000, PromptEvalDurationMS: 10, CacheCreationTokens: 400}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := guardFor(t, tc.shared)
			g.lastRaw = 1000
			u := tc.usage
			g.afterTurn(&u, 10_000)
			if _, samples := g.calib.Scale(); samples != tc.want {
				t.Errorf("samples = %d, want %d", samples, tc.want)
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
		MaxTokens: 100, ContextWindowTokens: 10_000, SharedContextWindow: true})
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

// recordingBudgetedCompactor is a Compactor that also records the token budgets
// decorated onto the calls it receives.
//
// It records at WithTokenBudget rather than inside Compact deliberately: the
// engine's half of the seam is the decoration, and a double that only inspected
// the context inside Compact could not tell a budget that was never pushed from
// one pushed onto a context the compaction never used.
type recordingBudgetedCompactor struct {
	noticeCompactor
	overheads []int
	scales    []float64
	triggers  []int
}

func (r *recordingBudgetedCompactor) WithTokenBudget(ctx context.Context, overhead int, scale float64, trigger int) context.Context {
	r.overheads = append(r.overheads, overhead)
	r.scales = append(r.scales, scale)
	r.triggers = append(r.triggers, trigger)
	return compaction.WithTokenBudget(ctx, overhead, scale, trigger)
}

// TestCalibrationReachesTheCompactor: the engine and the Compactor run two gates
// over the same messages, so a correction the engine keeps to itself re-creates
// P41.1 — the engine asks for compaction the summarizer then declines, and the
// engine reads that as "nothing left to compact". Since P66.14 the trigger rides
// the same seam, for the same reason.
func TestCalibrationReachesTheCompactor(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &recordingBudgetedCompactor{}
	// Two model turns, because the budget travels *into* a call: the first turn's
	// beforeTurn runs before anything has been learned, and the correction reaches
	// the compactor on the next call rather than being stored on it. That is the
	// same turn's-worth of delay the setter had — afterTurn pushed after the turn
	// it learned from, so a compaction could only ever read it on a later one —
	// but it has to be driven to be observed.
	adapter := &denseBackendAdapter{ratio: 1.5, turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		endTurn(),
	}}
	// The window and the fixture size are chosen together so the run actually
	// reaches a compaction on its second turn, which is what makes the push
	// observable. The budget rides the *call*, so a run that never compacts
	// pushes nothing — that is the seam working, not a gap to assert around.
	//
	// Sized as: the corrected estimate (1.5 x raw) must land above the trigger and
	// below the window, or the calibrator discards the sample as a truncated
	// prompt. At a 6,000-token window the trigger is 5,100, so ~3,600 raw tokens
	// reports ~5,400 and sits in that band.
	const window = 6_000
	eng, err := New(Options{Adapter: adapter, Tools: reg, Compactor: comp, Model: "test",
		MaxTokens: 100, ContextWindowTokens: window, SharedContextWindow: true})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{System: "sys"}
	for i := 0; i < 20; i++ {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		conv.Append(provider.Message{Role: role, Content: []provider.Block{
			provider.TextBlock{Text: strings.Repeat("filler words here ", 40)},
		}})
	}
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(comp.scales) < 2 {
		t.Fatalf("only %d budget push(es) — the fixture never reached a second compaction, "+
			"so nothing here observes a *learned* correction", len(comp.scales))
	}

	if len(comp.scales) == 0 {
		t.Fatal("no token budget was pushed to the compactor")
	}
	if got := comp.scales[len(comp.scales)-1]; got < 1.4 || got > 1.6 {
		t.Errorf("pushed scale = %v, want ~1.5 (the backend's ratio)", got)
	}
	if got := comp.overheads[len(comp.overheads)-1]; got != tokenest.Tools(reg.Schemas()) {
		t.Errorf("pushed overhead = %d, want %d (the tool schemas)", got, tokenest.Tools(reg.Schemas()))
	}
	// The trigger is the P66.14 half: whatever number the engine gated on is the
	// number the compactor is told to gate on, so the two cannot drift.
	if got, want := comp.triggers[len(comp.triggers)-1], compactionTrigger(window, 100); got != want {
		t.Errorf("pushed trigger = %d, want %d (the engine's own)", got, want)
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
// The band is real, not contrived. Before P66.14 the engine fired at its
// completion-sized trigger and the summarizer at a flat 80% of the window, so
// uncorrected the engine was the stricter of the two and they never disagreed —
// which is exactly why applying a correction to only one of them is an easy
// mistake to make and a quiet one to ship.
//
// Since P66.14 there are two ways to re-open the gap and this fixture catches
// both: withholding the correction (P62.4), or letting the summarizer compute a
// threshold of its own instead of taking the engine's (LLM-02). The fixture is
// sized against the *old* flat rule on purpose, because that is the behaviour a
// regression would revert to.
func TestCalibratedEngineAndRealSummarizerAgree(t *testing.T) {
	const (
		window    = 20_000
		maxTokens = 100
		scale     = 1.5
	)

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	summarizer := compaction.New(compaction.Options{
		Adapter: summaryAdapter{}, Model: "test", ContextWindow: window, KeepRecent: 2,
		MaxTokens: maxTokens,
	})
	eng, err := New(Options{Adapter: endTurnAdapter(), Tools: reg, Compactor: summarizer,
		Model: "test", MaxTokens: maxTokens, ContextWindowTokens: window,
		SharedContextWindow: true})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()

	// A conversation whose raw estimate lands between the summarizer's
	// pre-P66.14 gate (a flat 80% of 20,000 = 16,000) and the engine's corrected
	// one (the shared trigger / 1.5 = ~11,333 raw). ~13,000 raw sits inside it.
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
	engineGate := float64(compactionTrigger(window, maxTokens)) / scale // raw tokens the engine fires at, once corrected
	summarizerGate := window * 80 / 100                                // raw tokens the pre-P66.14 flat rule fired at
	if float64(raw) <= engineGate || raw >= summarizerGate {
		t.Fatalf("fixture conversation is %d raw tokens, outside the disagreement band "+
			"(%.0f..%d) — adjust the filler", raw, engineGate, summarizerGate)
	}

	if _, ok := g.compactor.(BudgetedCompactor); !ok {
		t.Fatal("compaction.Summarizer no longer implements BudgetedCompactor — the engine's " +
			"correction and trigger cannot reach it, so the two gates are free to disagree")
	}
	// Teach the guard the backend's ratio, exactly as afterTurn would. Nothing is
	// pushed to the compactor here: beforeTurn decorates the call below, which is
	// the whole point of the seam being per-call.
	g.calib.Observe(raw, g.overhead(), int(float64(raw+g.overhead())*scale), window)

	if est := g.estimate(conv); est <= compactionTrigger(window, maxTokens) {
		t.Fatalf("corrected estimate %d did not cross the engine's trigger %d — fixture is not in the band",
			est, compactionTrigger(window, maxTokens))
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
