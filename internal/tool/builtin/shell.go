package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
)

type shellTool struct {
	root       string
	timeoutSec int
	mgr        *task.Manager   // optional; enables background:true
	sb         sandbox.Backend // optional; nil = inline local exec (legacy path)
}

// maxShellTimeoutSec is the ceiling a caller-supplied timeout_sec is clamped
// to. Package-level rather than a const inside Execute so
// TestToolTimeoutsStayUnderTheStallBound can enumerate it alongside every other
// per-call bound — the same reason maxShellOutput is package-level, and the
// same failure mode: a bound no test can name is a bound that drifts above the
// stall detector without anything noticing.
const maxShellTimeoutSec = 600

func newShellTool(root string, timeoutSec int, mgr *task.Manager, sb sandbox.Backend) *shellTool {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &shellTool{root: root, timeoutSec: timeoutSec, mgr: mgr, sb: sb}
}

func (t *shellTool) Name() string                { return "shell" }
func (t *shellTool) Capability() tool.Capability { return tool.CapExecute }

// CapabilityFor implements tool.CapabilityOverrider (P25.4c): a narrow
// allowlist of read-only commands (ls, cat, git status/log/diff, …) whose
// arguments stay within root (P32.1) is gated as CapRead instead of the
// tool's usual CapExecute, so it no longer needs a full execute approval in
// build mode and is allowed outright (not silently denied) under the
// plan-mode read gate. The static Capability() above is unchanged and still
// governs anything readOnlyShellCommand doesn't recognize as safe — which
// now includes any command reading outside the workspace root, so it falls
// back to requiring the normal execute approval instead of being silently
// auto-allowed. CapabilityOverrider carries no context, so this uses the
// tool's construction-time root rather than a session-scoped override.
func (t *shellTool) CapabilityFor(input json.RawMessage) tool.Capability {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &args) == nil {
		// P67.8: the classifier answers with a capability, not a bool — a
		// recognized `gh` subcommand is read-only on the *filesystem* but is
		// still network egress, and plan mode Asks for CapNetwork where it
		// silently Allows CapRead.
		if cap, ok := classifyShellCommand(t.root, args.Command); ok {
			return cap
		}
	}
	return tool.CapExecute
}

// usesPowerShell reports whether this tool's commands actually reach
// PowerShell. A Windows host normally means they do — but a container backend
// runs them inside a Linux container, where /bin/sh and the Unix commands are
// correct. The distinction was cosmetic while it only shaped the tool's
// description; it stops being cosmetic once a POSIX command is refused.
func (t *shellTool) usesPowerShell() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return t.sb == nil || !strings.HasPrefix(t.sb.Name(), "container:")
}

func (t *shellTool) Description() string {
	if t.usesPowerShell() {
		return "Run a PowerShell command in the workspace directory and return combined stdout/stderr. Commands execute via: powershell -NoProfile -NonInteractive -Command <command>. Use PowerShell syntax — Unix commands (ls, cat, grep, find, rm, chmod, etc.) are not available; use Get-ChildItem, Get-Content, Select-String, Remove-Item, etc."
	}
	return "Run a shell command in the workspace directory and return combined stdout/stderr. Commands execute via /bin/sh -c. Bounded by a configurable timeout."
}
func (t *shellTool) InputSchema() json.RawMessage {
	if t.usesPowerShell() {
		return schema(`{"type":"object","properties":{"command":{"type":"string","description":"PowerShell command to run. Use PowerShell syntax (Get-ChildItem, Get-Content, Select-String, Remove-Item, etc.) — Unix commands do not work in PowerShell."},"timeout_sec":{"type":"integer","description":"optional per-call timeout override in seconds"},"background":{"type":"boolean","description":"run as a detached background job and return a task id immediately instead of blocking"}},"required":["command"]}`)
	}
	return schema(`{"type":"object","properties":{"command":{"type":"string","description":"the shell command to run via /bin/sh -c"},"timeout_sec":{"type":"integer","description":"optional per-call timeout override in seconds"},"background":{"type":"boolean","description":"run as a detached background job and return a task id immediately instead of blocking"}},"required":["command"]}`)
}

