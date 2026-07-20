package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestSkillToolCorrectiveGuard verifies that misrouting a sub-agent workflow
// (mode/agents) to the skill tool returns a corrective error pointing at the
// `agent` tool rather than silently dropping the fields (P38.1).
func TestSkillToolCorrectiveGuard(t *testing.T) {
	st := NewSkillTool(t.TempDir(), t.TempDir(), nil)
	input := `{"name":"threat-modeling","mode":"sequential","agents":[{"prompt":"x"}]}`
	res, err := st.Execute(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true, got false; content=%q", res.Content)
	}
	if !strings.Contains(res.Content, "`agent`") {
		t.Fatalf("expected corrective message mentioning the agent tool, got %q", res.Content)
	}
}

// TestSkillToolGuardNotTriggeredForPlainName confirms a plain name-only call is
// not flagged by the corrective guard (it falls through to normal load, which
// errors on an unknown skill — not the guard's error).
func TestSkillToolGuardNotTriggeredForPlainName(t *testing.T) {
	st := NewSkillTool(t.TempDir(), t.TempDir(), nil)
	res, err := st.Execute(context.Background(), json.RawMessage(`{"name":"nonexistent-skill"}`))
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if strings.Contains(res.Content, "no `mode` or `agents` parameter") {
		t.Fatalf("guard should not trigger for a name-only call, got %q", res.Content)
	}
}
