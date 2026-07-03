package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
)

// TeamTools returns the agent-team coordination tools (P5.1): a shared task list
// teammates claim/complete, and direct peer-to-peer messaging. They are
// registered as deferred tools since they only matter during multi-agent work.
func TeamTools(tasks *swarm.TaskList, mailboxRoot string) []tool.Tool {
	return []tool.Tool{
		&teamTaskAddTool{tasks: tasks},
		&teamTaskListTool{tasks: tasks},
		&teamTaskClaimTool{tasks: tasks},
		&teamTaskCompleteTool{tasks: tasks},
		&teamSendTool{root: mailboxRoot},
		&teamInboxTool{root: mailboxRoot},
	}
}

const defaultTeam = "default"

func teamOrDefault(s string) string {
	if strings.TrimSpace(s) == "" {
		return defaultTeam
	}
	return s
}

// --- team_task_add ---

type teamTaskAddTool struct{ tasks *swarm.TaskList }

func (t *teamTaskAddTool) Name() string                { return "team_task_add" }
func (t *teamTaskAddTool) Capability() tool.Capability { return tool.CapWrite }
func (t *teamTaskAddTool) Description() string {
	return "Add a task to the team's shared task list so any teammate can claim and complete it."
}
func (t *teamTaskAddTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"description":{"type":"string","description":"what needs to be done"},"team":{"type":"string","description":"team name (default: \"default\")"}},"required":["description"]}`)
}
func (t *teamTaskAddTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Description string `json:"description"`
		Team        string `json:"team"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Description) == "" {
		return tool.Result{Content: "description is required", IsError: true}, nil
	}
	id, err := t.tasks.Add(ctx, teamOrDefault(args.Team), args.Description)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("could not add task: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("added task #%d to team %q", id, teamOrDefault(args.Team))}, nil
}

// --- team_task_list ---

type teamTaskListTool struct{ tasks *swarm.TaskList }

func (t *teamTaskListTool) Name() string                { return "team_task_list" }
func (t *teamTaskListTool) Capability() tool.Capability { return tool.CapRead }
func (t *teamTaskListTool) Description() string {
	return "List the team's shared tasks with their status (open, claimed, completed) and owner."
}
func (t *teamTaskListTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"team":{"type":"string","description":"team name (default: \"default\")"}}}`)
}
func (t *teamTaskListTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Team string `json:"team"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	list, err := t.tasks.List(ctx, teamOrDefault(args.Team))
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("could not list tasks: %v", err), IsError: true}, nil
	}
	if len(list) == 0 {
		return tool.Result{Content: "no tasks"}, nil
	}
	var sb strings.Builder
	for _, tk := range list {
		fmt.Fprintf(&sb, "#%d [%s] %s", tk.ID, tk.Status, tk.Description)
		if tk.Owner != "" {
			fmt.Fprintf(&sb, " (owner: %s)", tk.Owner)
		}
		sb.WriteString("\n")
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}

// --- team_task_claim ---

type teamTaskClaimTool struct{ tasks *swarm.TaskList }

func (t *teamTaskClaimTool) Name() string                { return "team_task_claim" }
func (t *teamTaskClaimTool) Capability() tool.Capability { return tool.CapWrite }
func (t *teamTaskClaimTool) Description() string {
	return "Claim the oldest open task from the team's shared list. Pass your agent name as owner. Returns the claimed task, or reports none available."
}
func (t *teamTaskClaimTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"owner":{"type":"string","description":"your agent name, recorded as the task owner"},"team":{"type":"string","description":"team name (default: \"default\")"}},"required":["owner"]}`)
}
func (t *teamTaskClaimTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Owner string `json:"owner"`
		Team  string `json:"team"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Owner) == "" {
		return tool.Result{Content: "owner is required", IsError: true}, nil
	}
	tk, err := t.tasks.Claim(ctx, teamOrDefault(args.Team), args.Owner)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("could not claim task: %v", err), IsError: true}, nil
	}
	if tk == nil {
		return tool.Result{Content: "no open tasks to claim"}, nil
	}
	return tool.Result{Content: fmt.Sprintf("claimed task #%d: %s", tk.ID, tk.Description)}, nil
}

