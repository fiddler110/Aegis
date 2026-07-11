package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tool"
)

// secretTool is a read-capability tool whose output looks like it leaked a
// credential, standing in for a real file-read tool for P24.12 / FIND-09
// redaction tests. Unlike a live-gitleaks integration test, the redaction
// decision itself is exercised via the redactSecretsFn seam (mirroring
// gitpr.go's scanPRTextForSecrets seam) so this test needs no gitleaks
// binary and runs in CI unconditionally.
type secretTool struct{}

func (secretTool) Name() string                 { return "read_file" }
func (secretTool) Description() string          { return "read a file" }
func (secretTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (secretTool) Capability() tool.Capability  { return tool.CapRead }
func (secretTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "config:\nAWS_ACCESS_KEY_ID=FAKE-NOT-A-REAL-CREDENTIAL-0000\n"}, nil
}

func toolRoundTripConv() (*scriptedAdapter, *Conversation) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{}`),
			}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 5}},
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 20, OutputTokens: 3}},
		},
	}}
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "read the config"}}})
	return adapter, conv
}

// toolResultContent finds the tool_result block content in conv.Messages for
// the given tool_use_id.
func toolResultContent(t *testing.T, conv *Conversation, toolUseID string) string {
	t.Helper()
	for _, msg := range conv.Messages {
		for _, blk := range msg.Content {
			if tr, ok := blk.(provider.ToolResultBlock); ok && tr.ToolUseID == toolUseID {
				return tr.Content
			}
		}
	}
	t.Fatalf("no tool_result block found for tool_use_id %q", toolUseID)
	return ""
}

func TestExecuteToolRedactsSecretsWhenEnabled(t *testing.T) {
	old := redactSecretsFn
	defer func() { redactSecretsFn = old }()
	redactSecretsFn = func(ctx context.Context, text string) (string, []security.Finding, error) {
		return "config:\n[REDACTED:aws-access-key]\n", []security.Finding{
			{Tool: "gitleaks", RuleID: "aws-access-key"},
		}, nil
	}

	reg := tool.NewRegistry()
	if err := reg.Register(secretTool{}); err != nil {
		t.Fatal(err)
	}

	adapter, conv := toolRoundTripConv()
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100, RedactSecrets: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResultContent(t, conv, "tu_1")
	if got != "config:\n[REDACTED:aws-access-key]\n" {
		t.Errorf("tool result content = %q, want redacted placeholder", got)
	}
}

func TestExecuteToolLeavesContentUnchangedWhenDisabled(t *testing.T) {
	old := redactSecretsFn
	defer func() { redactSecretsFn = old }()
	called := false
	redactSecretsFn = func(ctx context.Context, text string) (string, []security.Finding, error) {
		called = true
		return "should not be used", []security.Finding{{Tool: "gitleaks"}}, nil
	}

	reg := tool.NewRegistry()
	if err := reg.Register(secretTool{}); err != nil {
		t.Fatal(err)
	}

	adapter, conv := toolRoundTripConv()
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100, RedactSecrets: false})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if called {
		t.Errorf("redactSecretsFn should not be called when RedactSecrets is false")
	}
	got := toolResultContent(t, conv, "tu_1")
	const want = "config:\nAWS_ACCESS_KEY_ID=FAKE-NOT-A-REAL-CREDENTIAL-0000\n"
	if got != want {
		t.Errorf("tool result content = %q, want unchanged %q", got, want)
	}
}
