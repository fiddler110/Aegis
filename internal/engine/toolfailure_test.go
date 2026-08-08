package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// failEditTool models the P52.3 failure shape: an edit tool that always reports
// "old_string not found", the single most common local-model stall. When
// echoArgs is false the error text is byte-identical every call regardless of
// the arguments — the same-error-different-args case that loopDetector cannot
// see, because the *arguments* are what vary.
type failEditTool struct {
	called   int
	echoArgs bool
}

func (f *failEditTool) Name() string                 { return "fail_edit" }
func (f *failEditTool) Description() string          { return "an edit tool that always fails" }
func (f *failEditTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *failEditTool) Capability() tool.Capability  { return tool.CapRead }
func (f *failEditTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	f.called++
	msg := "old_string not found in parser.go"
	if f.echoArgs {
		msg = fmt.Sprintf("old_string not found in parser.go (tried %s)", string(input))
	}
	return tool.Result{Content: msg, IsError: true}, nil
}

// failTurn scripts one model turn that calls fail_edit with the given arguments.
func failTurn(id, args string) []provider.Event {
	return []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: id, Name: "fail_edit", Input: json.RawMessage(args),
		}},
		{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
	}
}

// okTurn scripts one model turn that calls the always-succeeding echo tool.
func okTurn(id string) []provider.Event {
	return []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: id, Name: "echo", Input: json.RawMessage(`{"msg":"progress"}`),
		}},
		{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
	}
}

func textTurn(text string) []provider.Event {
	return []provider.Event{
		{Type: provider.EventTextDelta, Text: text},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
	}
}

// countNudges reports how many messages in the slice open with prefix.
func countNudges(msgs []provider.Message, prefix string) int {
	n := 0
	for _, m := range msgs {
		if isNudge(m, prefix) {
			n++
		}
	}
	return n
}

// TestToolFailureNudgeAfterThreeAllErrorRounds is the P52.3 regression: three
// consecutive tool rounds in which every result was an error must inject the
// corrective nudge exactly once (not once per subsequent failing round), and
// the nudge must be retracted from the durable transcript once the run settles,
// like every other corrective.
func TestToolFailureNudgeAfterThreeAllErrorRounds(t *testing.T) {
	// Five failing rounds, then a final text answer: rounds 3, 4 and 5 all
	// satisfy the nudge condition, but only one nudge may ever be injected.
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		failTurn("t1", `{"old":"a"}`),
		failTurn("t2", `{"old":"b"}`),
		failTurn("t3", `{"old":"c"}`),
		failTurn("t4", `{"old":"d"}`),
		failTurn("t5", `{"old":"e"}`),
		textTurn("I could not apply the edit."),
	}}

	reg := tool.NewRegistry()
	ft := &failEditTool{}
	if err := reg.Register(ft); err != nil {
		t.Fatal(err)
	}

	capture := &reqCapturingAdapter{inner: adapter}
	// LoopThreshold is disabled so this test observes the failure breaker alone.
	eng, err := New(Options{Adapter: capture, Tools: reg, Model: "test", LoopThreshold: -1})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil check in parser.go"},
	}})

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if adapter.calls != 6 {
		t.Fatalf("expected 6 model calls (5 failing rounds + final answer), got %d", adapter.calls)
	}
	if len(notices) != 1 {
		t.Errorf("expected exactly 1 KindNotice for the tool-failure nudge, got %d: %v", len(notices), notices)
	} else if !strings.Contains(notices[0], "fail_edit") || !strings.Contains(notices[0], "old_string not found") {
		t.Errorf("notice should name the tool and the error, got %q", notices[0])
	}

	// The nudge must have reached the model exactly once, quoting the real error.
	last := capture.reqs[len(capture.reqs)-1]
	if n := countNudges(last.Messages, toolFailureNudgePrefix); n != 1 {
		t.Fatalf("expected exactly 1 tool-failure nudge in the final request, got %d", n)
	}
	var nudgeText string
	for _, m := range last.Messages {
		if isNudge(m, toolFailureNudgePrefix) {
			nudgeText = m.Content[0].(provider.TextBlock).Text
		}
	}
	if !strings.Contains(nudgeText, "old_string not found in parser.go") {
		t.Errorf("nudge must quote the actual error text, got %q", nudgeText)
	}
	if !strings.Contains(nudgeText, "fail_edit") {
		t.Errorf("nudge must name the failing tool, got %q", nudgeText)
	}

	// ... and must not survive into the durable transcript.
	if n := countNudges(conv.Messages, toolFailureNudgePrefix); n != 0 {
		t.Errorf("tool-failure nudge survived retraction (%d occurrences)", n)
	}
	if final := assistantText(conv.Messages[len(conv.Messages)-1]); final != "I could not apply the edit." {
		t.Errorf("final message = %q, want the model's answer", final)
	}
	if ft.called != 5 {
		t.Errorf("fail_edit ran %d times, want 5", ft.called)
	}
}

