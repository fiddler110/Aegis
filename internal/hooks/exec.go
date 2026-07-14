package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// Lifecycle event names for user-configured hooks (P4.4). These mirror Claude
// Code's hook events.
const (
	EventPreToolUse   = "pre_tool_use"
	EventPostToolUse  = "post_tool_use"
	EventSessionStart = "session_start"
	EventStop         = "stop"
	EventSubagentStop = "subagent_stop"
)

// ExecSpec is one configured external hook command.
type ExecSpec struct {
	Event      string
	Command    string
	Tools      []string // optional tool-name filter for tool events
	TimeoutSec int
}

// hookEvent is the JSON document delivered to a hook command on stdin.
type hookEvent struct {
	Event   string          `json:"event"`
	Tool    string          `json:"tool,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Result  string          `json:"result,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	Session string          `json:"session_id,omitempty"`
	AgentID string          `json:"agent_id,omitempty"`
	Status  string          `json:"status,omitempty"`
	Summary string          `json:"summary,omitempty"`
}

// Exec runs user-configured shell commands on lifecycle events. It implements
// the engine Hook interface (PreToolUse/PostToolUse) and offers helpers for the
// non-tool events fired by the server. A pre_tool_use command that exits with
// code 2 vetoes the tool call; its stderr is surfaced to the model.
type Exec struct {
	byEvent map[string][]ExecSpec
	logger  *slog.Logger
}

// NewExec groups the given specs by event. Returns nil when no specs are given
// so callers can skip wiring it entirely.
func NewExec(specs []ExecSpec, logger *slog.Logger) *Exec {
	if len(specs) == 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	byEvent := map[string][]ExecSpec{}
	for _, s := range specs {
		if s.Command == "" || s.Event == "" {
			continue
		}
		byEvent[s.Event] = append(byEvent[s.Event], s)
	}
	return &Exec{byEvent: byEvent, logger: logger}
}

func (e *Exec) matches(s ExecSpec, tool string) bool {
	if len(s.Tools) == 0 {
		return true
	}
	for _, t := range s.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

// run executes one hook command, returning its exit code and captured stderr.
func (e *Exec) run(ctx context.Context, s ExecSpec, ev hookEvent) (exitCode int, stderr string) {
	timeout := time.Duration(s.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, _ := json.Marshal(ev)
	shell, args := shellCommand(s.Command)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Stdin = bytes.NewReader(payload)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	err := cmd.Run()
	stderr = strings.TrimSpace(errBuf.String())
	if err == nil {
		return 0, stderr
	}
	var ee *exec.ExitError
	if ok := asExitError(err, &ee); ok {
		return ee.ExitCode(), stderr
	}
	// Failed to start / timed out: log but do not veto.
	e.logger.Warn("hook command failed to run", "event", s.Event, "err", err)
	return -1, stderr
}

// PreToolUse fires pre_tool_use hooks. The first hook to exit 2 vetoes the call.
func (e *Exec) PreToolUse(ctx context.Context, toolName string, input json.RawMessage) error {
	if e == nil {
		return nil
	}
	for _, s := range e.byEvent[EventPreToolUse] {
		if !e.matches(s, toolName) {
			continue
		}
		code, stderr := e.run(ctx, s, hookEvent{Event: EventPreToolUse, Tool: toolName, Input: input})
		if code == 2 {
			reason := stderr
			if reason == "" {
				reason = "vetoed by pre_tool_use hook"
			}
			return fmt.Errorf("%s", reason)
		}
		if code > 0 {
			e.logger.Warn("pre_tool_use hook non-blocking failure", "tool", toolName, "code", code, "stderr", stderr)
		}
	}
	return nil
}

// PostToolUse fires post_tool_use hooks (best-effort; cannot veto).
func (e *Exec) PostToolUse(ctx context.Context, toolName string, input json.RawMessage, result string, isError bool) {
	if e == nil {
		return
	}
	for _, s := range e.byEvent[EventPostToolUse] {
		if !e.matches(s, toolName) {
			continue
		}
		e.run(ctx, s, hookEvent{Event: EventPostToolUse, Tool: toolName, Input: input, Result: result, IsError: isError})
	}
}

// SessionStart fires session_start hooks.
func (e *Exec) SessionStart(ctx context.Context, sessionID string) {
	if e == nil {
		return
	}
	for _, s := range e.byEvent[EventSessionStart] {
		e.run(ctx, s, hookEvent{Event: EventSessionStart, Session: sessionID})
	}
}

// Stop fires stop hooks when a run completes.
func (e *Exec) Stop(ctx context.Context, sessionID string) {
	if e == nil {
		return
	}
	for _, s := range e.byEvent[EventStop] {
		e.run(ctx, s, hookEvent{Event: EventStop, Session: sessionID})
	}
}

// SubagentStop fires subagent_stop hooks when a teammate finishes.
func (e *Exec) SubagentStop(ctx context.Context, agentID, status, summary string, isErr bool) {
	if e == nil {
		return
	}
	for _, s := range e.byEvent[EventSubagentStop] {
		e.run(ctx, s, hookEvent{Event: EventSubagentStop, AgentID: agentID, Status: status, Summary: summary, IsError: isErr})
	}
}

// shellCommand returns the platform shell binary and argument list for running
// a hook command string, mirroring sandbox.shellCommand's and
// security.shellInvocation's own invocation convention (P30.2) so hook
// commands configured via pre_tool_use/post_tool_use/session_start/stop/
// subagent_stop actually launch on a native Windows host, which has no
// POSIX `sh` on PATH.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return sandbox.WindowsShellBinary(), []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// asExitError is a tiny errors.As helper kept local to avoid importing errors
// solely for one call site.
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