// --- team_task_complete ---

type teamTaskCompleteTool struct{ tasks *swarm.TaskList }

func (t *teamTaskCompleteTool) Name() string                { return "team_task_complete" }
func (t *teamTaskCompleteTool) Capability() tool.Capability { return tool.CapWrite }
func (t *teamTaskCompleteTool) Description() string {
	return "Mark a shared team task completed by its id."
}
func (t *teamTaskCompleteTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"integer","description":"the task id to complete"}},"required":["id"]}`)
}
func (t *teamTaskCompleteTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		ID int64 `json:"id"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if err := t.tasks.Complete(ctx, args.ID); err != nil {
		return tool.Result{Content: fmt.Sprintf("could not complete task: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("completed task #%d", args.ID)}, nil
}

// --- team_send (peer messaging) ---

type teamSendTool struct{ root string }

func (t *teamSendTool) Name() string                { return "team_send" }
func (t *teamSendTool) Capability() tool.Capability { return tool.CapWrite }
func (t *teamSendTool) Description() string {
	return "Send a direct message to a teammate's inbox. Provide the recipient's agent name, the team, your name (from), and the message text."
}
func (t *teamSendTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"to":{"type":"string","description":"recipient agent name"},"team":{"type":"string","description":"team name (default: \"default\")"},"from":{"type":"string","description":"your agent name"},"text":{"type":"string","description":"the message"}},"required":["to","text"]}`)
}
func (t *teamSendTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		To   string `json:"to"`
		Team string `json:"team"`
		From string `json:"from"`
		Text string `json:"text"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.To) == "" || strings.TrimSpace(args.Text) == "" {
		return tool.Result{Content: "to and text are required", IsError: true}, nil
	}
	recipient := swarm.NewIdentity(args.To, teamOrDefault(args.Team), "")
	msg := swarm.Message{Type: swarm.MsgPeer, Sender: args.From, Text: args.Text}
	if err := swarm.SendTo(t.root, recipient, msg); err != nil {
		return tool.Result{Content: fmt.Sprintf("could not send message: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("sent message to %s", recipient.AgentID)}, nil
}

// --- team_inbox ---

type teamInboxTool struct{ root string }

func (t *teamInboxTool) Name() string                { return "team_inbox" }
func (t *teamInboxTool) Capability() tool.Capability { return tool.CapRead }
func (t *teamInboxTool) Description() string {
	return "Read messages sent to a teammate's inbox. Provide the agent name and team. Returns messages in chronological order and marks them read."
}
func (t *teamInboxTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"agent":{"type":"string","description":"the agent whose inbox to read (your name)"},"team":{"type":"string","description":"team name (default: \"default\")"},"unread_only":{"type":"boolean","description":"only return unread messages (default true)"}},"required":["agent"]}`)
}
func (t *teamInboxTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Agent      string `json:"agent"`
		Team       string `json:"team"`
		UnreadOnly *bool  `json:"unread_only"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Agent) == "" {
		return tool.Result{Content: "agent is required", IsError: true}, nil
	}
	id := swarm.NewIdentity(args.Agent, teamOrDefault(args.Team), "")
	mb, err := swarm.OpenMailbox(t.root, id)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("could not open inbox: %v", err), IsError: true}, nil
	}
	unreadOnly := args.UnreadOnly == nil || *args.UnreadOnly
	msgs, err := mb.ReadAll(unreadOnly)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("could not read inbox: %v", err), IsError: true}, nil
	}
	if len(msgs) == 0 {
		return tool.Result{Content: "inbox empty"}, nil
	}
	var sb strings.Builder
	for _, m := range msgs {
		sender := m.Sender
		if sender == "" {
			sender = "unknown"
		}
		fmt.Fprintf(&sb, "[%s from %s] %s\n", m.Type, sender, m.Text)
		_ = mb.MarkRead(m.ID)
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}
