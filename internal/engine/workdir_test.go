package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// workdirCapturingTool records the workdir it observed via
// tool.WorkdirFromContext on each Execute call, so a test can assert the
// engine actually threads Options.Workdir through to tool calls (P25.1).
type workdirCapturingTool struct{ seen []string }

func (t *workdirCapturingTool) Name() string        { return "read_file" }
func (t *workdirCapturingTool) Description() string { return "read a file" }
func (t *workdirCapturingTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *workdirCapturingTool) Capability() tool.Capability { return tool.CapRead }
func (t *workdirCapturingTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	wd, _ := tool.WorkdirFromContext(ctx)
	t.seen = append(t.seen, wd)
	return tool.Result{Content: "ok"}, nil
}

// TestExecuteToolAppliesOptionsWorkdir verifies Options.Workdir (P25.1) is
// carried onto every tool call's context, and that leaving it unset carries
// no override — a tool falls back to its own default exactly as before this
// feature existed.
func TestExecuteToolAppliesOptionsWorkdir(t *testing.T) {
	ct := &workdirCapturingTool{}
	reg := tool.NewRegistry()
	if err := reg.Register(ct); err != nil {
		t.Fatal(err)
	}
	adapter, conv := toolRoundTripConv()

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100, Workdir: "/session/root"})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ct.seen) != 1 || ct.seen[0] != "/session/root" {
		t.Errorf("workdir seen by tool = %v, want [/session/root]", ct.seen)
	}
}

func TestExecuteToolLeavesWorkdirUnsetWhenOptionsWorkdirEmpty(t *testing.T) {
	ct := &workdirCapturingTool{}
	reg := tool.NewRegistry()
	if err := reg.Register(ct); err != nil {
		t.Fatal(err)
	}
	adapter, conv := toolRoundTripConv()

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ct.seen) != 1 || ct.seen[0] != "" {
		t.Errorf("workdir seen by tool = %v, want [\"\"] (no override)", ct.seen)
	}
}

// TestGuardReadsBackFilesFromSessionWorkdir is the P66.10 (ARCH-03)
// regression. The output guard reads written files back by invoking read_file
// directly rather than through executeTool, and that path used to pass the
// bare run context — so read_file fell back to its construction-time root (the
// daemon workspace) and, on a session with a custom workdir, the guard saw no
// file content at all and silently validated the chat text only.
//
// The registry here is rooted at a *daemon* directory while the engine runs
// with Workdir set to a different *session* directory; without e.toolCtx on
// the read-back path the guard's Files is empty.
func TestGuardReadsBackFilesFromSessionWorkdir(t *testing.T) {
	daemonDir := t.TempDir()
	sessionDir := t.TempDir()

	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: daemonDir}); err != nil {
		t.Fatal(err)
	}

	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "write_file",
				Input: json.RawMessage(`{"path":"report.md","content":"the deliverable"}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}},
		},
		{
			{Type: provider.EventTextDelta, Text: "wrote it"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}

	var seen []guard.FileContent
	eng, err := New(Options{
		Adapter: adapter, Tools: reg, Model: "test", Workdir: sessionDir,
		OutputGuard: func(_ context.Context, in guard.Input) (bool, string, guard.Status) {
			seen = in.Files
			return true, "", guard.StatusPassed
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "write the report"}}})
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The write itself already honored the session workdir (executeTool).
	if _, err := os.Stat(filepath.Join(sessionDir, "report.md")); err != nil {
		t.Fatalf("write_file did not land in the session workdir: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("guard saw %d file(s), want 1 — the read-back context is missing the session workdir", len(seen))
	}
	// read_file numbers its lines, so match on the payload rather than equality.
	if !strings.Contains(seen[0].Content, "the deliverable") {
		t.Errorf("guard file content = %q, want it to contain %q", seen[0].Content, "the deliverable")
	}
}
