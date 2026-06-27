package builtin

import (
	"context"
	"encoding/json"
	"fmt"
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
	return "Run a shell command in the workspace directory and return combined stdout/stderr. Bounded by a timeout."
}
func (t *shellTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"command":{"type":"string","description":"the command line to run"},"timeout_sec":{"type":"integer","description":"optional per-call timeout override"},"background":{"type":"boolean","description":"run as a detached background job and return a task id immediately instead of blocking"}},"required":["command"]}`)
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
