package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditRecords reads back every record the audit hook wrote, failing the test on
// any line that is not valid JSON — which is the property every case below leans
// on, since a record that cannot be parsed is not a record.
func auditRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("invalid audit line: %v\n%s", err, sc.Text())
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func inputOf(t *testing.T, rec map[string]any) string {
	t.Helper()
	b, err := json.Marshal(rec["input"])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAuditRecordsLargeInputsInsteadOfDiscardingThem is SEC-11's
// redact-don't-truncate half. A 2 KiB write_file payload used to be replaced
// wholesale with "[N bytes, truncated]", so the trail lost precisely the calls
// whose content matters for reconstructing an incident.
func TestAuditRecordsLargeInputsInsteadOfDiscardingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()

	payload := strings.Repeat("package main // real content\n", 100) // ~2.8 KiB
	in, err := json.Marshal(map[string]string{"path": "main.go", "content": payload})
	if err != nil {
		t.Fatal(err)
	}
	a.PreToolUse(context.Background(), "write_file", in)

	recs := auditRecords(t, path)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	got := inputOf(t, recs[0])
	if strings.Contains(got, "truncated]") {
		t.Errorf("a %d-byte input was discarded rather than recorded: %s", len(in), got)
	}
	if !strings.Contains(got, "package main // real content") {
		t.Errorf("the recorded input does not contain the payload:\n%s", got)
	}
	if !strings.Contains(got, "main.go") {
		t.Errorf("the recorded input lost the path field:\n%s", got)
	}
}

// TestAuditRedactsCredentialsInRecordedInput: keeping the record is only
// acceptable because the credential does not come with it. This is the half of
// the trade that makes the change safe, so it is asserted on the same input shape
// the old bound existed for — a long shell command with a token in it.
func TestAuditRedactsCredentialsInRecordedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()

	const token = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	cmd := "curl -H 'Authorization: token " + token + "' https://api.github.com/user # " + strings.Repeat("x", 1500)
	in, err := json.Marshal(map[string]string{"command": cmd})
	if err != nil {
		t.Fatal(err)
	}
	a.PreToolUse(context.Background(), "shell", in)

	got := inputOf(t, auditRecords(t, path)[0])
	if strings.Contains(got, token) {
		t.Errorf("the audit record contains the token verbatim:\n%s", got)
	}
	if !strings.Contains(got, "[redacted:") {
		t.Errorf("nothing was redacted, so the token was either kept or the record was dropped:\n%s", got)
	}
	// And the command itself is still auditable — that is the whole point of
	// redacting rather than truncating.
	if !strings.Contains(got, "api.github.com/user") {
		t.Errorf("the recorded command lost its identity:\n%s", got)
	}
}

// TestAuditBoundStillBitesOnBulkData: the size bound is not gone, because "don't
// put a 4 MiB blob in the audit log" is a real concern redaction does not answer.
// When it bites it must keep the *head* — a truncated command still names the
// command — and say that it did.
func TestAuditBoundStillBitesOnBulkData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()

	// Raw JSON rather than a marshalled map, because map marshalling sorts keys —
	// and "keep the head" means a huge field ordered before the identifying one
	// crowds it out. That is a real limitation of a head-keeping bound over JSON
	// and is not worth machinery to fix; it is worth knowing, so the fixture is
	// written in the order a real tool call arrives in.
	blob := strings.Repeat("A", 4<<20)
	in := json.RawMessage(`{"path":"blob.bin","content":"` + blob + `"}`)
	a.PreToolUse(context.Background(), "write_file", in)

	got := inputOf(t, auditRecords(t, path)[0])
	// A little over maxAuditInput, not exactly it: the value is written as a JSON
	// string, so the notice and the quoting add their own bytes on top of the kept
	// head. The assertion is that the bound bit, not that it bit to the byte.
	if len(got) > maxAuditInput+len(fmt.Sprintf(auditTruncNote, maxAuditInput, len(in)))+64 {
		t.Errorf("record is %d bytes for a %d-byte input; the bound did not bite", len(got), len(in))
	}
	if !strings.Contains(got, "audit: input truncated") {
		t.Errorf("a shortened record does not say it was shortened, so it reads as complete:\n%s", got[:200])
	}
	if !strings.Contains(got, "blob.bin") {
		t.Errorf("the head was not kept, so the record does not name the call:\n%s", got[:200])
	}
}

// TestAuditInputStaysValidJSONWhenARedactionEatsADelimiter: the assignment pattern
// can match across `"key":"value"`, taking the quote and colon with it. The record
// must stay parseable — json.RawMessage is written verbatim, so an invalid value
// here corrupts the whole line rather than one field.
func TestAuditInputStaysValidJSONWhenARedactionEatsADelimiter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewAudit(path)
	defer a.Close()

	// Written as raw JSON rather than marshalled, so the exact byte layout the
	// pattern spans is what reaches auditInput.
	a.PreToolUse(context.Background(), "shell", json.RawMessage(`{"api_key":"0123456789abcdefghij"}`))

	// auditRecords fails the test if the line does not parse, which is the
	// assertion; the rest confirms the secret is gone rather than merely encoded.
	got := inputOf(t, auditRecords(t, path)[0])
	if strings.Contains(got, "0123456789abcdefghij") {
		t.Errorf("the secret survived: %s", got)
	}
}

// TestAuditEmptyInputIsUnchanged: a nil input (a tool called with no arguments,
// and the shape half the existing tests pass) must not become a placeholder or a
// quoted empty string.
func TestAuditEmptyInputIsUnchanged(t *testing.T) {
	if got := auditInput(nil); len(got) != 0 {
		t.Errorf("auditInput(nil) = %q, want empty", got)
	}
}
