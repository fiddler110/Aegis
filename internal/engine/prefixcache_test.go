package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
)

// P59.10 measurement harness.
//
// retractAll strips corrective scaffolding out of the *middle* of the
// conversation once a run settles. That is correct for the transcript, but it
// means the next run re-sends an edited history, and a local runner's KV prefix
// cache is invalid from the first changed token onward. The roadmap item
// assumed the damage was "bounded to the tail — nudges are appended late in a
// run". These tests measure whether that assumption holds, per nudge family.
//
// The measurement is deliberately split from the Ollama-side one. What a local
// runner does with a broken prefix was measured directly against Ollama 0.30.10
// / qwen3:14b on a ~5.5k-token conversation, and is a property of the runner,
// not of Aegis:
//
//	append-only continuation   94ms prefill   (1.0x — the P35.4 best case)
//	break 4 messages from end 283ms prefill   (3.0x)
//	break near the head       4109ms prefill  (43.5x — a full reprocess)
//
// prompt_eval_count stayed at the full prompt length in all three (5556/5283/
// 5290), confirming P35.13: duration, not count, is the cache-hit signal.
//
// So the runner-side cost is linear in "tokens after the break". What these
// tests supply is the other half — where Aegis puts the break.
//
// The answer turned out to be per-family, and the roadmap's assumption ("bounded
// to the tail") held for only one of the three. Replaying these exact
// conversations against Ollama (scratchpad replay: prefill of the *next* run's
// first turn, retracted vs not) measured:
//
//	guard corrective      85ms -> 57ms    (0.7x — free, as assumed)
//	tool-failure nudge    67ms -> 1745ms  (25.9x — mid-run break)
//	zero-tool nudge       71ms -> 3604ms  (51.0x — break at the run's start)
//
// P59.10 fixed the worst one by changing *when* the zero-tool nudge is
// retracted, not whether: retractSpentZeroTool strips it the moment the first
// tool round completes, which is the moment the engine already considers it
// spent. That returns it to 73ms (1.0x). The tool-failure nudge was left as-is
// at the time, for want of an observation that made it spent; P59.11 supplies
// one (the failure streak clearing) and retracts it there — see
// TestP5911ToolFailureNudgeRetractedOnRecovery.
//
// Set AEGIS_P5910_DUMP to a directory to re-emit the replay inputs.

// msgKey renders a message to a value comparable across a retraction, so two
// message lists can be compared for their longest common prefix the way a
// token-level prefix cache would compare them.
func msgKey(m provider.Message) string {
	var b strings.Builder
	b.WriteString(string(m.Role))
	for _, blk := range m.Content {
		switch v := blk.(type) {
		case provider.TextBlock:
			b.WriteString("|t:" + v.Text)
		case provider.ToolUseBlock:
			b.WriteString("|u:" + v.Name + string(v.Input))
		case provider.ToolResultBlock:
			b.WriteString("|r:" + v.Content)
		case provider.ThinkingBlock:
			b.WriteString("|k:" + v.Text)
		}
	}
	return b.String()
}

// prefixBreak reports the first index at which the next run's message list
// diverges from the list the model last saw — i.e. where the KV prefix cache
// stops being reusable — along with the token cost of everything the next run
// must therefore re-prefill.
type prefixBreak struct {
	breakIdx   int // first differing message index
	reprefill  int // tokens the next run must prefill (after[breakIdx:])
	baseline   int // tokens it would prefill with no retraction (the new answer alone)
	convTokens int // whole-conversation tokens, for scale
}

func measureBreak(lastReq, after []provider.Message) prefixBreak {
	idx := 0
	for idx < len(lastReq) && idx < len(after) && msgKey(lastReq[idx]) == msgKey(after[idx]) {
		idx++
	}
	pb := prefixBreak{breakIdx: idx, convTokens: tokenest.Messages("", after)}
	pb.reprefill = tokenest.Messages("", after[idx:])
	// With no retraction, `after` would be exactly lastReq plus the final
	// answer, so an append-only continuation re-prefills that answer alone.
	if len(after) > 0 {
		pb.baseline = tokenest.Message(after[len(after)-1])
	}
	return pb
}

