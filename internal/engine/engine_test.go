package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// scriptedAdapter returns a predefined event sequence for each successive call.
type scriptedAdapter struct {
	turns [][]provider.Event
	calls int
}

func (s *scriptedAdapter) Name() string { return "scripted" }

func (s *scriptedAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	events := s.turns[s.calls]
	s.calls++
	ch := make(chan provider.Event, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// echoTool returns its "msg" argument back as text.
type echoTool struct{ called int }

func (e *echoTool) Name() string                 { return "echo" }
func (e *echoTool) Description() string          { return "echo the msg argument" }
func (e *echoTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (e *echoTool) Capability() tool.Capability  { return tool.CapRead }
func (e *echoTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	e.called++
	var args struct {
		Msg string `json:"msg"`
	}
	_ = json.Unmarshal(input, &args)
	return tool.Result{Content: "echo:" + args.Msg}, nil
}

// namedFakeTool is a minimal registrable tool for tests that only need a
// distinct, chosen name (e.g. asserting sorted-name output in an error
// message), without any real behavior.
type namedFakeTool struct{ name string }

func (f *namedFakeTool) Name() string                 { return f.name }
func (f *namedFakeTool) Description() string          { return f.name + " description" }
func (f *namedFakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *namedFakeTool) Capability() tool.Capability  { return tool.CapRead }
func (f *namedFakeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

// oddShapeWriteTool declares CapWrite but takes an input shape
// writtenPathsFromInput doesn't recognize (no "path"/"file_path"/
// "edits[].path"), simulating an MCP tool or a future builtin whose field
// names differ from the ones the guard's path extraction knows about.
type oddShapeWriteTool struct{}

func (oddShapeWriteTool) Name() string                 { return "odd_write" }
func (oddShapeWriteTool) Description() string          { return "" }
func (oddShapeWriteTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (oddShapeWriteTool) Capability() tool.Capability  { return tool.CapWrite }
func (oddShapeWriteTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "ok"}, nil
}

// TestExecuteToolWarnsOnZeroPathWriteCall covers P32.6: a write-capability
// tool call that yields no extracted paths (writtenPathsFromInput only
// recognizes a few field-name shapes) must log a warning instead of silently
// degrading output-guard file coverage to nothing.
func TestExecuteToolWarnsOnZeroPathWriteCall(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(oddShapeWriteTool{}); err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	eng, err := New(Options{Adapter: &scriptedAdapter{}, Tools: reg, Model: "test", Logger: logger})
	if err != nil {
		t.Fatal(err)
	}

	_, isErr := eng.executeTool(context.Background(), provider.ToolUseBlock{
		ID: "tu_1", Name: "odd_write", Input: json.RawMessage(`{"unexpected_field":"value.txt"}`),
	})
	if isErr {
		t.Fatalf("expected the odd-shape write call to succeed")
	}

	if !strings.Contains(logBuf.String(), "write-capability tool call yielded no paths") {
		t.Errorf("expected a warning log for zero extracted paths, got: %s", logBuf.String())
	}
	eng.writtenFilesMu.Lock()
	n := len(eng.writtenFiles)
	eng.writtenFilesMu.Unlock()
	if n != 0 {
		t.Errorf("expected no written-files entries recorded, got %d", n)
	}
}

// capOverrideTool is a tool.CapabilityOverrider whose per-call capability is
// chosen by a caller-supplied function, so a test can build the reclassifying
// shapes no builtin currently has (only `shell` implements the interface in
// tree, and it only narrows).
type capOverrideTool struct {
	name     string
	static   tool.Capability
	perCall  func(json.RawMessage) tool.Capability
	executed int
}

func (c *capOverrideTool) Name() string                 { return c.name }
func (c *capOverrideTool) Description() string          { return "" }
func (c *capOverrideTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *capOverrideTool) Capability() tool.Capability  { return c.static }
func (c *capOverrideTool) CapabilityFor(input json.RawMessage) tool.Capability {
	return c.perCall(input)
}
func (c *capOverrideTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	c.executed++
	return tool.Result{Content: "ok"}, nil
}

// TestExecuteToolRecordsWrittenPathsByEffectiveCapability covers P63.8: the
// write-bookkeeping branch in executeTool reads tool.EffectiveCapability
// (P25.4c), not the static Capability(). A tool that reclassifies *into*
// CapWrite for a specific call must have that call's paths recorded, or the
// call silently loses output-guard file validation and quarantine-on-fail
// rollback; a tool that reclassifies *out of* CapWrite must not.
func TestExecuteToolRecordsWrittenPathsByEffectiveCapability(t *testing.T) {
	// A per-call rule shared by the widening and narrowing tools below: the
	// call is a write iff its input says so, regardless of the static
	// capability the tool declares.
	byMode := func(input json.RawMessage) tool.Capability {
		var args struct {
			Mode string `json:"mode"`
		}
		if json.Unmarshal(input, &args) == nil && args.Mode == "write" {
			return tool.CapWrite
		}
		return tool.CapRead
	}

	cases := []struct {
		name   string
		static tool.Capability
		// perCall defaults to byMode when nil.
		perCall func(json.RawMessage) tool.Capability
		input   string
		want    []string
	}{
		{
			// The defect: static CapRead, effective CapWrite. Before P63.8
			// this recorded nothing.
			name:   "widens into write",
			static: tool.CapRead,
			input:  `{"mode":"write","path":"widened.txt"}`,
			want:   []string{"widened.txt"},
		},
		{
			name:   "widening tool on a read call",
			static: tool.CapRead,
			input:  `{"mode":"read","path":"widened.txt"}`,
			want:   nil,
		},
		{
			// The behavior change P63.8 was split out of P63.3 for: a
			// statically-CapWrite tool that narrows for this call is now
			// skipped where it previously recorded.
			name:   "narrows out of write",
			static: tool.CapWrite,
			input:  `{"mode":"read","path":"narrowed.txt"}`,
			want:   nil,
		},
		{
			name:   "narrowing tool on a write call",
			static: tool.CapWrite,
			input:  `{"mode":"write","path":"narrowed.txt"}`,
			want:   []string{"narrowed.txt"},
		},
		{
			// The shape `shell` actually has today (CapExecute narrowing to
			// CapRead): unaffected either way, since neither capability is
			// CapWrite. Asserted explicitly so the one reachable overrider in
			// the tree is covered rather than assumed.
			name:    "shell-shaped execute narrowing to read",
			static:  tool.CapExecute,
			perCall: func(json.RawMessage) tool.Capability { return tool.CapRead },
			input:   `{"command":"git status","path":"shell.txt"}`,
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			perCall := tc.perCall
			if perCall == nil {
				perCall = byMode
			}
			ct := &capOverrideTool{name: "reclassify", static: tc.static, perCall: perCall}
			reg := tool.NewRegistry()
			if err := reg.Register(ct); err != nil {
				t.Fatal(err)
			}
			eng, err := New(Options{Adapter: &scriptedAdapter{}, Tools: reg, Model: "test"})
			if err != nil {
				t.Fatal(err)
			}

			if _, isErr := eng.executeTool(context.Background(), provider.ToolUseBlock{
				ID: "tu_1", Name: "reclassify", Input: json.RawMessage(tc.input),
			}); isErr {
				t.Fatalf("tool call reported an error")
			}
			if ct.executed != 1 {
				t.Fatalf("tool executed %d times, want 1", ct.executed)
			}

			eng.writtenFilesMu.Lock()
			got := make([]string, 0, len(eng.writtenFiles))
			for p := range eng.writtenFiles {
				got = append(got, p)
			}
			eng.writtenFilesMu.Unlock()
			slices.Sort(got)

			if len(got) != len(tc.want) || (len(tc.want) > 0 && !slices.Equal(got, tc.want)) {
				t.Errorf("recorded written paths = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunToolRoundTrip(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: assistant asks to call the echo tool.
		{
			{Type: provider.EventTextDelta, Text: "let me check"},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 5}},
		},
		// Turn 2: assistant produces the final answer.
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 20, OutputTokens: 3}},
		},
	}}

	reg := tool.NewRegistry()
	et := &echoTool{}
	if err := reg.Register(et); err != nil {
		t.Fatal(err)
	}

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var kinds []EventKind
	var finalText string
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})

	err = eng.Run(context.Background(), conv, func(ev Event) {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == KindText {
			finalText += ev.Text
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if et.called != 1 {
		t.Errorf("echo tool called %d times, want 1", et.called)
	}
	if finalText != "let me checkdone" {
		t.Errorf("accumulated text = %q", finalText)
	}
	// user + assistant(turn1) + tool_result(user) + assistant(turn2)
	if len(conv.Messages) != 4 {
		t.Fatalf("conversation has %d messages, want 4", len(conv.Messages))
	}
	if conv.Messages[3].Role != provider.RoleAssistant {
		t.Errorf("final message role = %s, want assistant", conv.Messages[3].Role)
	}
	if !slices.Contains(kinds, KindToolCall) || !slices.Contains(kinds, KindToolResult) || !slices.Contains(kinds, KindDone) {
		t.Errorf("missing expected event kinds, got %v", kinds)
	}
}

// TestRunEmitsColdLoadNotice covers P33.9: when a provider (the native
// Ollama adapter) reports a load_duration at or above the cold-load
// threshold, the engine surfaces it as a dim KindNotice so a long
// first-token wait gets named instead of looking like generic generation
// time. A load_duration below the threshold (an already-warm model's own
// bookkeeping overhead) must not produce a notice.
func TestRunEmitsColdLoadNotice(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "hi"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{
				InputTokens: 10, OutputTokens: 5, LoadDurationMS: 8200,
			}},
		},
	}}
	eng, err := New(Options{Adapter: adapter, Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})
	err = eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "cold-loaded") || !strings.Contains(notices[0], "8.2s") {
		t.Errorf("expected one cold-load notice mentioning 8.2s, got %v", notices)
	}
}

