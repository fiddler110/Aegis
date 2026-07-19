package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// fileStore is a tiny in-memory stand-in for the workspace, shared by the
// store-backed read/write tools below so a test can observe whether a read saw
// a write's effect.
type fileStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newFileStore() *fileStore { return &fileStore{data: map[string]string{}} }

func (s *fileStore) set(path, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[path] = content
}

func (s *fileStore) get(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[path]
	return v, ok
}

// storeWriteTool mimics write_file: a CapWrite tool that records content under
// a "path". The delay widens the window a racing read would exploit, making the
// regression deterministic — without same-path ordering a concurrent read runs
// during this sleep and finds nothing.
type storeWriteTool struct {
	store *fileStore
	delay time.Duration
}

func (storeWriteTool) Name() string                 { return "write_file" }
func (storeWriteTool) Description() string          { return "writes to the store" }
func (storeWriteTool) Capability() tool.Capability  { return tool.CapWrite }
func (storeWriteTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t storeWriteTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct{ Path, Content string }
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{}, err
	}
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	t.store.set(args.Path, args.Content)
	return tool.Result{Content: "wrote " + args.Path}, nil
}

// storeReadTool mimics read_file: a CapRead tool that returns stored content,
// or an error result when the path is absent (the symptom of losing a
// read-before-write race).
type storeReadTool struct{ store *fileStore }

func (storeReadTool) Name() string                 { return "read_file" }
func (storeReadTool) Description() string          { return "reads from the store" }
func (storeReadTool) Capability() tool.Capability  { return tool.CapRead }
func (storeReadTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t storeReadTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct{ Path string }
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{}, err
	}
	if v, ok := t.store.get(args.Path); ok {
		return tool.Result{Content: v}, nil
	}
	return tool.Result{Content: "not found: " + args.Path, IsError: true}, nil
}

// TestSamePathReadWaitsForWrite is the regression for the read-before-write
// race: a model that emits write_file then read_file on the same path in one
// tool round must have the read observe the write. The write sleeps to make a
// racing (unordered) read reliably lose, so this test fails if the same-path
// ordering in runTools is removed.
func TestSamePathReadWaitsForWrite(t *testing.T) {
	store := newFileStore()

	turn1 := []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_0", Name: "write_file", Input: json.RawMessage(`{"path":"fizzbuzz.py","content":"print(1)"}`)}},
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{"path":"fizzbuzz.py"}`)}},
		{Type: provider.EventDone, Stop: provider.StopToolUse},
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		turn1,
		{{Type: provider.EventTextDelta, Text: "ok"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(storeWriteTool{store: store, delay: 50 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(storeReadTool{store: store}); err != nil {
		t.Fatal(err)
	}
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test"})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := conv.Messages[2].Content
	if len(results) != 2 {
		t.Fatalf("got %d result blocks, want 2", len(results))
	}
	// Order is preserved regardless of execution order.
	read, ok := results[1].(provider.ToolResultBlock)
	if !ok {
		t.Fatalf("result 1 is %T, want ToolResultBlock", results[1])
	}
	if read.IsError {
		t.Fatalf("read raced ahead of the write: %q", read.Content)
	}
	if read.Content != "print(1)" {
		t.Errorf("read content = %q, want %q", read.Content, "print(1)")
	}
}

// TestDistinctPathReadDoesNotWaitForWrite guards the other direction: the
// same-path ordering must not over-serialize. A read on a different path than a
// concurrently-blocked write must be free to start immediately — proving reads
// only wait on writes that share their exact path.
func TestDistinctPathReadDoesNotWaitForWrite(t *testing.T) {
	writeStarted := make(chan struct{})
	release := make(chan struct{})
	readStarted := make(chan struct{})

	writeBlocker := blockingTool{
		name: "write_file", cap: tool.CapWrite,
		onStart: func() { close(writeStarted) }, block: release,
	}
	readProbe := blockingTool{
		name: "read_file", cap: tool.CapRead,
		onStart: func() { close(readStarted) },
	}

	turn1 := []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_0", Name: "write_file", Input: json.RawMessage(`{"path":"a.txt"}`)}},
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{"path":"b.txt"}`)}},
		{Type: provider.EventDone, Stop: provider.StopToolUse},
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		turn1,
		{{Type: provider.EventTextDelta, Text: "ok"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}}

	reg := tool.NewRegistry()
	if err := reg.Register(writeBlocker); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(readProbe); err != nil {
		t.Fatal(err)
	}
	eng, _ := New(Options{Adapter: adapter, Tools: reg, Model: "test"})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	runDone := make(chan error, 1)
	go func() { runDone <- eng.Run(context.Background(), conv, nil) }()

	// Wait for the write to be executing (and blocked), then the read on the
	// distinct path must still be able to start while the write holds.
	select {
	case <-writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("write never started")
	}
	select {
	case <-readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("read on a distinct path was blocked behind the write — over-serialized")
	}
	close(release)

	if err := <-runDone; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// blockingTool signals when its Execute begins and optionally blocks until a
// channel is closed, for proving overlap/non-overlap of concurrent calls.
type blockingTool struct {
	name    string
	cap     tool.Capability
	onStart func()
	block   chan struct{} // nil means don't block
}

func (b blockingTool) Name() string                 { return b.name }
func (b blockingTool) Description() string          { return "test tool" }
func (b blockingTool) Capability() tool.Capability  { return b.cap }
func (b blockingTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (b blockingTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	if b.onStart != nil {
		b.onStart()
	}
	if b.block != nil {
		<-b.block
	}
	return tool.Result{Content: "done"}, nil
}

func TestToolTargetPath(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", `{"path":"fizzbuzz.py"}`, "fizzbuzz.py"},
		{"dot-prefixed cleans equal", `{"path":"./fizzbuzz.py"}`, "fizzbuzz.py"},
		{"nested", `{"path":"docs/out.md","content":"x"}`, "docs/out.md"},
		{"no path field", `{"query":"recon_scan"}`, ""},
		{"empty path", `{"path":""}`, ""},
		{"empty input", ``, ""},
		{"non-string path", `{"path":42}`, ""},
		{"not an object", `"bare"`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// filepath.Clean emits the OS separator, so normalize the expected
			// value the same way to keep the assertion platform-independent.
			want := filepath.FromSlash(tc.want)
			if got := toolTargetPath(json.RawMessage(tc.input)); got != want {
				t.Errorf("toolTargetPath(%s) = %q, want %q", tc.input, got, want)
			}
		})
	}
}
