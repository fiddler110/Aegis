package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/sandbox"
	"github.com/scottymacleod/aegis/internal/task"
	"github.com/scottymacleod/aegis/internal/tool"
)

type shellTool struct {
	root       string
	timeoutSec int
	mgr        *task.Manager   // optional; enables background:true
	sb         sandbox.Backend // optional; nil = inline local exec (legacy path)
}

func newShellTool(root string, timeoutSec int, mgr *task.Manager, sb sandbox.Backend) *shellTool {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &shellTool{root: root, timeoutSec: timeoutSec, mgr: mgr, sb: sb}
}

func (t *shellTool) Name() string                { return "shell" }
func (t *shellTool) Capability() tool.Capability { return tool.CapExecute }
func (t *shellTool) Description() string {
	if runtime.GOOS == "windows" {
		return "Run a PowerShell command in the workspace directory and return combined stdout/stderr. Commands execute via: powershell -NoProfile -NonInteractive -Command <command>. Use PowerShell syntax — Unix commands (ls, cat, grep, find, rm, chmod, etc.) are not available; use Get-ChildItem, Get-Content, Select-String, Remove-Item, etc."
	}
	return "Run a shell command in the workspace directory and return combined stdout/stderr. Commands execute via /bin/sh -c. Bounded by a configurable timeout."
}
func (t *shellTool) InputSchema() json.RawMessage {
	if runtime.GOOS == "windows" {
		return schema(`{"type":"object","properties":{"command":{"type":"string","description":"PowerShell command to run. Use PowerShell syntax (Get-ChildItem, Get-Content, Select-String, Remove-Item, etc.) — Unix commands do not work in PowerShell."},"timeout_sec":{"type":"integer","description":"optional per-call timeout override in seconds"},"background":{"type":"boolean","description":"run as a detached background job and return a task id immediately instead of blocking"}},"required":["command"]}`)
	}
	return schema(`{"type":"object","properties":{"command":{"type":"string","description":"the shell command to run via /bin/sh -c"},"timeout_sec":{"type":"integer","description":"optional per-call timeout override in seconds"},"background":{"type":"boolean","description":"run as a detached background job and return a task id immediately instead of blocking"}},"required":["command"]}`)
}

func (t *shellTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Command    string `json:"command"`
		TimeoutSec int    `json:"timeout_sec"`
		Background bool   `json:"background"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Command) == "" {
		return tool.Result{Content: "command is required", IsError: true}, nil
	}

	const maxTimeoutSec = 600
	timeout := time.Duration(t.timeoutSec) * time.Second
	if args.TimeoutSec > 0 {
		timeout = time.Duration(min(args.TimeoutSec, maxTimeoutSec)) * time.Second
	}

	if args.Background {
		if t.mgr == nil {
			return tool.Result{Content: "background jobs are not available in this context", IsError: true}, nil
		}
		tk, err := t.mgr.Start(task.Spec{Kind: "shell", Title: truncateTitle(args.Command)}, func(jobCtx context.Context, emit func(string)) (string, error) {
			return "", t.execStreaming(jobCtx, args.Command, timeout, emit)
		})
		if err != nil {
			return tool.Result{Content: "shell: " + err.Error(), IsError: true}, nil
		}
		return tool.Result{Content: fmt.Sprintf("Started background shell job (task id %s). Poll with task_get; read output with task_output; cancel with task_stop.", tk.ID)}, nil
	}

	// Foreground execution.
	text, err := t.exec(ctx, args.Command, timeout)
	const maxOutput = 200 << 10 // 200 KiB — prevent context flooding on large outputs
	if len(text) > maxOutput {
		text = text[:maxOutput] + fmt.Sprintf("\n[...%d bytes truncated — use background:true and task_output for large commands]", len(text)-maxOutput)
	}
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("%v\n%s", err, text), IsError: true}, nil
	}
	return tool.Result{Content: text}, nil
}

// exec runs a command synchronously, delegating to the sandbox backend if set.
func (t *shellTool) exec(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if t.sb != nil {
		return t.sb.Exec(ctx, command, sandbox.ExecOpts{Dir: t.root, Timeout: timeout})
	}
	return sandbox.NewLocalBackend().Exec(ctx, command, sandbox.ExecOpts{Dir: t.root, Timeout: timeout})
}

// execStreaming runs a command with streaming output.
func (t *shellTool) execStreaming(ctx context.Context, command string, timeout time.Duration, emit func(string)) error {
	if t.sb != nil {
		return t.sb.ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: t.root, Timeout: timeout}, emit)
	}
	return sandbox.NewLocalBackend().ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: t.root, Timeout: timeout}, emit)
}
