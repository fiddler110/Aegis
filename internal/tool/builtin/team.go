package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/trust"
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

// PollExempt marks team_inbox as a poll (P53.2). A swarm teammate blocked on a
// reply has no callback to wait on — the mailbox is file-based and the only way
// to learn a message arrived is to look again — so repeatedly reading the same
// inbox while the sender is still working is the intended coordination pattern,
// not a stuck agent. Only team_inbox is exempted, not the rest of the team_*
// family: team_task_list/team_task_claim also observe shared state, but they
// mutate or drive work, and a claim loop that never completes anything is a
// genuine stall the detector should still catch.
func (t *teamInboxTool) PollExempt(json.RawMessage) bool { return true }

var _ tool.PollExempter = (*teamInboxTool)(nil)

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
	return tool.Result{Content: formatInbox(mb, id.AgentID, msgs, unreadOnly)}, nil
}

// maxInboxResult bounds one team_inbox result's message body in bytes, before
// the provenance envelope is added (P70.2).
//
// The value and the end are web_fetch's, and for the same reasons (see the
// posture table in truncate.go): this is untrusted prose whose point is at the
// top, and 20000 bytes is ~5.0k estimated tokens, small enough to bind before
// a local context window does. Two properties of this site differ from a page
// fetch and are handled below rather than by the shared helper:
//
//   - The remainder is deliberately NOT spilled, the same exclusion web_fetch
//     takes. Spilling would write the *unwrapped* overflow to a workspace file
//     that read_file returns with no marker at all, turning a context-budget
//     feature into exactly the laundering path this item closes.
//   - A message that does not fit is left unread rather than dropped, so the
//     budget costs a second call rather than the message. Only a single
//     over-cap message loses bytes, and its notice says so.
const maxInboxResult = 20000

// formatInbox renders messages into one model-facing result: each message is
// individually capped, the batch is bounded by maxInboxResult, and the whole
// body is wrapped as untrusted content. Messages actually included are marked
// read; ones deferred by the budget are not, so the next call returns them.
func formatInbox(mb *swarm.Mailbox, agent string, msgs []swarm.Message, unreadOnly bool) string {
	var sb strings.Builder
	deferred := 0
	for i, m := range msgs {
		sender := m.Sender
		if sender == "" {
			sender = "unknown"
		}
		header := fmt.Sprintf("[%s from %s] ", m.Type, sender)
		// The header and the separating newline come out of the same budget,
		// so one over-cap message can never push the body past the cap.
		text, _ := TruncateHead(m.Text, maxInboxResult-len(header)-1, "ask the sender to resend the remainder as further messages")
		entry := header + text + "\n"
		if sb.Len() > 0 && sb.Len()+len(entry) > maxInboxResult {
			deferred = len(msgs) - i
			break
		}
		sb.WriteString(entry)
		_ = mb.MarkRead(m.ID)
	}
	body := strings.TrimRight(sb.String(), "\n")
	if deferred > 0 {
		recovery := "read the inbox again to receive them"
		if !unreadOnly {
			recovery = "read the inbox again with unread_only:true to receive them"
		}
		body += fmt.Sprintf("\n[%d further message(s) withheld: one team_inbox result is capped at %d bytes. They are left unread — %s.]",
			deferred, maxInboxResult, recovery)
	}
	return wrapInboxMessages(agent, body)
}

// wrapInboxMessages marks a mailbox batch as untrusted content before it
// re-enters the model's context (P70.2).
//
// The mailbox is a file-backed queue under the shared data dir, writable by
// any peer agent and by any local process with file access. A teammate that
// read a poisoned web page, an MCP result or a workspace file can relay those
// bytes to a peer through team_send, where they previously arrived as plain,
// trusted-looking text — laundering the provenance marking web_fetch and MCP
// results carry at ingestion. Aegis takes the zero-trust reading: content in
// the mailbox did not originate with the agent that sent it, so the mailbox
// wraps too.
//
// The heuristic injection scan is off, matching the persona/skill and network
// scan-report sites: it is a config-gated opt-in everywhere it is on, there is
// no per-mailbox knob to gate it, and peer coordination prose ("ignore the
// earlier instructions in task #3") is exactly the text its keyword patterns
// over-fire on. The envelope — this is data, not instructions — is what the
// provenance gap asked for; the ingestion points remain where scanning happens.
func wrapInboxMessages(agent, body string) string {
	return trust.Wrap(
		"team_untrusted_output",
		[][2]string{{"inbox", agent}},
		"another agent's mailbox (a teammate can relay text it read from the web, an MCP server or a file, so these bytes did not necessarily originate with the sender)",
		body,
		false,
	)
}
