package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/scottymacleod/agentharness/internal/task"
	"github.com/scottymacleod/agentharness/internal/tool"
)

type shellTool struct {
	root       string
	timeoutSec int
	mgr        *task.Manager // optional; enables background:true
}

func newShellTool(root string, timeoutSec int, mgr *task.Manager) *shellTool {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &shellTool{root: root, timeoutSec: timeoutSec, mgr: mgr}
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

	timeout := time.Duration(t.timeoutSec) * time.Second
	if args.TimeoutSec > 0 {
		timeout = time.Duration(args.TimeoutSec) * time.Second
	}

	if args.Background {
		if t.mgr == nil {
			return tool.Result{Content: "background jobs are not available in this context", IsError: true}, nil
		}
		tk, err := t.mgr.Start(task.Spec{Kind: "shell", Title: truncateTitle(args.Command)}, func(jobCtx context.Context, emit func(string)) (string, error) {
			return "", runShellStreaming(jobCtx, t.root, args.Command, timeout, emit)
		})
		if err != nil {
			return tool.Result{Content: "shell: " + err.Error(), IsError: true}, nil
		}
		return tool.Result{Content: fmt.Sprintf("Started background shell job (task id %s). Poll with task_get; read output with task_output; cancel with task_stop.", tk.ID)}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name, shellArgs := shellCommand(args.Command)
	cmd := exec.CommandContext(runCtx, name, shellArgs...)
	cmd.Dir = t.root

	out, err := cmd.CombinedOutput()
	text := string(out)
	if runCtx.Err() == context.DeadlineExceeded {
		return tool.Result{Content: fmt.Sprintf("command timed out after %s\n%s", timeout, text), IsError: true}, nil
	}
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("exit error: %v\n%s", err, text), IsError: true}, nil
	}
	if strings.TrimSpace(text) == "" {
		text = "(no output)"
	}
	return tool.Result{Content: text}, nil
}

// runShellStreaming runs command in root, streaming combined stdout/stderr to
// emit as it arrives, and kills the process if ctx is cancelled (the "killable
// background job" path). It returns a timeout/exit error for the task layer.
func runShellStreaming(ctx context.Context, root, command string, timeout time.Duration, emit func(string)) error {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name, shellArgs := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, shellArgs...)
	cmd.Dir = root
	w := emitWriter{emit: emit}
	cmd.Stdout = w
	cmd.Stderr = w

	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command timed out after %s", timeout)
	}
	if ctx.Err() != nil {
		return ctx.Err() // cancelled via task_stop / shutdown
	}
	if err != nil {
		return fmt.Errorf("exit error: %w", err)
	}
	return nil
}

// emitWriter adapts a streaming emit callback to io.Writer. exec may call Write
// from separate stdout/stderr copier goroutines, so emit must be concurrency
// safe (the task buffer is).
type emitWriter struct{ emit func(string) }

func (w emitWriter) Write(p []byte) (int, error) {
	w.emit(string(p))
	return len(p), nil
}

// shellCommand returns the platform shell binary and the argument list needed
// to execute command.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}
