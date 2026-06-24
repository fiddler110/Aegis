package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/scottymacleod/agentharness/internal/tool"
)

type shellTool struct {
	root       string
	timeoutSec int
}

func newShellTool(root string, timeoutSec int) *shellTool {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &shellTool{root: root, timeoutSec: timeoutSec}
}

func (t *shellTool) Name() string                { return "shell" }
func (t *shellTool) Capability() tool.Capability { return tool.CapExecute }
func (t *shellTool) Description() string {
	return "Run a shell command in the workspace directory and return combined stdout/stderr. Bounded by a timeout."
}
func (t *shellTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"command":{"type":"string","description":"the command line to run"},"timeout_sec":{"type":"integer","description":"optional per-call timeout override"}},"required":["command"]}`)
}

func (t *shellTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Command    string `json:"command"`
		TimeoutSec int    `json:"timeout_sec"`
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

// shellCommand returns the platform shell binary and the argument list needed
// to execute command.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}
