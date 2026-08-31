package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/reqorigin"
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

// P81.14: the origin attached to the run context (internal/reqorigin) is
// stamped onto every record the hook writes, and absent when nothing
// attached one.
func TestAuditStampsOriginFromContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()

	mcpCtx := reqorigin.WithOrigin(context.Background(), reqorigin.MCP)
	a.PreToolUse(mcpCtx, "grep", nil)
	a.PostToolUse(mcpCtx, "grep", nil, "result", false)
	a.PreToolUse(context.Background(), "read_file", nil) // no origin attached

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var recs []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("invalid jsonl line: %v", err)
		}
		recs = append(recs, rec)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d audit lines, want 3", len(recs))
	}
	if recs[0]["origin"] != "mcp" || recs[1]["origin"] != "mcp" {
		t.Errorf("mcp-context records did not carry origin: %+v / %+v", recs[0], recs[1])
	}
	if _, ok := recs[2]["origin"]; ok {
		t.Errorf("record with no origin in context should omit the field, got %+v", recs[2])
	}
}

// P81.8: PostToolUse records the result's byte length, which is what an
// egress ledger needs ("every fetched URL and byte count") — the URL itself
// is already in the paired PreToolUse record's redacted input.
func TestAuditRecordsResultByteCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()
	ctx := context.Background()
	a.PostToolUse(ctx, "web_fetch", nil, "0123456789", false)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("invalid jsonl line: %v", err)
	}
	if got, want := rec["result_bytes"], float64(10); got != want {
		t.Errorf("result_bytes = %v, want %v", got, want)
	}
}

// P81.14: the audit sink rotates once it crosses its size bound, instead of
// growing without limit for the life of the daemon.
func TestAuditRotatesBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	// A tiny bound so a handful of records forces at least one rotation.
	a := NewAuditWithRotation(path, 200, 2)
	defer a.Close()
	ctx := context.Background()
	for range 50 {
		a.PreToolUse(ctx, "grep", json.RawMessage(`{"pattern":"some fairly long pattern to pad the line"}`))
	}
	a.Close()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected a rotated backup at %s.1, stat err: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat live audit file: %v", err)
	}
	if info.Size() >= 5000 {
		t.Errorf("live audit file did not rotate, size = %d", info.Size())
	}
}

// NewAudit (the default constructor every real daemon uses) rotates too, not
// just NewAuditWithRotation — the premise a caller should be able to rely on
// without knowing the internal default exists.
func TestNewAuditRotatesByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	if a.maxBytes <= 0 {
		t.Errorf("NewAudit's default maxBytes = %d, want > 0 (rotation on by default)", a.maxBytes)
	}
	a.Close()
}

func TestAuditInputRedactsAndRecordsOriginTogether(t *testing.T) {
	// Sanity check that redaction (existing behavior) and the new origin
	// stamp (P81.14) don't interfere with each other on the same record.
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()
	ctx := reqorigin.WithOrigin(context.Background(), reqorigin.CLI)
	const token = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	in, err := json.Marshal(map[string]string{"command": "curl -H 'Authorization: token " + token + "'"})
	if err != nil {
		t.Fatal(err)
	}
	a.PreToolUse(ctx, "shell", in)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("invalid jsonl line: %v", err)
	}
	if rec["origin"] != "cli" {
		t.Errorf("origin = %v, want cli", rec["origin"])
	}
	if strings.Contains(line, token) {
		t.Error("credential leaked into the audit record despite an origin stamp being present")
	}
}
