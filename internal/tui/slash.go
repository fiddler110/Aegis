package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/commands"
)

// SlashResult describes what a slash command produced for the TUI to render.
type SlashResult struct {
	Output  string // text to append to the transcript
	IsError bool   // render in error style
	Quit    bool   // signal the TUI to exit
	Message string // if non-empty, send this text to the daemon as a normal message
}

// SlashDispatcher dispatches slash commands to built-in handlers or custom
// command templates.
type SlashDispatcher struct {
	client    *client.Client
	sessionID string
	mode      string
	model     string
	builtins  map[string]func(args []string) SlashResult
	customs   []api.CommandInfo
}

// NewSlashDispatcher creates a dispatcher for the given session.
func NewSlashDispatcher(cl *client.Client, sessionID, mode, model string) *SlashDispatcher {
	d := &SlashDispatcher{
		client:    cl,
		sessionID: sessionID,
		mode:      mode,
		model:     model,
	}
	d.builtins = map[string]func(args []string) SlashResult{
		"help":     d.cmdHelp,
		"persona":  d.cmdPersona,
		"mode":     d.cmdMode,
		"clear":    d.cmdClear,
		"memory":   d.cmdMemory,
		"remember": d.cmdRemember,
		"skills":   d.cmdSkills,
		"commands": d.cmdCommands,
		"models":   d.cmdModels,
		"session":  d.cmdSession,
		"quit":     d.cmdQuit,
		"exit":     d.cmdQuit,
	}
	return d
}

// Dispatch executes a parsed slash command. It checks builtins first, then
// custom commands.
func (d *SlashDispatcher) Dispatch(parsed *commands.ParsedCommand) SlashResult {
	if handler, ok := d.builtins[parsed.Name]; ok {
		return handler(parsed.Args)
	}
	return d.tryCustom(parsed)
}

func (d *SlashDispatcher) tryCustom(parsed *commands.ParsedCommand) SlashResult {
	if d.customs == nil {
		d.refreshCustoms()
	}
	for _, c := range d.customs {
		if c.Name == parsed.Name {
			argText := strings.Join(parsed.Args, " ")
			prompt := c.Description
			if argText != "" {
				prompt = c.Description + "\n\nContext: " + argText
			}
			if prompt == "" {
				prompt = "Execute the /" + parsed.Name + " command"
			}
			return SlashResult{Message: prompt}
		}
	}
	return SlashResult{
		Output:  fmt.Sprintf("Unknown command: /%s\nType /help for available commands.", parsed.Name),
		IsError: true,
	}
}

func (d *SlashDispatcher) refreshCustoms() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmds, err := d.client.ListCommands(ctx)
	if err != nil {
		d.customs = []api.CommandInfo{}
		return
	}
	d.customs = cmds
}

// --- built-in handlers ---

func (d *SlashDispatcher) cmdHelp(args []string) SlashResult {
	if len(args) > 0 {
		name := strings.ToLower(args[0])
		if _, ok := d.builtins[name]; ok {
			return SlashResult{Output: builtinHelp(name)}
		}
		return SlashResult{Output: fmt.Sprintf("Unknown command: /%s", name), IsError: true}
	}

	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, entry := range []struct{ name, desc string }{
		{"help [cmd]", "Show this help or detail for a command"},
		{"persona [name]", "List or switch persona"},
		{"mode <plan|build|auto>", "Switch permission mode"},
		{"clear", "Clear the transcript"},
		{"memory", "Show saved memories"},
		{"remember <text>", "Save a memory entry"},
		{"skills", "List saved skills"},
		{"commands", "List custom commands"},
		{"models", "Show current model info"},
		{"session [list]", "Show session info or list sessions"},
		{"quit", "Exit Aegis"},
	} {
		fmt.Fprintf(&b, "  /%-22s %s\n", entry.name, entry.desc)
	}

	if d.customs == nil {
		d.refreshCustoms()
	}
	if len(d.customs) > 0 {
		b.WriteString("\nCustom commands:\n")
		for _, c := range d.customs {
			argStr := ""
			if len(c.Args) > 0 {
				argStr = " <" + strings.Join(c.Args, "> <") + ">"
			}
			fmt.Fprintf(&b, "  /%-22s %s\n", c.Name+argStr, c.Description)
		}
	}
	return SlashResult{Output: b.String()}
}