// TestToolFailureAbortsAfterSixAllErrorRounds covers the second threshold: a
// run where nothing ever succeeds must end with a clear error naming the tool
// and the repeated error text, rather than burning to maxIterations (40 turns
// on a ~7 tok/s local model is potentially hours).
func TestToolFailureAbortsAfterSixAllErrorRounds(t *testing.T) {
	turns := make([][]provider.Event, 0, 8)
	for i := 0; i < 8; i++ {
		turns = append(turns, failTurn(fmt.Sprintf("t%d", i), fmt.Sprintf(`{"old":"v%d"}`, i)))
	}
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	if err := reg.Register(&failEditTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", LoopThreshold: -1})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil check in parser.go"},
	}})

	var gotErr error
	err = eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	})
	if err == nil {
		t.Fatal("expected the run to abort")
	}
	if gotErr == nil {
		t.Fatal("expected a KindError event carrying the abort error")
	}
	if gotErr.Error() != err.Error() {
		t.Errorf("emitted error %q differs from returned error %q", gotErr, err)
	}
	for _, want := range []string{"consecutive failing tool rounds", "fail_edit", "old_string not found in parser.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("abort error %q must mention %q", err, want)
		}
	}
	if adapter.calls != toolFailureAbortThreshold {
		t.Errorf("expected abort after %d model calls, made %d", toolFailureAbortThreshold, adapter.calls)
	}
}

// TestToolFailureCounterResetsOnSuccess verifies the counters are strictly
// about *consecutive* failure: a single successful tool round in the middle
// clears them, so an intermittently-failing but progressing run is never
// nudged or aborted.
func TestToolFailureCounterResetsOnSuccess(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		failTurn("t1", `{"old":"a"}`),
		failTurn("t2", `{"old":"b"}`),
		okTurn("t3"),
		failTurn("t4", `{"old":"c"}`),
		failTurn("t5", `{"old":"d"}`),
		textTurn("done"),
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&failEditTool{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	capture := &reqCapturingAdapter{inner: adapter}
	eng, err := New(Options{Adapter: capture, Tools: reg, Model: "test", LoopThreshold: -1})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil check in parser.go"},
	}})

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != 6 {
		t.Fatalf("expected all 6 scripted turns to run, got %d", adapter.calls)
	}
	if len(notices) != 0 {
		t.Errorf("expected no tool-failure notice (4 failures, never 3 in a row), got %v", notices)
	}
	for _, req := range capture.reqs {
		if n := countNudges(req.Messages, toolFailureNudgePrefix); n != 0 {
			t.Fatalf("tool-failure nudge fired despite a successful round in between")
		}
	}
}

