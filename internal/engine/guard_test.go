package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// scriptAdapter returns one text response per Stream call, in order.
type scriptAdapter struct {
	replies []string
	i       int
}

func (a *scriptAdapter) Name() string { return "script" }
func (a *scriptAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	text := "done"
	if a.i < len(a.replies) {
		text = a.replies[a.i]
	}
	a.i++
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}}
	close(ch)
	return ch, nil
}

// reqCapturingAdapter wraps another adapter and snapshots every request it
// receives. Needed since P25.3: retractGuardCorrectives strips the corrective
// prompt (and the failed answer it scolded) from the conversation once the run
// settles, so tests asserting on the corrective's wording must observe the
// request the model actually saw mid-run, not conv.Messages afterwards.
type reqCapturingAdapter struct {
	inner provider.Adapter
	reqs  []provider.Request
}

func (a *reqCapturingAdapter) Name() string { return a.inner.Name() }
func (a *reqCapturingAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.reqs = append(a.reqs, req)
	return a.inner.Stream(ctx, req)
}

// capturedCorrective returns the text of the first guard corrective message
// seen in any captured request, or "".
func (a *reqCapturingAdapter) capturedCorrective() string {
	for _, req := range a.reqs {
		for _, m := range req.Messages {
			if m.Role != provider.RoleUser {
				continue
			}
			for _, b := range m.Content {
				if tb, ok := b.(provider.TextBlock); ok && strings.Contains(tb.Text, "did not pass output validation") {
					return tb.Text
				}
			}
		}
	}
	return ""
}

func runWith(t *testing.T, opts Options) []Event {
	t.Helper()
	eng, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	var got []Event
	if err := eng.Run(context.Background(), conv, func(ev Event) { got = append(got, ev) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	return got
}

func kinds(evs []Event) []EventKind {
	var k []EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

// TestGuardPassEmitsGuardEvent is the FIND-16 regression: a genuine pass must
// now be observable, not silent. Before this fix, nothing was emitted on the
// success path at all, so a real PASS and a fail-open skip (guard never
// actually ran) were both invisible — and therefore indistinguishable — to
// any consumer of the event stream.
func TestGuardPassEmitsGuardEvent(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"final"}}, Model: "m",
		OutputGuard: func(context.Context, guard.Input) (bool, string, guard.Status) {
			return true, "", guard.StatusPassed
		},
		OutputGuardMaxRetries: 2,
	})
	var guardEvents int
	for _, e := range evs {
		if e.Kind == KindGuard {
			guardEvents++
			if !e.GuardPassed {
				t.Error("expected GuardPassed=true")
			}
			if e.GuardStatus != string(guard.StatusPassed) {
				t.Errorf("GuardStatus = %q, want %q", e.GuardStatus, guard.StatusPassed)
			}
		}
	}
	if guardEvents != 1 {
		t.Errorf("expected 1 KindGuard event on a genuine pass, got %d", guardEvents)
	}
}

// TestGuardSkippedTransportErrorEmitsSkippedStatus verifies a fail-open guard
// result (transport error: ok=true but no real verdict) is distinguishable in
// the emitted event from a genuine pass via GuardStatus (FIND-16) —
// GuardPassed alone is true in both cases.
func TestGuardSkippedTransportErrorEmitsSkippedStatus(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"final"}}, Model: "m",
		OutputGuard: func(context.Context, guard.Input) (bool, string, guard.Status) {
			return true, "", guard.StatusSkippedTransportError
		},
		OutputGuardMaxRetries: 2,
	})
	var guardEvents int
	for _, e := range evs {
		if e.Kind == KindGuard {
			guardEvents++
			if !e.GuardPassed {
				t.Error("fail-open skip should still report GuardPassed=true")
			}
			if e.GuardStatus != string(guard.StatusSkippedTransportError) {
				t.Errorf("GuardStatus = %q, want %q", e.GuardStatus, guard.StatusSkippedTransportError)
			}
		}
	}
	if guardEvents != 1 {
		t.Errorf("expected 1 KindGuard event, got %d", guardEvents)
	}
}

