package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/scottymacleod/agentharness/internal/sandbox"
	"github.com/scottymacleod/agentharness/internal/task"
	"github.com/scottymacleod/agentharness/internal/tool"
)

// TaskTools returns the background-job management tools, all backed by the same
// task.Manager. They let the model launch long work (via task_create or
// `shell` with background:true, or the `agent` tool in background mode), keep
// going, and poll for results later. shellTimeoutSec bounds a task_create job
// (0 -> default).
func TaskTools(mgr *task.Manager, root string, shellTimeoutSec int, sb sandbox.Backend) []tool.Tool {
	if shellTimeoutSec <= 0 {
		shellTimeoutSec = 120
	}
	return []tool.Tool{
		&taskCreateTool{mgr: mgr, root: root, timeoutSec: shellTimeoutSec, sb: sb},
		&taskListTool{mgr: mgr},
		&taskGetTool{mgr: mgr},
		&taskOutputTool{mgr: mgr},
		&taskUpdateTool{mgr: mgr},
		&taskStopTool{mgr: mgr},
	}
}

// --- task_create ---

type taskCreateTool struct {
	mgr        *task.Manager
	root       string
	timeoutSec int
	sb         sandbox.Backend
}

func (t *taskCreateTool) Name() string                { return "task_create" }
func (t *taskCreateTool) Capability() tool.Capability { return tool.CapExecute }
func (t *taskCreateTool) Description() string {
	return "Run a shell command as a background job and return its task id immediately, " +
		"instead of blocking until it finishes. Use for long-running commands (builds, " +
		"test suites, servers). Poll progress with task_get/task_output; cancel with task_stop."
}
func (t *taskCreateTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"command":{"type":"string","description":"the command line to run in the background"},"title":{"type":"string","description":"optional short label for the job"},"timeout_sec":{"type":"integer","description":"optional max runtime in seconds"}},"required":["command"]}`)
}
func (t *taskCreateTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Command    string `json:"command"`
		Title      string `json:"title"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Command) == "" {
		return tool.Result{Content: "command is required", IsError: true}, nil
	}
	title := args.Title
	if title == "" {
		title = truncateTitle(args.Command)
	}
	timeout := time.Duration(t.timeoutSec) * time.Second
	if args.TimeoutSec > 0 {
		timeout = time.Duration(min(args.TimeoutSec, 600)) * time.Second
	}
	sb := t.sb
	if sb == nil {
		sb = sandbox.NewLocalBackend()
	}
	tk, err := t.mgr.Start(task.Spec{Kind: "shell", Title: title}, func(ctx context.Context, emit func(string)) (string, error) {
		return "", sb.ExecStreaming(ctx, args.Command, sandbox.ExecOpts{Dir: t.root, Timeout: timeout}, emit)
	})
	if err != nil {
		return tool.Result{Content: "task_create: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("Started background job %s (task id %s). Poll with task_get; read output with task_output; cancel with task_stop.", title, tk.ID)}, nil
}

// --- task_list ---

type taskListTool struct{ mgr *task.Manager }

func (t *taskListTool) Name() string                { return "task_list" }
func (t *taskListTool) Capability() tool.Capability { return tool.CapRead }
func (t *taskListTool) Description() string {
	return "List background jobs (newest first) with their id, kind, state, and title."
}
func (t *taskListTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{}}`)
}
func (t *taskListTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	tasks, err := t.mgr.List("")
	if err != nil {
		return tool.Result{Content: "task_list: " + err.Error(), IsError: true}, nil
	}
	if len(tasks) == 0 {
		return tool.Result{Content: "(no background jobs)"}, nil
	}
	var sb strings.Builder
	for _, tk := range tasks {
		fmt.Fprintf(&sb, "%s  [%s]  %s  %s\n", tk.ID, tk.State, tk.Kind, tk.Title)
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}

// --- task_get ---

type taskGetTool struct{ mgr *task.Manager }

func (t *taskGetTool) Name() string                { return "task_get" }
func (t *taskGetTool) Capability() tool.Capability { return tool.CapRead }
func (t *taskGetTool) Description() string {
	return "Get a background job's status and a tail of its output by task id. " +
		"Use task_output for the full output."
}
func (t *taskGetTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"string","description":"the task id"}},"required":["id"]}`)
}
func (t *taskGetTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	id, errRes := taskID(input)
	if errRes != nil {
		return *errRes, nil
	}
	tk, err := t.mgr.Get(id)
	if err != nil {
		return tool.Result{Content: "task_get: " + err.Error(), IsError: true}, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "id: %s\nstate: %s\nkind: %s\ntitle: %s\n", tk.ID, tk.State, tk.Kind, tk.Title)
	if tk.Error != "" {
		fmt.Fprintf(&sb, "error: %s\n", tk.Error)
	}
	sb.WriteString("--- output (tail) ---\n")
	sb.WriteString(tail(tk.Output, 2000))
	return tool.Result{Content: sb.String()}, nil
}