// TestToolFailureCatchesSameErrorDifferentArgs is the structural point of
// P52.3: the arguments legitimately differ every round — the model retries
// edit_file with a slightly different old_string after each "not found" — so
// every turnSignature is distinct and loopDetector never fires, even at its
// most aggressive threshold. The failure breaker must catch it anyway.
func TestToolFailureCatchesSameErrorDifferentArgs(t *testing.T) {
	args := []string{
		`{"old":"func parse(s string) {"}`,
		`{"old":"func parse(s string) error {"}`,
		`{"old":"func  parse(s string) {"}`,
		`{"old":"func parse(s string){"}`,
		`{"old":"func parse (s string) {"}`,
		`{"old":"func parse(s  string) {"}`,
	}

	// Precondition: these calls are genuinely distinct to the loop detector, so
	// this test really does exercise the gap rather than the loop guard.
	loop := newLoopDetector(3)
	for i, a := range args {
		sig := turnSignature([]provider.ToolUseBlock{{Name: "fail_edit", Input: json.RawMessage(a)}})
		if loop.record(sig) {
			t.Fatalf("loopDetector unexpectedly fired at round %d — the fixture no longer models the gap", i+1)
		}
	}

	turns := make([][]provider.Event, 0, len(args))
	for i, a := range args {
		turns = append(turns, failTurn(fmt.Sprintf("t%d", i), a))
	}
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	// echoArgs: the error text also varies with the arguments, so even the
	// same-error counter cannot carry this — only the all-error round counter can.
	if err := reg.Register(&failEditTool{echoArgs: true}); err != nil {
		t.Fatal(err)
	}
	// Loop detection left at its default (threshold 5) to prove it stays silent.
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the parse signature in parser.go"},
	}})

	var notices []string
	err = eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err == nil {
		t.Fatal("expected the run to abort on repeated tool failures")
	}
	if strings.Contains(err.Error(), "suspected loop") {
		t.Fatalf("loop detector fired; this test must exercise the failure breaker: %v", err)
	}
	if !strings.Contains(err.Error(), "consecutive failing tool rounds") {
		t.Errorf("expected the tool-failure abort, got %v", err)
	}
	if len(notices) != 1 {
		t.Errorf("expected the single corrective nudge before the abort, got %v", notices)
	}
}