func builtinHelp(name string) string {
	switch name {
	case "help":
		return "/help [command]\n  Show available commands, or detailed help for a specific command."
	case "persona":
		return "/persona [name]\n  No args: list available personas.\n  With name: switch to that persona for the current session."
	case "mode":
		return "/mode <plan|build|auto>\n  Switch the permission mode for the current session.\n  plan = read-only\n  build = file edits allowed, shell execution requires approval\n  auto  = all capabilities allowed without prompting"
	case "clear":
		return "/clear\n  Clear the conversation transcript (session history is preserved)."
	case "memory":
		return "/memory\n  Display saved project and user memory entries."
	case "remember":
		return "/remember <text>\n  Save a fact to project memory for future sessions."
	case "skills":
		return "/skills\n  List saved skill files."
	case "commands":
		return "/commands\n  List custom user-defined commands from .aegis/commands/."
	case "models":
		return "/models\n  Show the current model and provider."
	case "session":
		return "/session [list]\n  No args: show current session info.\n  list: show all sessions."
	case "quit", "exit":
		return "/quit\n  Exit Aegis."
	default:
		return "No help available for /" + name
	}
}

func (d *SlashDispatcher) cmdPersona(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(args) == 0 {
		personas, err := d.client.ListPersonas(ctx)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to list personas: %v", err), IsError: true}
		}
		var b strings.Builder
		b.WriteString("Available personas:\n")
		for _, p := range personas {
			fmt.Fprintf(&b, "  %-28s %s\n", p.Name, p.Description)
		}
		b.WriteString("\nUsage: /persona <name>")
		return SlashResult{Output: b.String()}
	}

	name := strings.ToLower(args[0])
	personas, err := d.client.ListPersonas(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to list personas: %v", err), IsError: true}
	}
	var found *api.PersonaInfo
	for _, p := range personas {
		if p.Name == name {
			found = &p
			break
		}
	}
	if found == nil {
		var names []string
		for _, p := range personas {
			names = append(names, p.Name)
		}
		return SlashResult{
			Output:  fmt.Sprintf("Unknown persona %q. Available: %s", name, strings.Join(names, ", ")),
			IsError: true,
		}
	}

	// Look up the full persona system prompt and update the session.
	// The daemon resolves the persona name to a system prompt via the PATCH endpoint.
	// We need to send the persona's system prompt. Since the client doesn't have
	// the full system text, we'll use a convention: send the persona name as the
	// system value prefixed with "persona:" so the daemon can resolve it.
	// Actually, simpler: use the persona package directly from the API.
	// The PATCH endpoint takes a raw system string, so we ask the personas endpoint
	// for the name and then we need the system text. But PersonaInfo doesn't include
	// the full system prompt (intentionally — it's huge).
	//
	// Solution: Add persona resolution in the PATCH handler. For now, send a special
	// marker that the PATCH handler recognizes.
	personaSystem := "persona:" + name
	_, err = d.client.UpdateSession(ctx, d.sessionID, api.UpdateSessionRequest{System: &personaSystem})
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to switch persona: %v", err), IsError: true}
	}

	return SlashResult{Output: fmt.Sprintf("Switched to %s persona: %s", found.Name, found.Description)}
}

func (d *SlashDispatcher) cmdMode(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: fmt.Sprintf("Current mode: %s\nUsage: /mode <plan|build|auto>", d.mode)}
	}
	mode := strings.ToLower(args[0])
	if mode != "plan" && mode != "build" && mode != "auto" {
		return SlashResult{Output: "Mode must be 'plan', 'build', or 'auto'.", IsError: true}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := d.client.UpdateSession(ctx, d.sessionID, api.UpdateSessionRequest{Mode: &mode})
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to switch mode: %v", err), IsError: true}
	}
	d.mode = mode
	return SlashResult{Output: fmt.Sprintf("Switched to %s mode.", mode)}
}