// TestRunSkipsColdLoadNoticeBelowThreshold: a small load_duration (already-
// warm model) must not be reported as a cold load.
func TestRunSkipsColdLoadNoticeBelowThreshold(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "hi"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{
				InputTokens: 10, OutputTokens: 5, LoadDurationMS: 50,
			}},
		},
	}}
	eng, err := New(Options{Adapter: adapter, Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})
	err = eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(notices) != 0 {
		t.Errorf("expected no cold-load notice below threshold, got %v", notices)
	}
}

// TestRunLogsPrefillDiagnostic covers P35.7: when a provider reports
// PromptEvalDurationMS (the native Ollama adapter), the engine logs
// prompt_eval_count and prompt_eval_duration_ms every turn so a live run can
// compare turn N vs. turn N+1 and tell a KV-cache hit (count tracks only the
// newly appended delta) from a full reprocess (count tracks the whole running
// conversation). A provider that never reports it (PromptEvalDurationMS == 0,
// every non-Ollama provider) must not log the line.
func TestRunLogsPrefillDiagnostic(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "hi"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{
				InputTokens: 4096, OutputTokens: 5, PromptEvalDurationMS: 1234,
			}},
		},
	}}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng, err := New(Options{Adapter: adapter, Model: "test", MaxTokens: 100, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := logBuf.String()
	if !strings.Contains(out, "prefill (prompt_eval)") {
		t.Fatalf("expected prefill diagnostic log line, got: %s", out)
	}
	if !strings.Contains(out, "prompt_eval_count=4096") {
		t.Errorf("expected prompt_eval_count=4096 in log, got: %s", out)
	}
	if !strings.Contains(out, "prompt_eval_duration_ms=1234") {
		t.Errorf("expected prompt_eval_duration_ms=1234 in log, got: %s", out)
	}
}

// TestRunSkipsPrefillDiagnosticWhenUnreported: a provider that never reports
// PromptEvalDurationMS (every non-Ollama adapter) must not emit the P35.7
// prefill diagnostic log line.
func TestRunSkipsPrefillDiagnosticWhenUnreported(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "hi"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{
				InputTokens: 10, OutputTokens: 5,
			}},
		},
	}}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng, err := New(Options{Adapter: adapter, Model: "test", MaxTokens: 100, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if strings.Contains(logBuf.String(), "prefill (prompt_eval)") {
		t.Errorf("expected no prefill diagnostic log line when PromptEvalDurationMS is unreported, got: %s", logBuf.String())
	}
}

// TestRunForwardsToolCallStart is the P33.3 guard: an adapter that announces
// a tool call while its arguments are still streaming
// (provider.EventToolUseStart) must reach the consumer as KindToolCallStart —
// ahead of the KindToolCall for the same call, which is unchanged and still
// carries the input. An adapter that never announces (every scripted turn in
// the tests above, and any older adapter) emits no such event at all.
func TestRunForwardsToolCallStart(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUseStart, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "echo"}},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var toolEvents []Event
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})
	err = eng.Run(context.Background(), conv, func(ev Event) {
		switch ev.Kind {
		case KindToolCallStart, KindToolCall:
			toolEvents = append(toolEvents, ev)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(toolEvents) != 2 {
		t.Fatalf("got %d tool-call events, want a start followed by the call: %+v", len(toolEvents), toolEvents)
	}
	start, call := toolEvents[0], toolEvents[1]
	if start.Kind != KindToolCallStart || start.ToolName != "echo" || start.ToolID != "tu_1" {
		t.Errorf("start event = %+v, want KindToolCallStart for echo/tu_1", start)
	}
	if len(start.ToolInput) != 0 {
		t.Errorf("start event carried input %q; the arguments aren't assembled yet", start.ToolInput)
	}
	if call.Kind != KindToolCall || string(call.ToolInput) != `{"msg":"hi"}` {
		t.Errorf("call event = %+v, want an unchanged KindToolCall carrying the input", call)
	}
}

func TestRunEmitsTurnTraces(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		// Turn 1: assistant asks to call the echo tool.
		{
			{Type: provider.EventTextDelta, Text: "let me check"},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 5}},
		},
		// Turn 2: final answer, no tools.
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 20, OutputTokens: 3}},
		},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}

	// Use a priced model so per-turn cost is exercised.
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "claude-opus-4-8", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var traces []TurnTraceForTest
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}})

	err = eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindTrace && ev.Trace != nil {
			traces = append(traces, TurnTraceForTest{
				Index: ev.Trace.Index, In: ev.Trace.InputTokens, Out: ev.Trace.OutputTokens,
				Cost: ev.Trace.CostUSD, Tools: len(ev.Trace.ToolCalls),
			})
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(traces) != 2 {
		t.Fatalf("got %d traces, want 2", len(traces))
	}
	if traces[0].Index != 0 || traces[1].Index != 1 {
		t.Errorf("trace indices = %d,%d want 0,1", traces[0].Index, traces[1].Index)
	}
	if traces[0].Tools != 1 {
		t.Errorf("turn 0 tool calls = %d, want 1 (echo)", traces[0].Tools)
	}
	if traces[1].Tools != 0 {
		t.Errorf("turn 1 tool calls = %d, want 0", traces[1].Tools)
	}
	if traces[0].In != 10 || traces[1].In != 20 {
		t.Errorf("input tokens = %d,%d want 10,20", traces[0].In, traces[1].In)
	}
	// Priced model must yield a positive per-turn cost.
	if traces[0].Cost <= 0 {
		t.Errorf("turn 0 cost = %f, want > 0", traces[0].Cost)
	}
}