func TestGuardFailThenPass(t *testing.T) {
	calls := 0
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"bad", "good"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard: func(_ context.Context, in guard.Input) (bool, string, guard.Status) {
			calls++
			if in.Text == "good" {
				return true, "", guard.StatusPassed
			}
			return false, "needs work", guard.StatusFailed
		},
	})
	var guardEvents, passEvents, failEvents int
	for _, e := range evs {
		if e.Kind == KindGuard {
			guardEvents++
			if e.GuardPassed {
				passEvents++
				if e.GuardStatus != string(guard.StatusPassed) {
					t.Errorf("pass event GuardStatus = %q, want %q", e.GuardStatus, guard.StatusPassed)
				}
			} else {
				failEvents++
				if e.GuardReason != "needs work" {
					t.Errorf("guard reason = %q", e.GuardReason)
				}
				if e.GuardStatus != string(guard.StatusFailed) {
					t.Errorf("fail event GuardStatus = %q, want %q", e.GuardStatus, guard.StatusFailed)
				}
			}
		}
	}
	// One failure event (first attempt) plus one pass event (retry succeeds).
	if guardEvents != 2 || passEvents != 1 || failEvents != 1 {
		t.Errorf("expected 1 fail + 1 pass KindGuard event, got total=%d pass=%d fail=%d", guardEvents, passEvents, failEvents)
	}
	if calls != 2 {
		t.Errorf("expected guard called twice, got %d", calls)
	}
}