func (t *shellTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"output":{"type":"string","description":"combined stdout and stderr"},"exit_code":{"type":"integer"}},"required":["output","exit_code"]}`)
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
	// Answer a POSIX-on-PowerShell mistake with the fix, before the shell
	// answers it with a parse error only a PowerShell user could decode.
	if hint := checkPosixOnWindows(args.Command, t.usesPowerShell()); hint != "" {
		return tool.Result{Content: hint, IsError: true}, nil
	}

	timeout := time.Duration(t.timeoutSec) * time.Second
	if args.TimeoutSec > 0 {
		timeout = time.Duration(min(args.TimeoutSec, maxShellTimeoutSec)) * time.Second
	}
	root := effectiveRoot(ctx, t.root)

	if args.Background {
		if t.mgr == nil {
			return tool.Result{Content: "background jobs are not available in this context", IsError: true}, nil
		}
		tk, err := t.mgr.Start(task.Spec{Kind: "shell", Title: truncateTitle(args.Command)}, func(jobCtx context.Context, emit func(string)) (string, error) {
			return "", t.execStreaming(jobCtx, root, args.Command, timeout, emit)
		})
		if err != nil {
			return tool.Result{Content: "shell: " + err.Error(), IsError: true}, nil
		}
		return tool.Result{Content: fmt.Sprintf("Started background shell job (task id %s). Poll with task_get; read output with task_output; cancel with task_stop.", tk.ID)}, nil
	}

	// Foreground execution. A command not classified read-only might write
	// files directly (bypassing write_file/edit_file's own checkpoint
	// capture), so bracket it with best-effort git-status-based capture —
	// see shell_checkpoint.go — unless the command is already known safe.
	var text string
	var err error
	if _, classified := classifyShellCommand(root, args.Command); classified {
		// Any classified command is non-mutating (CapRead or, for gh,
		// CapNetwork), so there is nothing for the checkpoint snapshot to
		// capture.
		text, err = t.exec(ctx, root, args.Command, timeout)
	} else {
		text, err = captureShellWrites(ctx, checkpoint.SnapshotterFrom(ctx), root, func() (string, error) {
			return t.exec(ctx, root, args.Command, timeout)
		})
	}
	// P64.3: was 200 << 10 (200 KiB), which tokenest prices at 51,200 tokens —
	// four to twelve times the whole context window under the local profile, so
	// it could never bind before the window did. 24 KiB is ~6,144 estimated
	// tokens, the same value the skill-script cap (maxSkillScriptOutput) picked
	// for the same class of thing. See the posture table in truncate.go.
	//
	// Tail, not head: command output is a log, and a failing build prints its
	// errors last. The old head-keeping slice was never argued for — it is what
	// `text[:maxOutput]` does. The dropped bytes are recoverable (P64.1 spills
	// them), so this only decides which end gets the inline budget.
	text = SpillTail(ctx, root, "shell", text, maxShellOutput, "use background:true and task_output for large commands")
	if err != nil {
		content := fmt.Sprintf("%v\n%s", err, text)
		if hint := interpreterHint(args.Command); hint != "" {
			content += "\n" + hint
		}
		return tool.Result{Content: content, IsError: true}, nil
	}
	return tool.Result{Content: text}, nil
}

// scriptExtInterpreters maps a scripting file extension to the interpreter
// commonly used to run it, for interpreterHint's suggestion (P39.2).
var scriptExtInterpreters = map[string]string{
	".py": "python", ".sh": "sh", ".js": "node", ".rb": "ruby",
}

// knownInterpreterPrefixes are first-token values that already indicate the
// command runs through an interpreter, so no hint is needed.
var knownInterpreterPrefixes = []string{"python", "python3", "bash", "sh", "node", "ruby"}

// interpreterHint returns a "did you mean to run this with an interpreter"
// suggestion when command's first token looks like a bare script path with a
// known scripting extension and isn't already prefixed by a known
// interpreter, or "" otherwise. Small local models were observed in live
// testing (P38.1) invoking a bare script path (e.g. `recon.py`) as if it were
// an executable, which fails differently on Windows (no shebang support)
// than Unix. This only appends a hint on the failure path — it never blocks
// or rewrites the command pre-execution, so the shell tool stays permissive.
func interpreterHint(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	first := fields[0]
	for _, p := range knownInterpreterPrefixes {
		if strings.EqualFold(first, p) {
			return ""
		}
	}
	interp, ok := scriptExtInterpreters[strings.ToLower(filepath.Ext(first))]
	if !ok {
		return ""
	}
	return fmt.Sprintf("(did you mean to run this with an interpreter, e.g. `%s %s`?)", interp, first)
}

// exec runs a command synchronously, delegating to the sandbox backend if set.
func (t *shellTool) exec(ctx context.Context, root, command string, timeout time.Duration) (string, error) {
	if t.sb != nil {
		return t.sb.Exec(ctx, command, sandbox.ExecOpts{Dir: root, Timeout: timeout})
	}
	return sandbox.NewLocalBackend().Exec(ctx, command, sandbox.ExecOpts{Dir: root, Timeout: timeout})
}

// execStreaming runs a command with streaming output.
func (t *shellTool) execStreaming(ctx context.Context, root, command string, timeout time.Duration, emit func(string)) error {
	if t.sb != nil {
		return t.sb.ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: root, Timeout: timeout}, emit)
	}
	return sandbox.NewLocalBackend().ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: root, Timeout: timeout}, emit)
}
