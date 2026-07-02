package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExecPreToolUseVeto(t *testing.T) {
	// exit 2 with stderr → veto surfaced to the model.
	h := NewExec([]ExecSpec{{
		Event:   EventPreToolUse,
		Command: `echo "policy: no shell in prod" 1>&2; exit 2`,
	}}, nil)
	err := h.PreToolUse(context.Background(), "shell", json.RawMessage(`{"cmd":"ls"}`))
	if err == nil {
		t.Fatal("expected veto error")
	}
	if !strings.Contains(err.Error(), "no shell in prod") {
		t.Errorf("stderr not surfaced: %v", err)
	}
}

func TestExecPreToolUseAllows(t *testing.T) {
	// exit 0 → allow; exit 1 → logged but allowed.
	for _, cmd := range []string{`exit 0`, `exit 1`} {
		h := NewExec([]ExecSpec{{Event: EventPreToolUse, Command: cmd}}, nil)
		if err := h.PreToolUse(context.Background(), "read", nil); err != nil {
			t.Errorf("cmd %q: unexpected veto: %v", cmd, err)
		}
	}
}

func TestExecToolFilter(t *testing.T) {
	// Hook scoped to "shell" should not fire (or veto) for "read".
	h := NewExec([]ExecSpec{{
		Event:   EventPreToolUse,
		Command: `exit 2`,
		Tools:   []string{"shell"},
	}}, nil)
	if err := h.PreToolUse(context.Background(), "read", nil); err != nil {
		t.Errorf("filtered hook fired for wrong tool: %v", err)
	}
	if err := h.PreToolUse(context.Background(), "shell", nil); err == nil {
		t.Error("expected veto for matching tool")
	}
}

func TestNewExecNilWhenEmpty(t *testing.T) {
	if h := NewExec(nil, nil); h != nil {
		t.Error("expected nil Exec for empty specs")
	}
}
