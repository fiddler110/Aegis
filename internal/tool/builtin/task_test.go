package builtin

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scottymacleod/agentharness/internal/task"
	"github.com/scottymacleod/agentharness/internal/tool"

	_ "modernc.org/sqlite"
)

func newTaskMgr(t *testing.T) *task.Manager {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	st, err := task.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return task.NewManager(st, nil)
}

// findTool returns the tool with the given name from a slice.
func findTool(t *testing.T, tools []tool.Tool, name string) tool.Tool {
	t.Helper()
	for _, tl := range tools {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func runTool(t *testing.T, tl tool.Tool, args map[string]any) tool.Result {
	t.Helper()
	in, _ := json.Marshal(args)
	res, err := tl.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", tl.Name(), err)
	}
	return res
}

// extractID pulls the "task id <uuid>" token out of a tool's confirmation text.
func extractID(t *testing.T, s string) string {
	t.Helper()
	const marker = "task id "
	i := strings.Index(s, marker)
	if i < 0 {
		t.Fatalf("no task id in %q", s)
	}
	rest := s[i+len(marker):]
	end := strings.IndexAny(rest, ").")
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func TestTaskCreateAndPoll(t *testing.T) {
	mgr := newTaskMgr(t)
	tools := TaskTools(mgr, t.TempDir(), 30, nil)

	create := findTool(t, tools, "task_create")
	res := runTool(t, create, map[string]any{"command": "echo task-tool-ok", "title": "greet"})
	if res.IsError {
		t.Fatalf("task_create failed: %s", res.Content)
	}
	id := extractID(t, res.Content)

	// Wait for completion.
	mgr.Wait(id)

	get := findTool(t, tools, "task_get")
	g := runTool(t, get, map[string]any{"id": id})
	if g.IsError || !strings.Contains(g.Content, "done") {
		t.Fatalf("task_get = %+v", g)
	}

	out := findTool(t, tools, "task_output")
	o := runTool(t, out, map[string]any{"id": id})
	if !strings.Contains(o.Content, "task-tool-ok") {
		t.Errorf("task_output = %q, want echo output", o.Content)
	}

	list := findTool(t, tools, "task_list")
	l := runTool(t, list, nil)
	if !strings.Contains(l.Content, id) {
		t.Errorf("task_list missing id: %q", l.Content)
	}
}

func TestTaskUpdateAndStopErrors(t *testing.T) {
	mgr := newTaskMgr(t)
	tools := TaskTools(mgr, t.TempDir(), 30, nil)

	get := findTool(t, tools, "task_get")
	if res := runTool(t, get, map[string]any{"id": "missing"}); !res.IsError {
		t.Error("task_get on missing id should error")
	}

	update := findTool(t, tools, "task_update")
	if res := runTool(t, update, map[string]any{"id": "x"}); !res.IsError {
		t.Error("task_update without title should error")
	}

	stop := findTool(t, tools, "task_stop")
	if res := runTool(t, stop, map[string]any{"id": "missing"}); !res.IsError {
		t.Error("task_stop on missing id should error")
	}
}

func TestBackgroundShellTool(t *testing.T) {
	mgr := newTaskMgr(t)
	sh := newShellTool(t.TempDir(), 30, mgr, nil)
	res := runTool(t, sh, map[string]any{"command": "echo bg-shell", "background": true})
	if res.IsError {
		t.Fatalf("background shell: %s", res.Content)
	}
	id := extractID(t, res.Content)
	done, ok := mgr.Wait(id)
	if !ok || done.State != task.StateDone {
		t.Fatalf("task state = %+v", done)
	}
	if !strings.Contains(done.Output, "bg-shell") {
		t.Errorf("output = %q", done.Output)
	}
}

func TestBackgroundShellWithoutManager(t *testing.T) {
	sh := newShellTool(t.TempDir(), 30, nil, nil)
	res := runTool(t, sh, map[string]any{"command": "echo x", "background": true})
	if !res.IsError {
		t.Error("background shell without a manager should error")
	}
}

func TestBackgroundAgentTool(t *testing.T) {
	mgr := newTaskMgr(t)
	b := &fakeBackend{root: t.TempDir(), output: "sub-agent answer"}
	at := NewAgentTool(b, mgr)
	res, err := at.Execute(context.Background(), json.RawMessage(`{"prompt":"do it","subagent_type":"general","background":true}`))
	if err != nil || res.IsError {
		t.Fatalf("background agent: %v %+v", err, res)
	}
	id := extractID(t, res.Content)
	done, ok := mgr.Wait(id)
	if !ok || done.State != task.StateDone {
		t.Fatalf("task = %+v", done)
	}
	if done.Output != "sub-agent answer" {
		t.Errorf("output = %q, want %q", done.Output, "sub-agent answer")
	}
}

func TestBackgroundAgentWithoutManager(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "x"}
	at := NewAgentTool(b, nil)
	res, _ := at.Execute(context.Background(), json.RawMessage(`{"prompt":"x","background":true}`))
	if !res.IsError {
		t.Error("background agent without a manager should error")
	}
}
