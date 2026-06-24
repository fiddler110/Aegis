// Package hooks provides engine.Hooks implementations. The audit hook records
// every tool call to a JSONL trail, which doubles as a security audit log.
package hooks

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
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
}

// NewAudit creates an audit hook writing to path.
func NewAudit(path string) *Audit { return &Audit{path: path} }

type auditRecord struct {
	Time    time.Time       `json:"time"`
	Phase   string          `json:"phase"`
	Tool    string          `json:"tool"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

func (a *Audit) PreToolUse(_ context.Context, name string, input json.RawMessage) error {
	a.write(auditRecord{Time: time.Now(), Phase: "pre", Tool: name, Input: input})
	return nil
}

func (a *Audit) PostToolUse(_ context.Context, name string, _ json.RawMessage, _ string, isErr bool) {
	a.write(auditRecord{Time: time.Now(), Phase: "post", Tool: name, IsError: isErr})
}

func (a *Audit) write(rec auditRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(rec)
	f.Write(append(line, '\n'))
}