// TestToolFailureNudgesOnRepeatedErrorWithPartialSuccess covers the secondary
// counter: a round that mixes a failing call with a succeeding one never trips
// the strict all-error counter, but the same error text recurring round after
// round is still a stall worth naming. It earns a nudge and, deliberately,
// never an abort — a run that is still landing successful calls is making
// progress somewhere, and killing it would be worse than the stall.
func TestToolFailureNudgesOnRepeatedErrorWithPartialSuccess(t *testing.T) {
	mixedTurn := func(id string) []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: id + "a", Name: "fail_edit", Input: json.RawMessage(`{"old":"` + id + `"}`),
			}},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: id + "b", Name: "echo", Input: json.RawMessage(`{"msg":"still here"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		}
	}
	turns := make([][]provider.Event, 0, 9)
	for i := 0; i < 8; i++ {
		turns = append(turns, mixedTurn(fmt.Sprintf("t%d", i)))
	}
	turns = append(turns, textTurn("gave up on the edit"))
	adapter := &scriptedAdapter{turns: turns}

	reg := tool.NewRegistry()
	if err := reg.Register(&failEditTool{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", LoopThreshold: -1})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "fix the nil check in parser.go"},
	}})

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run must not abort while calls are still succeeding: %v", err)
	}
	if len(notices) != 1 {
		t.Errorf("expected exactly 1 tool-failure notice, got %d: %v", len(notices), notices)
	}
	if adapter.calls != 9 {
		t.Errorf("expected all 9 scripted turns to run, got %d", adapter.calls)
	}
}

func TestToolFailureTrackerRecord(t *testing.T) {
	fail := func(content string) []provider.Block {
		return []provider.Block{provider.ToolResultBlock{ToolUseID: "x", Content: content, IsError: true}}
	}
	uses := []provider.ToolUseBlock{{ID: "x", Name: "fail_edit"}}

	var tr toolFailureTracker
	tr.record(uses, fail("not found"))
	tr.record(uses, fail("not found\n"))
	if tr.allErrorRounds != 2 || tr.sameErrorRounds != 2 {
		t.Errorf("whitespace-only variation must compare equal: all=%d same=%d", tr.allErrorRounds, tr.sameErrorRounds)
	}
	if tr.shouldNudge() {
		t.Error("two rounds must not reach the nudge threshold")
	}
	tr.record(uses, fail("a different error"))
	if tr.allErrorRounds != 3 {
		t.Errorf("all-error rounds = %d, want 3", tr.allErrorRounds)
	}
	if tr.sameErrorRounds != 1 {
		t.Errorf("a different error must restart the same-error counter, got %d", tr.sameErrorRounds)
	}
	if !tr.shouldNudge() || tr.shouldAbort() {
		t.Error("three all-error rounds must nudge but not abort")
	}

	// A clean round wipes everything.
	tr.record(uses, []provider.Block{provider.ToolResultBlock{ToolUseID: "x", Content: "ok"}})
	if tr.allErrorRounds != 0 || tr.sameErrorRounds != 0 || tr.lastErrorKey != "" {
		t.Errorf("a successful round must reset the tracker, got %+v", tr)
	}

	for i := 0; i < toolFailureAbortThreshold; i++ {
		tr.record(uses, fail("not found"))
	}
	if !tr.shouldAbort() {
		t.Errorf("%d all-error rounds must abort", toolFailureAbortThreshold)
	}
	if got := tr.abortError().Error(); !strings.Contains(got, "fail_edit") || !strings.Contains(got, "not found") {
		t.Errorf("abort error must name the tool and error, got %q", got)
	}
}

func TestNormalizeAndTruncateToolError(t *testing.T) {
	if got := normalizeToolError("  old_string\n  not\tfound  "); got != "old_string not found" {
		t.Errorf("normalizeToolError = %q", got)
	}
	long := strings.Repeat("é", toolFailureMaxQuoted+50)
	got := truncateToolError(long)
	if r := []rune(got); len(r) != toolFailureMaxQuoted+1 || r[len(r)-1] != '…' {
		t.Errorf("truncateToolError produced %d runes, want %d plus an ellipsis", len(r), toolFailureMaxQuoted)
	}
	if !strings.HasPrefix(got, "é") {
		t.Error("truncation must not split a multi-byte rune")
	}
}

// nudgeEatingCompactor stands in for what real compaction does to an old
// corrective: internal/compaction summarizes everything ahead of its
// keep-recent tail, so a nudge that has scrolled out of that tail is deleted
// outright. This drops exactly the tool-failure nudge and leaves the rest of
// the conversation alone, so the test isolates the deletion from every other
// effect a summarization pass would have.
type nudgeEatingCompactor struct{ eaten int }

func (c *nudgeEatingCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	kept := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		if isNudge(m, toolFailureNudgePrefix) {
			c.eaten++
			continue
		}
		kept = append(kept, m)
	}
	return kept, len(kept) != len(msgs), nil
}

// TestP6312ToolFailureNudgeReinjectedAfterCompactionDeletesIt is the P63.12
// regression. The P52.3 corrective is the one piece of scaffolding whose whole
// purpose is to be *visible* to the model, and re-injection used to be gated on
// a bool set when it was injected. Compaction can delete the message after
// that, and the bool went on claiming it was present — so the run lost its
// correction with no notice, no log line, and a counter that still looked
// healthy.
//
// The `sameErrorRounds` path makes this reachable rather than theoretical: it
// nudges at three rounds but never aborts (only `allErrorRounds` does, at six),
// so such a run continues to the iteration cap, far past compaction's
// keep-recent tail.
//
// Asking the conversation instead of the flag, a deleted corrective is
// re-injected — an append, which costs a local runner's prefix cache nothing.
func TestP6312ToolFailureNudgeReinjectedAfterCompactionDeletesIt(t *testing.T) {
	var turns [][]provider.Event
	for i := 0; i < 5; i++ {
		turns = append(turns, failTurn(fmt.Sprintf("tu-%d", i), fmt.Sprintf(`{"old_string":"v%d"}`, i)))
	}
	turns = append(turns, textTurn("giving up"))

	reg := tool.NewRegistry()
	if err := reg.Register(&failEditTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &nudgeEatingCompactor{}
	eng, err := New(Options{
		Adapter: &scriptedAdapter{turns: turns}, Tools: reg, Compactor: comp,
		Model: "test", MaxTokens: 100, ContextWindowTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	var nudgeNotices int
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "in a row failed") {
			nudgeNotices++
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if comp.eaten == 0 {
		t.Fatal("the compactor never deleted a nudge — the fixture no longer reproduces the condition")
	}
	if nudgeNotices < 2 {
		t.Errorf("tool-failure nudges injected = %d, want >= 2: compaction deleted the first one and the "+
			"corrective must be re-sent, not suppressed by a flag that outlived the message", nudgeNotices)
	}
	if nudgeNotices > toolFailureNudgeMax {
		t.Errorf("tool-failure nudges injected = %d, want <= toolFailureNudgeMax (%d)", nudgeNotices, toolFailureNudgeMax)
	}
}