// TestGuardCorrectiveMentionsToolsAfterToolRound is a regression for a real
// failure mode: a model asked to "write a document" calls write_file (with
// placeholder/TODO content), then gives a final chat answer that itself
// mentions the unfinished placeholders, failing the "no placeholders or
// TODOs" default output-guard rubric. The old corrective prompt ("Revise and
// produce a corrected final answer") only asked for better *phrasing*, so the
// model could satisfy it with another content-free reply like "Let me do
// this properly" without ever re-calling a tool to fix the actual file. The
// corrective prompt must tell the model to use its tools when a tool round
// already happened this run.
func TestGuardCorrectiveMentionsToolsAfterToolRound(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: writes a file via a tool call.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"draft with TODO"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 2: final answer that fails the guard (mentions TODOs).
		{
			{Type: provider.EventTextDelta, Text: "Done, but there are still some TODOs to fill in."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 3: corrected final answer.
		{
			{Type: provider.EventTextDelta, Text: "Done, the document is complete."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	capture := &reqCapturingAdapter{inner: adapter}
	eng, err := New(Options{
		Adapter: capture, Tools: reg, Model: "test",
		OutputGuardMaxRetries: 1,
		OutputGuard: func(_ context.Context, in guard.Input) (bool, string, guard.Status) {
			if strings.Contains(in.Text, "TODO") {
				return false, "contains TODOs", guard.StatusFailed
			}
			return true, "", guard.StatusPassed
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "write a document"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	correctiveText := capture.capturedCorrective()
	if correctiveText == "" {
		t.Fatal("expected a corrective message in the retry turn's request")
	}
	if !strings.Contains(correctiveText, "call your file tools now") {
		t.Errorf("expected corrective prompt to direct tool use after a tool round, got: %q", correctiveText)
	}
	if !strings.Contains(correctiveText, "Do not reply with only an acknowledgment") {
		t.Errorf("expected corrective prompt to forbid a content-free reply, got: %q", correctiveText)
	}
	// P25.3: the corrective must forbid echoing validation language, so a
	// retry can't leak "PASS"/"FAIL" meta-text into the user-visible answer.
	if !strings.Contains(correctiveText, "never mention this validation step") {
		t.Errorf("expected corrective prompt to forbid mentioning validation, got: %q", correctiveText)
	}
	// P25.3: once the run settles, the retry scaffolding is retracted from the
	// conversation — but the tool round the run performed must stay intact.
	var hasToolRound bool
	for _, m := range conv.Messages {
		if hasToolUse(m) {
			hasToolRound = true
		}
		for _, b := range m.Content {
			if tb, ok := b.(provider.TextBlock); ok {
				if strings.Contains(tb.Text, "did not pass output validation") {
					t.Errorf("corrective message survived retraction: %q", tb.Text)
				}
				if strings.Contains(tb.Text, "still some TODOs") {
					t.Errorf("failed answer survived retraction: %q", tb.Text)
				}
			}
		}
	}
	if !hasToolRound {
		t.Error("retraction must not remove tool-use messages")
	}
	if final := assistantText(conv.Messages[len(conv.Messages)-1]); final != "Done, the document is complete." {
		t.Errorf("final message = %q, want the corrected answer", final)
	}
}

// TestGuardCorrectiveOmitsToolMentionWithoutToolRound verifies the
// tool-specific instruction is only added when a tool round actually
// happened this run — a plain text-only conversation has nothing to "fix" via
// file tools.
func TestGuardCorrectiveOmitsToolMentionWithoutToolRound(t *testing.T) {
	capture := &reqCapturingAdapter{inner: &scriptAdapter{replies: []string{"bad", "good"}}}
	eng, err := New(Options{
		Adapter: capture, Model: "m",
		OutputGuardMaxRetries: 2,
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
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	correctiveText := capture.capturedCorrective()
	if correctiveText == "" {
		t.Fatal("expected a corrective message in the retry turn's request")
	}
	if strings.Contains(correctiveText, "call your file tools now") {
		t.Errorf("did not expect tool-use instruction with no prior tool round, got: %q", correctiveText)
	}
}

// fakeFS backs fakeWriteTool/fakeReadTool with an in-memory path->content map,
// standing in for the real write_file/read_file tools without touching disk.
type fakeFS struct{ files map[string]string }

type fakeWriteTool struct{ fs *fakeFS }

func (t *fakeWriteTool) Name() string                 { return "write_file" }
func (t *fakeWriteTool) Description() string          { return "" }
func (t *fakeWriteTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *fakeWriteTool) Capability() tool.Capability  { return tool.CapWrite }
func (t *fakeWriteTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct{ Path, Content string }
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{}, err
	}
	t.fs.files[args.Path] = args.Content
	return tool.Result{Content: "wrote " + args.Path}, nil
}

type fakeReadTool struct{ fs *fakeFS }

func (t *fakeReadTool) Name() string                 { return "read_file" }
func (t *fakeReadTool) Description() string          { return "" }
func (t *fakeReadTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *fakeReadTool) Capability() tool.Capability  { return tool.CapRead }
func (t *fakeReadTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct{ Path string }
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{}, err
	}
	content, ok := t.fs.files[args.Path]
	if !ok {
		return tool.Result{Content: "not found", IsError: true}, nil
	}
	return tool.Result{Content: content}, nil
}

// TestGuardValidatesActualFileContentNotJustChatReply is the end-to-end
// regression: a model can write a file containing TODOs/placeholders while
// giving an innocuous chat reply ("Done") that says nothing about it. Before
// this fix, the guard only ever saw the chat text and would pass such a
// response. It must now read back the file the run wrote and fail the guard
// on the file's actual content.
func TestGuardValidatesActualFileContentNotJustChatReply(t *testing.T) {
	fs := &fakeFS{files: map[string]string{}}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: writes a file containing a TODO, via a tool call.
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "write_file",
				Input: json.RawMessage(`{"path":"docs/report.md","content":"# Report\nTODO: fill in numbers"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		// Turn 2: an innocuous final chat reply that says nothing about TODOs.
		{
			{Type: provider.EventTextDelta, Text: "Done."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&fakeWriteTool{fs: fs}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&fakeReadTool{fs: fs}); err != nil {
		t.Fatal(err)
	}

	// The guard always passes here — the point of this test is only to
	// observe what Files it was given, not to exercise the retry path
	// (covered separately by TestGuardCorrectiveMentionsToolsAfterToolRound).
	var seenFiles []guard.FileContent
	eng, err := New(Options{
		Adapter: adapter, Tools: reg, Model: "test",
		OutputGuard: func(_ context.Context, in guard.Input) (bool, string, guard.Status) {
			seenFiles = in.Files
			return true, "", guard.StatusPassed
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "write a document"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(seenFiles) != 1 {
		t.Fatalf("expected the guard to see 1 written file, got %d: %+v", len(seenFiles), seenFiles)
	}
	if seenFiles[0].Path != "docs/report.md" {
		t.Errorf("path = %q, want docs/report.md", seenFiles[0].Path)
	}
	if !strings.Contains(seenFiles[0].Content, "TODO: fill in numbers") {
		t.Errorf("expected guard to see actual file content, got: %q", seenFiles[0].Content)
	}
}

// TestGuardRetryReplacesVisibleAnswer is the P25.3 "replace, not append"
// regression: when a corrective retry succeeds, (a) the failure event is
// flagged GuardRetrying so a client withdraws the answer it already rendered,
// and (b) the failed answer and the corrective prompt are retracted from the
// conversation, so the durable transcript — and the model's context on later
// turns — holds only the answer the user actually saw.
func TestGuardRetryReplacesVisibleAnswer(t *testing.T) {
	eng, err := New(Options{
		Adapter: &scriptAdapter{replies: []string{"bad", "good"}}, Model: "m",
		OutputGuardMaxRetries: 2,
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
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	conv.Persisted = 1
	var evs []Event
	if err := eng.Run(context.Background(), conv, func(ev Event) { evs = append(evs, ev) }); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, e := range evs {
		if e.Kind != KindGuard {
			continue
		}
		if !e.GuardPassed && !e.GuardRetrying {
			t.Error("failure event that triggers a retry must be flagged GuardRetrying")
		}
		if e.GuardPassed && e.GuardRetrying {
			t.Error("pass event must not be flagged GuardRetrying")
		}
	}

	if len(conv.Messages) != 2 {
		t.Fatalf("conversation = %d messages after retraction, want 2 (user + final answer): %+v", len(conv.Messages), conv.Messages)
	}
	if got := assistantText(conv.Messages[1]); got != "good" {
		t.Errorf("surviving assistant message = %q, want %q", got, "good")
	}
	// Retraction rewrites already-persisted messages in place, so the caller
	// must be told to fall back to a full re-save (Persisted = -1), not append.
	if conv.Persisted != -1 {
		t.Errorf("Persisted = %d after retraction, want -1 (full re-save)", conv.Persisted)
	}
}

// TestGuardExhaustedRetractsIntermediateAttempts: when retries run out and the
// last attempt is surfaced anyway, the *intermediate* failed answers and their
// corrective prompts are still retracted — only the surfaced answer stays.
func TestGuardExhaustedRetractsIntermediateAttempts(t *testing.T) {
	eng, err := New(Options{
		Adapter: &scriptAdapter{replies: []string{"a", "b", "c"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard: func(context.Context, guard.Input) (bool, string, guard.Status) {
			return false, "always bad", guard.StatusFailed
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("conversation = %d messages after retraction, want 2: %+v", len(conv.Messages), conv.Messages)
	}
	if got := assistantText(conv.Messages[1]); got != "c" {
		t.Errorf("surviving assistant message = %q, want the surfaced final attempt %q", got, "c")
	}
}

func TestGuardExhaustedSurfaces(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"a", "b", "c", "d"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard: func(context.Context, guard.Input) (bool, string, guard.Status) {
			return false, "always bad", guard.StatusFailed
		},
	})
	var guardEvents, doneEvents int
	for _, e := range evs {
		switch e.Kind {
		case KindGuard:
			guardEvents++
			if e.GuardStatus != string(guard.StatusFailed) {
				t.Errorf("GuardStatus = %q, want %q", e.GuardStatus, guard.StatusFailed)
			}
		case KindDone:
			doneEvents++
		}
	}
	// 2 retries => 2 failure events on retries + 1 final exhausted event = 3.
	if guardEvents != 3 {
		t.Errorf("expected 3 KindGuard events, got %d", guardEvents)
	}
	if doneEvents != 1 {
		t.Errorf("expected exactly 1 KindDone, got %d", doneEvents)
	}
}
