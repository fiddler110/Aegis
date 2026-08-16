// Package hooks provides engine.Hooks implementations. The audit hook records
// every tool call to a JSONL trail, which doubles as a security audit log.
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/redact"
)

// Multi runs several hooks in order. PreToolUse stops at the first veto.
type Multi struct {
	hooks []Hook
}

// Hook is the local alias of the engine hook surface (kept here to avoid an
// import cycle; it matches engine.Hooks structurally).
type Hook interface {
	PreToolUse(ctx context.Context, toolName string, input json.RawMessage) error
	PostToolUse(ctx context.Context, toolName string, input json.RawMessage, result string, isError bool)
}

// NewMulti composes hooks.
func NewMulti(hs ...Hook) *Multi { return &Multi{hooks: hs} }

func (m *Multi) PreToolUse(ctx context.Context, name string, input json.RawMessage) error {
	for _, h := range m.hooks {
		if err := h.PreToolUse(ctx, name, input); err != nil {
			return err
		}
	}
	return nil
}

func (m *Multi) PostToolUse(ctx context.Context, name string, input json.RawMessage, result string, isErr bool) {
	for _, h := range m.hooks {
		h.PostToolUse(ctx, name, input, result, isErr)
	}
}

// Audit appends a JSONL record for each tool call to a file.
type Audit struct {
	mu   sync.Mutex
	path string
	file *os.File
}

// NewAudit creates an audit hook writing to path.
func NewAudit(path string) *Audit { return &Audit{path: path} }

// Close flushes and closes the audit file.
func (a *Audit) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file != nil {
		err := a.file.Close()
		a.file = nil
		return err
	}
	return nil
}

type auditRecord struct {
	Time    time.Time       `json:"time"`
	Phase   string          `json:"phase"`
	Tool    string          `json:"tool"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	// Sub-agent lifecycle fields (phase "subagent_stop").
	AgentID string `json:"agent_id,omitempty"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
	// Security policy fields (phase "policy_decision").
	Rule     string `json:"rule,omitempty"`
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Cap      string `json:"cap,omitempty"`
}

// SubagentStop records the SUBAGENT_STOP lifecycle event for a finished teammate.
func (a *Audit) SubagentStop(agentID, status, summary string, isErr bool) {
	a.write(auditRecord{
		Time:    time.Now(),
		Phase:   "subagent_stop",
		AgentID: agentID,
		Status:  status,
		Summary: summary,
		IsError: isErr,
	})
}

// PolicyDecision records a contextual security policy decision.
func (a *Audit) PolicyDecision(toolName, cap, rule, decision, reason string) {
	a.write(auditRecord{
		Time:     time.Now(),
		Phase:    "policy_decision",
		Tool:     toolName,
		Cap:      cap,
		Rule:     rule,
		Decision: decision,
		Reason:   reason,
	})
}

// Audit-trail input fidelity (P66.11 / SEC-11's redact-don't-truncate half).
//
// Every tool input over 1 KiB used to be replaced outright with
// "[N bytes, truncated]" — so a write_file with a 2 KiB payload, or any long
// shell pipeline, was **not recorded at all**. The audit trail lost exactly the
// calls whose content matters most for reconstructing an incident, and the stated
// reason for dropping them ("avoid logging bulk data or credentials embedded in
// long commands") is an argument for redacting the credential, not for discarding
// the record.
//
// So: redact, then record. Credential-shaped substrings are replaced with a class
// placeholder (internal/redact — the same set the MCP outbound boundary flags on),
// and the input is kept. The size bound stays, because "bulk data" is a real
// concern that redaction does not answer — a 4 MiB base64 blob is not evidence
// worth an audit line — but it is now an order of magnitude larger and, when it
// bites, it keeps the *head* of the input with a notice rather than substituting
// the length for the content. A truncated command still names the command.
const (
	// maxAuditInput bounds what one record holds. 16 KiB covers essentially every
	// real tool input (the largest inline tool *result* cap in the tree is 32 KiB,
	// and inputs are far smaller) while still refusing an embedded blob.
	maxAuditInput = 16 << 10
	// auditTruncNote marks a record whose input hit the bound, so a reader can
	// tell a complete record from a shortened one. Without it a shortened input
	// reads as the whole call, which is the same defect as dropping it, quieter.
	auditTruncNote = "…[audit: input truncated to %d of %d bytes]"
)

// auditInput redacts and, only if still oversized, shortens a tool input for the
// audit record. It returns a valid JSON value in every case: json.RawMessage is
// marshalled verbatim, so an invalid one here would corrupt the whole record
// rather than one field.
func auditInput(input json.RawMessage) json.RawMessage {
	if len(input) == 0 {
		return input
	}
	red, n := redact.Text(string(input))
	if n > 0 && !json.Valid([]byte(red)) {
		// A redaction can consume a delimiter (the assignment pattern can match
		// across `"key":"value"`), which would leave this field invalid JSON. Fall
		// back to recording it as a JSON string: the record stays parseable and
		// still shows what the call was, minus the credential.
		if b, err := json.Marshal(red); err == nil {
			red = string(b)
		} else {
			red = `"[redacted]"`
		}
	}
	if len(red) <= maxAuditInput {
		return json.RawMessage(red)
	}
	// Over the bound even after redaction: keep the head, as a JSON string so the
	// notice can ride along and the value stays well-formed regardless of where
	// the cut landed.
	head := red[:maxAuditInput]
	b, err := json.Marshal(head + fmt.Sprintf(auditTruncNote, maxAuditInput, len(red)))
	if err != nil {
		b, _ = json.Marshal(fmt.Sprintf("[%d bytes, unrecordable]", len(red)))
	}
	return json.RawMessage(b)
}

func (a *Audit) PreToolUse(_ context.Context, name string, input json.RawMessage) error {
	a.write(auditRecord{Time: time.Now(), Phase: "pre", Tool: name, Input: auditInput(input)})
	return nil
}

func (a *Audit) PostToolUse(_ context.Context, name string, _ json.RawMessage, _ string, isErr bool) {
	a.write(auditRecord{Time: time.Now(), Phase: "post", Tool: name, IsError: isErr})
}

func (a *Audit) write(rec auditRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file == nil {
		f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			slog.Error("audit: failed to open log", "path", a.path, "err", err)
			return
		}
		a.file = f
	}
	line, _ := json.Marshal(rec)
	if _, err := a.file.Write(append(line, '\n')); err != nil {
		slog.Error("audit: failed to write record", "path", a.path, "err", err)
	}
}