// TurnTraceForTest is a flattened view used to assert on emitted traces.
type TurnTraceForTest struct {
	Index, In, Out, Tools int
	Cost                  float64
}

func TestRunUnknownTool(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "nope", Input: json.RawMessage(`{}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "recovered"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}
	reg := tool.NewRegistry()
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test"})

	var gotErrResult bool
	var gotContent string
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindToolResult && ev.ToolIsError {
			gotErrResult = true
			gotContent = ev.ToolResult
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !gotErrResult {
		t.Error("expected an error tool result for the unknown tool")
	}
	if !strings.Contains(gotContent, "registered tools:") {
		t.Errorf("unknown-tool error should list the registered-tools marker, got %q", gotContent)
	}
}

// TestRunUnknownTool_ListsRegisteredNames is P39.2: a small local model that
// invents a tool name should see the real registered names in the error, so
// it can self-correct without spending another turn guessing.
func TestRunUnknownTool_ListsRegisteredNames(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "run_tool", Input: json.RawMessage(`{}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "recovered"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&namedFakeTool{name: "zebra_tool"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&namedFakeTool{name: "alpha_tool"}); err != nil {
		t.Fatal(err)
	}
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test"})

	var gotContent string
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindToolResult && ev.ToolIsError {
			gotContent = ev.ToolResult
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantOrder := strings.Index(gotContent, "alpha_tool") < strings.Index(gotContent, "zebra_tool")
	if !strings.Contains(gotContent, "alpha_tool") || !strings.Contains(gotContent, "zebra_tool") || !wantOrder {
		t.Errorf("expected sorted registered tool names alpha_tool, zebra_tool in error, got %q", gotContent)
	}
}

// recordingHook vetoes a named tool and counts post-call invocations.
type recordingHook struct {
	veto      string
	postCalls int
}

func (h *recordingHook) PreToolUse(_ context.Context, name string, _ json.RawMessage) error {
	if name == h.veto {
		return errInterruptHook
	}
	return nil
}
func (h *recordingHook) PostToolUse(context.Context, string, json.RawMessage, string, bool) {
	h.postCalls++
}

var errInterruptHook = &hookErr{}

type hookErr struct{}

func (*hookErr) Error() string { return "blocked" }

func TestRunHookVeto(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo", Input: json.RawMessage(`{"msg":"x"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "ok"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}
	reg := tool.NewRegistry()
	et := &echoTool{}
	_ = reg.Register(et)
	hook := &recordingHook{veto: "echo"}
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Hooks: hook, Model: "test"})

	var blocked bool
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindToolResult && ev.ToolIsError {
			blocked = true
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !blocked {
		t.Error("expected vetoed tool to return an error result")
	}
	if et.called != 0 {
		t.Errorf("vetoed tool should not execute, called %d", et.called)
	}
	if hook.postCalls != 0 {
		t.Errorf("PostToolUse should not run for a vetoed call, got %d", hook.postCalls)
	}
}

func TestRunInterrupt(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{{Type: provider.EventTextDelta, Text: "x"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}}
	eng, _ := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Model: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conv := &Conversation{}
	err := eng.Run(ctx, conv, nil)
	if err != ErrInterrupted {
		t.Errorf("err = %v, want ErrInterrupted", err)
	}
}

func TestRepairOrphanedToolUses(t *testing.T) {
	t.Run("no orphans", func(t *testing.T) {
		msgs := []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
			{Role: provider.RoleAssistant, Content: []provider.Block{
				provider.ToolUseBlock{ID: "1", Name: "echo"},
			}},
			{Role: provider.RoleUser, Content: []provider.Block{
				provider.ToolResultBlock{ToolUseID: "1", Content: "ok"},
			}},
		}
		got := repairOrphanedToolUses(msgs, nil)
		if len(got) != len(msgs) {
			t.Errorf("len = %d, want %d (should be unchanged)", len(got), len(msgs))
		}
	})

	t.Run("orphan no following user message", func(t *testing.T) {
		msgs := []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
			{Role: provider.RoleAssistant, Content: []provider.Block{
				provider.ToolUseBlock{ID: "tu_1", Name: "shell"},
			}},
		}
		got := repairOrphanedToolUses(msgs, nil)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3 (synthetic result injected)", len(got))
		}
		userMsg := got[2]
		if userMsg.Role != provider.RoleUser {
			t.Errorf("injected message role = %s, want user", userMsg.Role)
		}
		if len(userMsg.Content) != 1 {
			t.Fatalf("injected content len = %d, want 1", len(userMsg.Content))
		}
		tr, ok := userMsg.Content[0].(provider.ToolResultBlock)
		if !ok {
			t.Fatalf("injected block is not ToolResultBlock")
		}
		if tr.ToolUseID != "tu_1" {
			t.Errorf("ToolUseID = %q, want %q", tr.ToolUseID, "tu_1")
		}
		if !tr.IsError {
			t.Error("synthetic result should be an error")
		}
	})

	t.Run("partial orphan merged into existing user message", func(t *testing.T) {
		msgs := []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
			{Role: provider.RoleAssistant, Content: []provider.Block{
				provider.ToolUseBlock{ID: "id1", Name: "read"},
				provider.ToolUseBlock{ID: "id2", Name: "write"},
			}},
			// Only id1 has a result; id2 is orphaned.
			{Role: provider.RoleUser, Content: []provider.Block{
				provider.ToolResultBlock{ToolUseID: "id1", Content: "data"},
			}},
		}
		got := repairOrphanedToolUses(msgs, nil)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3 (merged into existing user message)", len(got))
		}
		merged := got[2]
		if len(merged.Content) != 2 {
			t.Fatalf("merged content len = %d, want 2", len(merged.Content))
		}
		tr, ok := merged.Content[1].(provider.ToolResultBlock)
		if !ok {
			t.Fatal("second block should be ToolResultBlock")
		}
		if tr.ToolUseID != "id2" {
			t.Errorf("ToolUseID = %q, want id2", tr.ToolUseID)
		}
	})
}

