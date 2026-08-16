package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// fileAwareCompactor implements FileContextCompactor and records what the engine
// handed it, so a test can assert the paths actually crossed the seam rather
// than only that the engine collected them.
type fileAwareCompactor struct {
	gotRead     []string
	gotModified []string
	sawContext  bool
}

type fileCtxKey struct{}

func (c *fileAwareCompactor) WithFiles(ctx context.Context, read, modified []string) context.Context {
	c.gotRead, c.gotModified = read, modified
	return context.WithValue(ctx, fileCtxKey{}, true)
}

func (c *fileAwareCompactor) Compact(ctx context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	if v, _ := ctx.Value(fileCtxKey{}).(bool); v {
		c.sawContext = true
	}
	if len(msgs) <= 1 {
		return msgs, false, nil
	}
	return msgs[len(msgs)-1:], true, nil
}

// readWriteTool reads or writes the path it is given, so executeTool's
// capability branches record it. CapabilityFor makes the same tool answer both
// ways, which is what proves the recording follows *effective* capability
// (P25.4c) rather than a static one.
type readWriteTool struct{}

func (r *readWriteTool) Name() string                 { return "touch_file" }
func (r *readWriteTool) Description() string          { return "reads or writes a path" }
func (r *readWriteTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (r *readWriteTool) Capability() tool.Capability  { return tool.CapRead }
func (r *readWriteTool) CapabilityFor(input json.RawMessage) tool.Capability {
	var args struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Mode == "write" {
		return tool.CapWrite
	}
	return tool.CapRead
}
func (r *readWriteTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "ok"}, nil
}

// TestTouchedFilesReachTheCompactor covers the P65.2 engine half: read and
// written paths are recorded by effective capability and handed to a
// FileContextCompactor, split into the right lists.
func TestTouchedFilesReachTheCompactor(t *testing.T) {
	call := func(id, path, mode string) provider.Event {
		return provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: id, Name: "touch_file",
			Input: json.RawMessage(`{"path":"` + path + `","mode":"` + mode + `"}`)}}
	}
	turns := [][]provider.Event{
		{call("a", "src/read_one.go", "read"), call("b", "src/written.go", "write"), {Type: provider.EventDone, Stop: provider.StopToolUse}},
		{call("c", "src/read_two.go", "read"), {Type: provider.EventDone, Stop: provider.StopToolUse}},
		{{Type: provider.EventTextDelta, Text: "done"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}

	reg := tool.NewRegistry()
	if err := reg.Register(&readWriteTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &fileAwareCompactor{}
	eng, err := New(Options{
		Adapter: &scriptedAdapter{turns: turns}, Tools: reg, Compactor: comp,
		Model: "test", MaxTokens: 100, ContextWindowTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.Run(context.Background(), bigConversation(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !comp.sawContext {
		t.Fatal("the compactor was called without the decorated context")
	}
	// The compactor sees what had been touched by the time it ran, which is the
	// correct semantics and worth pinning: a compaction that fires after turn 1
	// must carry turn 1's paths and cannot know about turn 2's.
	if !containsStr(comp.gotRead, "src/read_one.go") {
		t.Errorf("read list %v is missing the path read before the compaction", comp.gotRead)
	}
	if !containsStr(comp.gotModified, "src/written.go") {
		t.Errorf("modified list %v is missing the written path", comp.gotModified)
	}
	// The split is the point: a path recorded on the wrong side tells the model
	// it changed a file it only looked at.
	if containsStr(comp.gotRead, "src/written.go") {
		t.Error("a written path leaked into the read list")
	}
	if containsStr(comp.gotModified, "src/read_one.go") {
		t.Error("a read path leaked into the modified list")
	}

	// By the end of the run both reads are on record, in the order they
	// happened — the ordering the carried list's cap depends on.
	read, modified := eng.touchedFiles()
	if len(read) != 2 || read[0] != "src/read_one.go" || read[1] != "src/read_two.go" {
		t.Errorf("run-end read list = %v, want both reads in call order", read)
	}
	if len(modified) != 1 || modified[0] != "src/written.go" {
		t.Errorf("run-end modified list = %v", modified)
	}
}

// TestFailedReadIsNotRecorded: a read that errored tells the model nothing about
// the file, so putting its path in the carried set would advertise a file the
// session never actually saw.
func TestFailedReadIsNotRecorded(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&failingReadTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: &scriptedAdapter{}, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, isErr := eng.executeTool(context.Background(), provider.ToolUseBlock{
		ID: "x", Name: "bad_read", Input: json.RawMessage(`{"path":"missing.txt"}`),
	}); !isErr {
		t.Fatal("expected the read to fail")
	}
	read, _ := eng.touchedFiles()
	if len(read) != 0 {
		t.Errorf("a failed read was recorded: %v", read)
	}
}

type failingReadTool struct{}

func (failingReadTool) Name() string                 { return "bad_read" }
func (failingReadTool) Description() string          { return "always fails" }
func (failingReadTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (failingReadTool) Capability() tool.Capability  { return tool.CapRead }
func (failingReadTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "no such file", IsError: true}, nil
}

// TestCompactorWithoutFileSupportIsUnaffected: the seam is optional, so a
// Compactor that predates P65.2 must be called exactly as before.
func TestCompactorWithoutFileSupportIsUnaffected(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&readWriteTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &noticeCompactor{}
	eng, err := New(Options{
		Adapter: &scriptedAdapter{turns: [][]provider.Event{
			{{Type: provider.EventTextDelta, Text: "ok"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
		}},
		Tools: reg, Compactor: comp, Model: "test", MaxTokens: 100, ContextWindowTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), bigConversation(), nil); err != nil {
		t.Fatalf("a Compactor without FileContextCompactor must still run: %v", err)
	}
}

func containsStr(xs []string, want string) bool {
	return strings.Contains("\x00"+strings.Join(xs, "\x00")+"\x00", "\x00"+want+"\x00")
}