func (p prefixBreak) report(t *testing.T, name string, total int) {
	t.Helper()
	ratio := float64(p.reprefill) / float64(max(p.baseline, 1))
	t.Logf("%s: break at message %d/%d — re-prefill %d tokens of a %d-token conversation "+
		"(append-only baseline %d tokens, %.1fx)",
		name, p.breakIdx, total, p.reprefill, p.convTokens, p.baseline, ratio)
}

// dumpForReplay writes the two message lists to JSON when AEGIS_P5910_DUMP
// names a directory, so the composed end-to-end cost can be replayed against a
// real Ollama (scratchpad/replay.py) rather than inferred from the two halves.
// Blocks are flattened to text: both variants flatten identically, and the
// measurement is about where the prefix diverges, not exact tokenization.
func dumpForReplay(t *testing.T, name string, lastReq, after []provider.Message) {
	t.Helper()
	dir := os.Getenv("AEGIS_P5910_DUMP")
	if dir == "" {
		return
	}
	flatten := func(msgs []provider.Message) []map[string]string {
		out := make([]map[string]string, 0, len(msgs))
		for _, m := range msgs {
			role := string(m.Role)
			if role == string(provider.RoleUser) {
				// A tool-results message is a user message carrying only
				// ToolResultBlocks; keep it a user turn, content is what matters.
				role = "user"
			}
			out = append(out, map[string]string{"role": role, "content": msgKey(m)})
		}
		return out
	}
	blob, err := json.MarshalIndent(map[string]any{
		"last_request": flatten(lastReq),
		"next_run":     flatten(after),
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal replay dump: %v", err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatalf("write replay dump: %v", err)
	}
	t.Logf("wrote replay dump %s", path)
}

func bigText(tag string, approxTokens int) string {
	unit := tag + " lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod "
	var b strings.Builder
	for b.Len() < approxTokens*4 {
		b.WriteString(unit)
	}
	return b.String()
}

// TestP5910ZeroToolNudgeRetractedWhileStillCheap measures the zero-tool nudge
// (P28.3) — the case that refuted the roadmap's "bounded to the tail"
// assumption. The nudge only fires *before any tool round has completed this
// run* (TestZeroToolNudgeSkippedAfterToolRound), so it is injected as early in
// the run as it is possible to be. Retracting it at run end therefore
// invalidated every token the run produced after it: the longer and more
// productive the run, the more expensive its own retraction. P59.10 retracts it
// as soon as it is spent instead, and this asserts the break stays at the tail.
func TestP5910ZeroToolNudgeRetractedWhileStillCheap(t *testing.T) {
	const toolRounds = 8

	turns := [][]provider.Event{
		// Turn 1: text-only on an actionable prompt — this is what earns the nudge.
		{
			{Type: provider.EventTextDelta, Text: "I would fix this by editing the file to add a nil check."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}
	// Turns 2..N: the nudge worked and the model does real, bulky work —
	// several file reads, each returning a realistic payload.
	for i := 0; i < toolRounds; i++ {
		turns = append(turns, []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID:   fmt.Sprintf("tu_%d", i),
				Name: "echo",
				Input: json.RawMessage(fmt.Sprintf(`{"msg":%q}`,
					bigText(fmt.Sprintf("round%d", i), 400))),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		})
	}
	turns = append(turns, []provider.Event{
		{Type: provider.EventTextDelta, Text: "Fixed the nil check."},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
	})

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	capture := &reqCapturingAdapter{inner: &scriptedAdapter{turns: turns}}
	eng, err := New(Options{Adapter: capture, Tools: reg, Model: "test", MaxIterations: 40})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil pointer bug in parser.go"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	lastReq := capture.reqs[len(capture.reqs)-1].Messages
	pb := measureBreak(lastReq, conv.Messages)
	pb.report(t, "zero-tool nudge", len(conv.Messages))
	dumpForReplay(t, "zerotool", lastReq, conv.Messages)

	// This is what P59.10 fixed. Before retractSpentZeroTool, the nudge was
	// retracted at run end, the break landed at message 1, and the next run
	// re-prefilled 6517 of 6526 tokens — measured against Ollama as 3604ms vs
	// 71ms unretracted (51x). Retracting the moment the nudge is spent leaves
	// only the run's first tool round downstream of the break, so the model's
	// final answer and every later round are a cache hit.
	if pb.breakIdx <= 1 {
		t.Errorf("break at message %d — the zero-tool nudge is being retracted at run "+
			"end again, which re-opens P59.10's 51x next-run prefill regression",
			pb.breakIdx)
	}
	if pb.reprefill*4 > pb.convTokens {
		t.Errorf("re-prefill %d tokens of a %d-token conversation — P59.10 expects the "+
			"break to leave only the first tool round downstream, not most of the run",
			pb.reprefill, pb.convTokens)
	}
}

// TestP5910ZeroToolNudgeStillRetractedFromTranscript guards the property
// P25.3/P28.3 care about, which the P59.10 timing change must not weaken: the
// scaffolding is gone from the settled conversation either way. Only *when* it
// is removed changed, never *whether*.
func TestP5910ZeroToolNudgeStillRetractedFromTranscript(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "I would fix this by editing the file."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"fixed"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		{
			{Type: provider.EventTextDelta, Text: "Fixed it."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil pointer bug in parser.go"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, m := range conv.Messages {
		for _, b := range m.Content {
			tb, ok := b.(provider.TextBlock)
			if !ok {
				continue
			}
			if strings.HasPrefix(tb.Text, zeroToolNudgePrefix) {
				t.Errorf("nudge scaffolding survived: %q", tb.Text)
			}
			if strings.Contains(tb.Text, "I would fix this by editing") {
				t.Errorf("the failed text-only turn survived: %q", tb.Text)
			}
		}
	}
	// Retraction rewrites already-persisted messages, so the caller must still be
	// told to fall back to a full re-save rather than an append.
	if conv.Persisted != -1 {
		t.Errorf("Persisted = %d after retraction, want -1 (full re-save)", conv.Persisted)
	}
}

// TestP5910GuardCorrectiveBreaksPrefixAtTail measures the guard corrective
// (P25.3) — the family that does behave the way the roadmap assumed. The guard
// runs only once a final answer exists, so its scaffolding is by construction
// the last thing in the conversation, and retracting it costs a tail re-prefill.
func TestP5910GuardCorrectiveBreaksPrefixAtTail(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: bigText("history-q", 600)}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{provider.TextBlock{Text: bigText("history-a", 600)}}},
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "summarise that"}}},
	}
	capture := &reqCapturingAdapter{inner: &scriptAdapter{replies: []string{"bad", "good"}}}
	eng, err := New(Options{
		Adapter: capture, Model: "m", OutputGuardMaxRetries: 2,
		OutputGuard: func(_ context.Context, in guard.Input) (bool, string, guard.Status) {
			if in.Text == "good" {
				return true, "", guard.StatusPassed
			}
			return false, "needs work", guard.StatusFailed
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: history}
	conv.Persisted = len(history)
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	lastReq := capture.reqs[len(capture.reqs)-1].Messages
	pb := measureBreak(lastReq, conv.Messages)
	pb.report(t, "guard corrective", len(conv.Messages))
	dumpForReplay(t, "guard", lastReq, conv.Messages)

	// The pre-existing history is upstream of the break and stays cached — this
	// is the "bounded to the tail" case, and it is the cheap one.
	if pb.breakIdx < len(history) {
		t.Errorf("break at message %d, before the end of the %d-message pre-run history — "+
			"the guard corrective should only ever invalidate this run's own tail",
			pb.breakIdx, len(history))
	}
}

// TestP5911ToolFailureNudgeRetractedOnRecovery measures the tool-failure nudge
// (P52.3), the family that sat between the two extremes: it fires when a tool
// round fails, so it lands wherever in the run that happened, and retracting it
// at run end invalidated everything after it — 25.9x on the replay above.
//
// P59.10 left it there deliberately: unlike the zero-tool nudge, nothing made it
// *spent*. It was only bounded to one per run, so retracting it early would have
// removed the correction while the failures could still recur. P59.11 supplies
// the missing observation instead of assuming it — the streak actually clearing
// (toolFailureTracker.cleared) — and pairs the early retraction with
// re-injectability, so a recurrence gets a fresh nudge by append. This asserts
// the break now lands at the recovery window rather than the whole run tail;
// TestP5911ToolFailureNudgeReinjectedAfterRecovery holds the other half.
func TestP5911ToolFailureNudgeRetractedOnRecovery(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&alwaysFailTool{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	// toolFailureNudgeThreshold consecutive failing rounds early in the run earn
	// the nudge; the run then recovers and does bulky work downstream of it.
	var turns [][]provider.Event
	for i := 0; i < toolFailureNudgeThreshold; i++ {
		turns = append(turns, []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: fmt.Sprintf("tu_fail_%d", i), Name: "boom", Input: json.RawMessage(`{}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		})
	}
	for i := 0; i < 4; i++ {
		turns = append(turns, []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID:   fmt.Sprintf("tu_ok_%d", i),
				Name: "echo",
				Input: json.RawMessage(fmt.Sprintf(`{"msg":%q}`,
					bigText(fmt.Sprintf("after%d", i), 400))),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		})
	}
	turns = append(turns, []provider.Event{
		{Type: provider.EventTextDelta, Text: "Recovered and finished."},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
	})

	capture := &reqCapturingAdapter{inner: &scriptedAdapter{turns: turns}}
	eng, err := New(Options{Adapter: capture, Tools: reg, Model: "test", MaxIterations: 40})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "run the build and fix whatever breaks"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	lastReq := capture.reqs[len(capture.reqs)-1].Messages
	pb := measureBreak(lastReq, conv.Messages)
	pb.report(t, "tool-failure nudge", len(conv.Messages))
	dumpForReplay(t, "toolfailure", lastReq, conv.Messages)

	// The nudge is injected after the third failing round and retracted after the
	// first clean one — inside the run, so the model's later requests already see
	// the edited history and the *next* run diverges only at the final answer.
	// The in-run cost is one re-prefill of the recovery round; before P59.11 the
	// break landed at the nudge with the four bulky rounds and the final answer
	// downstream of it, which measured 25.9x against Ollama.
	if pb.reprefill*4 > pb.convTokens {
		t.Errorf("re-prefill %d tokens of a %d-token conversation — the tool-failure nudge "+
			"is being retracted at run end again, re-opening P59.10's measured 25.9x "+
			"next-run prefill cost", pb.reprefill, pb.convTokens)
	}
}

// TestP5911ToolFailureNudgeReinjectedAfterRecovery holds the property that makes
// the early retraction safe. P59.10 declined to retract this nudge early
// precisely because it is a correction that can still be needed; P59.11 removes
// it only on an observed recovery and lets a *new* failure streak earn a new
// one. Without this the run would recover, lose the corrective, relapse, and
// have nothing to say about it — trading the prefill cost for exactly the
// behavioral regression P59.10 refused to risk.
func TestP5911ToolFailureNudgeReinjectedAfterRecovery(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&alwaysFailTool{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	failRound := func(tag string) []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_fail_" + tag, Name: "boom", Input: json.RawMessage(`{}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		}
	}
	okRound := func(tag string) []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_ok_" + tag, Name: "echo", Input: json.RawMessage(`{"msg":"progress"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		}
	}

	// Streak, recovery, relapse: each streak is threshold-length, so each earns
	// its own nudge, and the clean round between them retracts the first.
	var turns [][]provider.Event
	for i := 0; i < toolFailureNudgeThreshold; i++ {
		turns = append(turns, failRound(fmt.Sprintf("a%d", i)))
	}
	turns = append(turns, okRound("recover"))
	for i := 0; i < toolFailureNudgeThreshold; i++ {
		turns = append(turns, failRound(fmt.Sprintf("b%d", i)))
	}
	turns = append(turns, []provider.Event{
		{Type: provider.EventTextDelta, Text: "I can't get past this failure."},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
	})

	capture := &reqCapturingAdapter{inner: &scriptedAdapter{turns: turns}}
	// LoopThreshold off: the repeated identical failing call is the loop
	// detector's signal too, and this test is about the failure breaker alone.
	eng, err := New(Options{Adapter: capture, Tools: reg, Model: "test",
		MaxIterations: 40, LoopThreshold: -1})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "run the build and fix whatever breaks"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Per request: never two at once (the outstanding gate replaces the old
	// one-per-run bound), and the relapse must have been corrected again.
	sawNudge := 0
	for _, req := range capture.reqs {
		n := countNudges(req.Messages, toolFailureNudgePrefix)
		if n > 1 {
			t.Fatalf("%d tool-failure nudges live in one request — the outstanding gate "+
				"must never let a second be injected while the first is in place", n)
		}
		if n == 1 {
			sawNudge++
		}
	}
	if sawNudge == 0 {
		t.Fatal("no request ever carried the tool-failure nudge")
	}

	// The last request is the relapse's: the second nudge must be present, which
	// is only possible if the first was retracted and re-injection is allowed.
	last := capture.reqs[len(capture.reqs)-1].Messages
	if n := countNudges(last, toolFailureNudgePrefix); n != 1 {
		t.Errorf("final request carried %d tool-failure nudges, want 1 — the corrective "+
			"was retracted on recovery and never restored when failures resumed", n)
	}

	// And retraction is still total once the run settles (P25.3).
	if n := countNudges(conv.Messages, toolFailureNudgePrefix); n != 0 {
		t.Errorf("tool-failure nudge survived into the durable transcript (%d occurrences)", n)
	}
}

// TestP5911ToolFailureNudgeSurvivesAnOngoingStreak is the converse guard: while
// the failures continue, the corrective must stay in front of the model. Only a
// clean round makes it spent, so a run that fails from the nudge onward keeps it
// until retractAll — the 25.9x case still exists, and is now the case where
// paying it is the only correct behavior.
func TestP5911ToolFailureNudgeSurvivesAnOngoingStreak(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&alwaysFailTool{}); err != nil {
		t.Fatal(err)
	}

	var turns [][]provider.Event
	for i := 0; i < toolFailureAbortThreshold-1; i++ {
		turns = append(turns, []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: fmt.Sprintf("tu_%d", i), Name: "boom", Input: json.RawMessage(`{}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		})
	}
	turns = append(turns, []provider.Event{
		{Type: provider.EventTextDelta, Text: "Giving up."},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
	})

	capture := &reqCapturingAdapter{inner: &scriptedAdapter{turns: turns}}
	eng, err := New(Options{Adapter: capture, Tools: reg, Model: "test",
		MaxIterations: 40, LoopThreshold: -1})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "run the build and fix whatever breaks"},
	}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	last := capture.reqs[len(capture.reqs)-1].Messages
	if n := countNudges(last, toolFailureNudgePrefix); n != 1 {
		t.Errorf("final request carried %d tool-failure nudges, want 1 — the corrective "+
			"must stay in context for as long as the failures it corrects continue", n)
	}
}

// alwaysFailTool is a registrable tool whose every call errors, for exercising
// the P52.3 tool-failure nudge.
type alwaysFailTool struct{}

func (a *alwaysFailTool) Name() string                 { return "boom" }
func (a *alwaysFailTool) Description() string          { return "always fails" }
func (a *alwaysFailTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (a *alwaysFailTool) Capability() tool.Capability  { return tool.CapRead }
func (a *alwaysFailTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, fmt.Errorf("boom: simulated tool failure")
}
