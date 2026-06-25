package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type vetoHook struct{ blocked string }

func (v *vetoHook) PreToolUse(_ context.Context, name string, _ json.RawMessage) error {
	if name == v.blocked {
		return errors.New("not allowed")
	}
	return nil
}
func (v *vetoHook) PostToolUse(context.Context, string, json.RawMessage, string, bool) {}

func TestMultiVeto(t *testing.T) {
	m := NewMulti(&vetoHook{blocked: "shell"})
	if err := m.PreToolUse(context.Background(), "read_file", nil); err != nil {
		t.Errorf("read_file should pass, got %v", err)
	}
	if err := m.PreToolUse(context.Background(), "shell", nil); err == nil {
		t.Error("shell should be vetoed")
	}
}

func TestAuditWritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()
	ctx := context.Background()
	a.PreToolUse(ctx, "grep", json.RawMessage(`{"pattern":"x"}`))
	a.PostToolUse(ctx, "grep", nil, "result", false)

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Errorf("invalid jsonl line: %v", err)
		}
		if rec["tool"] != "grep" {
			t.Errorf("tool field = %v", rec["tool"])
		}
		lines++
	}
	if lines != 2 {
		t.Errorf("got %d audit lines, want 2", lines)
	}
}