func TestUsageFallbackEstimation(t *testing.T) {
	// An adapter that returns zero usage simulates a local/Ollama model.
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "hello world"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}},
		},
	}}
	eng, _ := New(Options{Adapter: adapter, Model: "local"})
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}})

	var usageEv *Event
	_ = eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindTurnDone {
			cp := ev
			usageEv = &cp
		}
	})
	if usageEv == nil {
		t.Fatal("no KindTurnDone event")
	}
	if usageEv.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if !usageEv.Usage.IsEstimated {
		t.Error("IsEstimated should be true when provider returns zero counts")
	}
	if usageEv.Usage.InputTokens == 0 {
		t.Error("estimated InputTokens should be > 0")
	}
}

// TestDoneEventCarriesEstimatedUsage is the P25.5 regression: previously the
// terminal KindDone event was emitted bare (no Usage at all), so an API/eval
// client that only reads the final "done" event — unlike the TUI, which reads
// the live per-turn KindTurnDone events — always saw zero tokens for a
// provider that reports no usage (local/Ollama models), even though the
// estimate was computed and shown live in the TUI the whole time.
func TestDoneEventCarriesEstimatedUsage(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "hello world"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}},
		},
	}}
	eng, _ := New(Options{Adapter: adapter, Model: "local"})
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}})

	var doneEv *Event
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindDone {
			cp := ev
			doneEv = &cp
		}
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if doneEv == nil {
		t.Fatal("no KindDone event")
	}
	if doneEv.Usage == nil {
		t.Fatal("done event Usage is nil")
	}
	if !doneEv.Usage.IsEstimated {
		t.Error("done event Usage.IsEstimated should be true when the provider reported no usage")
	}
	if doneEv.Usage.InputTokens == 0 {
		t.Error("done event estimated InputTokens should be > 0")
	}
	if doneEv.Usage.OutputTokens == 0 {
		t.Error("done event estimated OutputTokens should be > 0")
	}
}
