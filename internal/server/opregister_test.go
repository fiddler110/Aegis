package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/opregister"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// hangingTool closes entered once its Execute is reached, then blocks forever
// without ever consulting ctx — standing in for a process that dies mid-call
// (a cancelled context still unblocks an ordinary tool; nothing does here,
// which is the point: Execute genuinely never returns, so OnToolFinished
// genuinely never fires, exactly as a killed daemon would leave it).
type hangingTool struct{ entered chan struct{} }

func (h *hangingTool) Name() string                 { return "threat_model_scaffold" }
func (h *hangingTool) Description() string          { return "hangs after entering Execute" }
func (h *hangingTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (h *hangingTool) Capability() tool.Capability  { return tool.CapWrite }
func (h *hangingTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	close(h.entered)
	select {} // never returns — the process "dies" here
}

// TestOperationRegisterSurvivesEngineAcrossServerInstances is P65.4's server-
// level integration point: build a session turn's engine the way
// handlePostMessage does (via newEngine), let a tool call reach Execute and
// never return (simulating the daemon dying mid-call), then build a *second*
// engine — through a second Server sharing only the sqlite file, the way a
// restarted daemon process would — and confirm its first Run()'s
// repairOrphanedToolUses classifies the orphaned call as "may have run"
// using the durable register, not the conservative "never started" default
// a brand-new in-memory startedTools map would otherwise force.
func TestOperationRegisterSurvivesEngineAcrossServerInstances(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "build"}}

	// --- "process A": starts the call, then dies before it finishes ---
	dbA, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open dbA: %v", err)
	}
	dbA.SetMaxOpenConns(1)
	regA, err := opregister.NewStore(dbA)
	if err != nil {
		t.Fatalf("opregister.NewStore A: %v", err)
	}

	ht := &hangingTool{entered: make(chan struct{})}
	toolsA := tool.NewRegistry()
	if err := toolsA.Register(ht); err != nil {
		t.Fatal(err)
	}

	srvA := &Server{cfg: cfg, logger: logger, tools: toolsA, opRegister: regA,
		adapter: &scriptedToolAdapter{toolName: "threat_model_scaffold", toolInput: json.RawMessage(`{}`)}}

	engA, _, err := srvA.newEngine("sess-1", "build", permission.AutoApprove{}, nil, persona.Persona{}, false, nil, toolsA, "", t.TempDir(), "go", nil, time.Time{})
	if err != nil {
		t.Fatalf("newEngine A: %v", err)
	}
	convA := &engine.Conversation{}
	convA.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	// Run leaks (Execute never returns) — the test doesn't wait for it, the
	// same way a real process wouldn't get to finish it either. What matters
	// is that OnToolStarted has fired by the time entered closes.
	go func() { _ = engA.Run(context.Background(), convA, nil) }()
	select {
	case <-ht.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("hangingTool never entered Execute")
	}
	// Give the OnToolStarted callback (fired just before Execute, on the same
	// goroutine, synchronously) a moment to land its write.
	time.Sleep(50 * time.Millisecond)
	dbA.Close()

	// --- "process B": a fresh daemon, opening the same file ---
	dbB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open dbB: %v", err)
	}
	t.Cleanup(func() { dbB.Close() })
	dbB.SetMaxOpenConns(1)
	regB, err := opregister.NewStore(dbB)
	if err != nil {
		t.Fatalf("opregister.NewStore B: %v", err)
	}

	pending, err := regB.Pending(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ToolUseID == "" {
		t.Fatalf("Pending = %+v, want the row process A wrote before dying", pending)
	}
	toolUseID := pending[0].ToolUseID

	toolsB := tool.NewRegistry()
	if err := toolsB.Register(&hangingTool{entered: make(chan struct{})}); err != nil {
		t.Fatal(err)
	}
	srvB := &Server{cfg: cfg, logger: logger, tools: toolsB, opRegister: regB,
		adapter: &scriptedToolAdapter{done: true}}

	engB, _, err := srvB.newEngine("sess-1", "build", permission.AutoApprove{}, nil, persona.Persona{}, false, nil, toolsB, "", t.TempDir(), "go", nil, time.Time{})
	if err != nil {
		t.Fatalf("newEngine B: %v", err)
	}

	// The conversation as it would have been loaded from the session store:
	// the assistant's tool_use with no matching tool_result, because process A
	// died before the round finished.
	convB := &engine.Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: toolUseID, Name: "threat_model_scaffold", Input: json.RawMessage(`{}`)},
		}},
	}}
	if err := engB.Run(context.Background(), convB, nil); err != nil {
		t.Fatalf("Run B: %v", err)
	}

	var found bool
	for _, msg := range convB.Messages {
		for _, b := range msg.Content {
			tr, ok := b.(provider.ToolResultBlock)
			if !ok || tr.ToolUseID != toolUseID {
				continue
			}
			found = true
			if !strings.Contains(tr.Content, "may have partially completed") {
				t.Errorf("result = %q, want the durable-record uncertain wording", tr.Content)
			}
			if strings.Contains(tr.Content, "did not run") {
				t.Errorf("result wrongly asserts the call never started: %q", tr.Content)
			}
		}
	}
	if !found {
		t.Fatal("no synthetic result injected for the durably-seeded orphan")
	}
}

// scriptedToolAdapter is a minimal provider.Adapter: on the first Stream call
// it emits one tool_use for toolName/toolInput and stops; every subsequent
// call (or every call at all when done is true) just ends the turn in text.
type scriptedToolAdapter struct {
	toolName  string
	toolInput json.RawMessage
	done      bool
}

func (a *scriptedToolAdapter) Name() string { return "scripted" }

func (a *scriptedToolAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 3)
	if a.done || a.toolName == "" {
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "recovered"}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	} else {
		ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: "tu_1", Name: a.toolName, Input: a.toolInput}}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{}}
		a.done = true
	}
	close(ch)
	return ch, nil
}