// --- task_output ---

type taskOutputTool struct{ mgr *task.Manager }

func (t *taskOutputTool) Name() string                { return "task_output" }
func (t *taskOutputTool) Capability() tool.Capability { return tool.CapRead }
func (t *taskOutputTool) Description() string {
	return "Return the full accumulated output of a background job by task id."
}
func (t *taskOutputTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"string","description":"the task id"}},"required":["id"]}`)
}
func (t *taskOutputTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	id, errRes := taskID(input)
	if errRes != nil {
		return *errRes, nil
	}
	tk, err := t.mgr.Get(id)
	if err != nil {
		return tool.Result{Content: "task_output: " + err.Error(), IsError: true}, nil
	}
	if tk.Output == "" {
		return tool.Result{Content: "(no output yet)"}, nil
	}
	return tool.Result{Content: tk.Output}, nil
}

// --- task_update ---

type taskUpdateTool struct{ mgr *task.Manager }

func (t *taskUpdateTool) Name() string                { return "task_update" }
func (t *taskUpdateTool) Capability() tool.Capability { return tool.CapWrite }
func (t *taskUpdateTool) Description() string {
	return "Rename a background job (update its title) by task id."
}
func (t *taskUpdateTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"string","description":"the task id"},"title":{"type":"string","description":"the new title"}},"required":["id","title"]}`)
}
func (t *taskUpdateTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.ID == "" || args.Title == "" {
		return tool.Result{Content: "id and title are required", IsError: true}, nil
	}
	if err := t.mgr.SetTitle(args.ID, args.Title); err != nil {
		return tool.Result{Content: "task_update: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: "updated"}, nil
}

// --- task_stop ---

type taskStopTool struct{ mgr *task.Manager }

func (t *taskStopTool) Name() string                { return "task_stop" }
func (t *taskStopTool) Capability() tool.Capability { return tool.CapExecute }
func (t *taskStopTool) Description() string {
	return "Cancel a running background job by task id. No-op if already finished."
}
func (t *taskStopTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"string","description":"the task id"}},"required":["id"]}`)
}
func (t *taskStopTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	id, errRes := taskID(input)
	if errRes != nil {
		return *errRes, nil
	}
	if err := t.mgr.Stop(id); err != nil {
		return tool.Result{Content: "task_stop: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: "stopped " + id}, nil
}

// --- helpers ---

// taskID extracts the required {"id": ...} argument, returning a friendly error
// Result (not a Go error) if missing/invalid.
func taskID(input json.RawMessage) (string, *tool.Result) {
	var args struct {
		ID string `json:"id"`
	}
	if err := parseArgs(input, &args); err != nil {
		return "", &tool.Result{Content: err.Error(), IsError: true}
	}
	if args.ID == "" {
		return "", &tool.Result{Content: "id is required", IsError: true}
	}
	return args.ID, nil
}

func truncateTitle(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 48 {
		return s[:48] + "…"
	}
	return s
}

// tail returns the last n bytes of s, prefixed with an elision marker if it was
// truncated.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…(truncated)…\n" + s[len(s)-n:]
}