func (d *SlashDispatcher) cmdClear(_ []string) SlashResult {
	return SlashResult{Output: "\x00clear"} // special marker handled by TUI
}

func (d *SlashDispatcher) cmdMemory(_ []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mem, err := d.client.GetMemory(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load memory: %v", err), IsError: true}
	}
	var b strings.Builder
	if mem.ProjectMemory != "" {
		b.WriteString("Project memory:\n" + mem.ProjectMemory + "\n")
	}
	if mem.UserMemory != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("User memory:\n" + mem.UserMemory + "\n")
	}
	if b.Len() == 0 {
		b.WriteString("No memories saved yet. Use /remember <text> to save one.")
	}
	return SlashResult{Output: b.String()}
}

func (d *SlashDispatcher) cmdRemember(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /remember <text to remember>", IsError: true}
	}
	entry := strings.Join(args, " ")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.client.AppendMemory(ctx, api.AppendMemoryRequest{Entry: entry, Scope: "project"}); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to save: %v", err), IsError: true}
	}
	return SlashResult{Output: "Saved to project memory."}
}

func (d *SlashDispatcher) cmdSkills(_ []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mem, err := d.client.GetMemory(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load skills: %v", err), IsError: true}
	}
	if len(mem.Skills) == 0 {
		return SlashResult{Output: "No skills saved yet."}
	}
	var b strings.Builder
	b.WriteString("Saved skills:\n")
	for _, s := range mem.Skills {
		fmt.Fprintf(&b, "  %s\n", s)
	}
	return SlashResult{Output: b.String()}
}

func (d *SlashDispatcher) cmdCommands(_ []string) SlashResult {
	d.customs = nil // force refresh
	d.refreshCustoms()
	if len(d.customs) == 0 {
		return SlashResult{Output: "No custom commands found.\nAdd .md files to .aegis/commands/ to create commands."}
	}
	var b strings.Builder
	b.WriteString("Custom commands:\n")
	for _, c := range d.customs {
		argStr := ""
		if len(c.Args) > 0 {
			argStr = " <" + strings.Join(c.Args, "> <") + ">"
		}
		fmt.Fprintf(&b, "  /%-22s %s\n", c.Name+argStr, c.Description)
	}
	return SlashResult{Output: b.String()}
}

func (d *SlashDispatcher) cmdModels(_ []string) SlashResult {
	return SlashResult{Output: fmt.Sprintf("Model: %s\nMode: %s", d.model, d.mode)}
}

func (d *SlashDispatcher) cmdSession(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(args) > 0 && strings.ToLower(args[0]) == "list" {
		sessions, err := d.client.ListSessions(ctx)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to list sessions: %v", err), IsError: true}
		}
		if len(sessions) == 0 {
			return SlashResult{Output: "No sessions."}
		}
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, s := range sessions {
			marker := "  "
			if s.ID == d.sessionID {
				marker = "* "
			}
			fmt.Fprintf(&b, "%s%-8s  %-6s  %s  %s\n", marker, s.ID[:8], s.Mode, s.UpdatedAt.Format("2006-01-02 15:04"), s.Title)
		}
		return SlashResult{Output: b.String()}
	}

	sess, err := d.client.GetSession(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to get session: %v", err), IsError: true}
	}
	return SlashResult{Output: fmt.Sprintf("Session: %s\nTitle: %s\nMode: %s\nMessages: %d\nCreated: %s",
		sess.ID, sess.Title, sess.Mode, len(sess.Messages), sess.CreatedAt.Format(time.RFC3339))}
}

func (d *SlashDispatcher) cmdQuit(_ []string) SlashResult {
	return SlashResult{Quit: true}
}
