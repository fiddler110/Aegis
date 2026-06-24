package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditSubagentStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	a.SubagentStop("explore-1@default", "done", "found the bug", false)
	a.SubagentStop("build-2@default", "failed", "timeout", true)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d audit lines, want 2", len(lines))
	}

	var rec struct {
		Phase   string `json:"phase"`
		AgentID string `json:"agent_id"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Phase != "subagent_stop" || rec.AgentID != "explore-1@default" || rec.Status != "done" || rec.Summary != "found the bug" {
		t.Errorf("record = %+v", rec)
	}

	_ = json.Unmarshal([]byte(lines[1]), &rec)
	if rec.Status != "failed" || !rec.IsError {
		t.Errorf("second record = %+v", rec)
	}
}
