package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/scottymacleod/aegis/internal/tool"
)

// TodoList is a structured task list the model maintains to track its plan.
// Thread-safe; shared across all tool calls in a session.
type TodoList struct {
	mu    sync.Mutex
	items []TodoItem
	next  int
}

// TodoItem is a single task in the plan.
type TodoItem struct {
	ID     int    `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"` // "pending", "in_progress", "done"
}

// NewTodoList creates an empty list.
func NewTodoList() *TodoList { return &TodoList{} }

// TodoTools returns the planning tools backed by the given list.
func TodoTools(list *TodoList) []tool.Tool {
	return []tool.Tool{
		&todoAddTool{list: list},
		&todoUpdateTool{list: list},
		&todoListTool{list: list},
	}
}

// Items returns a snapshot of all items.
func (tl *TodoList) Items() []TodoItem {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := make([]TodoItem, len(tl.items))
	copy(out, tl.items)
	return out
}

const maxTodoItems = 500

func (tl *TodoList) add(text string) int {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if len(tl.items) >= maxTodoItems {
		// Prune oldest completed items to make room.
		pruned := tl.items[:0]
		for _, item := range tl.items {
			if item.Status != "done" || len(pruned) < maxTodoItems-1 {
				pruned = append(pruned, item)
			}
		}
		tl.items = pruned
	}
	tl.next++
	tl.items = append(tl.items, TodoItem{ID: tl.next, Text: text, Status: "pending"})
	return tl.next
}

func (tl *TodoList) update(id int, status string) error {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	for i := range tl.items {
		if tl.items[i].ID == id {
			tl.items[i].Status = status
			return nil
		}
	}
	return fmt.Errorf("todo item %d not found", id)
}

func (tl *TodoList) format() string {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if len(tl.items) == 0 {
		return "(no items)"
	}
	var sb strings.Builder
	for _, item := range tl.items {
		marker := "[ ]"
		switch item.Status {
		case "in_progress":
			marker = "[~]"
		case "done":
			marker = "[x]"
		}
		fmt.Fprintf(&sb, "%s %d. %s\n", marker, item.ID, item.Text)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// --- todo_add ---

type todoAddTool struct{ list *TodoList }

func (t *todoAddTool) Name() string                { return "todo_add" }
func (t *todoAddTool) Capability() tool.Capability { return tool.CapWrite }
func (t *todoAddTool) Description() string {
	return "Add a task to the plan/todo list. Use this to decompose work into " +
		"trackable steps before starting."
}
func (t *todoAddTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"text":{"type":"string","description":"description of the task"}},"required":["text"]}`)
}
func (t *todoAddTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Text) == "" {
		return tool.Result{Content: "text is required", IsError: true}, nil
	}
	id := t.list.add(args.Text)
	return tool.Result{Content: fmt.Sprintf("added todo #%d", id)}, nil
}

// --- todo_update ---

type todoUpdateTool struct{ list *TodoList }

func (t *todoUpdateTool) Name() string                { return "todo_update" }
func (t *todoUpdateTool) Capability() tool.Capability { return tool.CapWrite }
func (t *todoUpdateTool) Description() string {
	return "Update a todo item's status. Status must be one of: pending, in_progress, done."
}
func (t *todoUpdateTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"integer","description":"the todo item id"},"status":{"type":"string","enum":["pending","in_progress","done"],"description":"new status"}},"required":["id","status"]}`)
}
func (t *todoUpdateTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	switch args.Status {
	case "pending", "in_progress", "done":
	default:
		return tool.Result{Content: "status must be pending, in_progress, or done", IsError: true}, nil
	}
	if err := t.list.update(args.ID, args.Status); err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("todo #%d → %s", args.ID, args.Status)}, nil
}

// --- todo_list ---

type todoListTool struct{ list *TodoList }

func (t *todoListTool) Name() string                { return "todo_list" }
func (t *todoListTool) Capability() tool.Capability { return tool.CapRead }
func (t *todoListTool) Description() string {
	return "Show the current plan/todo list with status markers: [ ] pending, [~] in_progress, [x] done."
}
func (t *todoListTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{}}`)
}
func (t *todoListTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: t.list.format()}, nil
}
