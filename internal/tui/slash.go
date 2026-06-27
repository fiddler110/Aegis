package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/commands"
	"github.com/scottymacleod/aegis/internal/share"
)

// SlashResult describes what a slash command produced for the TUI to render.
type SlashResult struct {
	Output   string            // text to append to the transcript
	IsError  bool              // render in error style
	Quit     bool              // signal the TUI to exit
	Message  string            // if non-empty, send this text to the daemon as a normal message
	Personas []api.PersonaInfo // if non-nil, open the persona picker with these entries

	// ReloadSession asks the TUI to refetch the current session and replay its
	// (possibly truncated) transcript — used after a /rewind that changes the
	// conversation. Output, if set, is shown as a toast rather than appended,
	// since the reload resets the transcript.
	ReloadSession bool
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
		"config":   d.cmdConfig,
		"memory":   d.cmdMemory,
		"remember": d.cmdRemember,
		"skills":   d.cmdSkills,
		"commands": d.cmdCommands,
		"models":   d.cmdModels,
		"session":  d.cmdSession,
		"rewind":   d.cmdRewind,
		"share":    d.cmdShare,
		"quit":     d.cmdQuit,
		"exit":     d.cmdQuit,
	}
	return d
}

// SetSession points the dispatcher at a different session (used when the TUI
// switches sessions via the picker).
func (d *SlashDispatcher) SetSession(id, mode string) {
	d.sessionID = id
	d.mode = mode
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

// Customs returns the cached custom command list, refreshing it once if it
// has not yet been loaded. Used by the inline completion popup and palette.
func (d *SlashDispatcher) Customs() []api.CommandInfo {
	if d.customs == nil {
		d.refreshCustoms()
	}
	return d.customs
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
		{"persona [name]", "Pick persona interactively, or switch directly by name"},
		{"mode <plan|build|auto>", "Switch permission mode"},
		{"clear", "Clear the transcript"},
		{"config", "Interactive configuration wizard"},
		{"memory", "Show saved memories"},
		{"remember <text>", "Save a memory entry"},
		{"skills", "List saved skills"},
		{"commands", "List custom commands"},
		{"models", "Show current model info"},
		{"session [list]", "Show session info or list sessions"},
		{"rewind [n] [scope]", "List or restore checkpoints (code/conversation/both)"},
		{"share [html|md|json]", "Export this session to a shareable transcript file"},
		{"exit", "Exit Aegis"},
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
		return "/persona [name]\n  No args: open an interactive list to pick a persona.\n  With name: switch directly, e.g. /persona security."
	case "mode":
		return "/mode <plan|build|auto>\n  Switch the permission mode for the current session.\n  plan = read-only\n  build = file edits allowed, shell execution requires approval\n  auto  = all capabilities allowed without prompting"
	case "clear":
		return "/clear\n  Clear the conversation transcript (session history is preserved)."
	case "config":
		return "/config\n  Open the interactive configuration wizard to change provider, model, tokens, and think settings.\n  Changes are written to the global config file and take effect on next restart."
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
	case "rewind":
		return "/rewind [n] [code|conversation|both]\n  No args: list checkpoints (rewind points) for this session, newest first.\n  /rewind <n>: restore checkpoint n (both files and conversation by default).\n  Scope: 'code' restores only files, 'conversation' only the transcript, 'both' (default) does both.\n  Each checkpoint is the state just before a user turn; rewinding undoes that turn's file changes and/or messages."
	case "share":
		return "/share [html|md|json]\n  Export this session as a shareable transcript file in the current directory.\n  html (default): a self-contained page with styling and inline images.\n  md: Markdown. json: the raw session.\n  Use `aegis sessions export <id>` for the same from the CLI."
	case "quit", "exit":
		return "/quit\n  Exit Aegis."
	default:
		return "No help available for /" + name
	}
}

func (d *SlashDispatcher) cmdPersona(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	personas, err := d.client.ListPersonas(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to list personas: %v", err), IsError: true}
	}

	if len(args) == 0 {
		// No name given — signal the TUI to open the interactive picker.
		if len(personas) == 0 {
			return SlashResult{Output: "No personas available."}
		}
		return SlashResult{Personas: personas}
	}

	name := strings.ToLower(args[0])
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
	if mode == "auto" {
		return SlashResult{Output: "Switched to auto mode.\n⚠ auto runs all tools — including shell commands — without asking. Unless a container sandbox is configured, commands execute directly on this host."}
	}
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

func (d *SlashDispatcher) cmdRewind(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cps, err := d.client.ListCheckpoints(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to list checkpoints: %v", err), IsError: true}
	}
	if len(cps) == 0 {
		return SlashResult{Output: "No checkpoints yet. One is captured at the start of each turn once you send a message."}
	}

	if len(args) == 0 {
		var b strings.Builder
		b.WriteString("Checkpoints (newest first):\n")
		for i, cp := range cps {
			files := ""
			if cp.FileCount > 0 {
				files = fmt.Sprintf(" · %d file(s)", cp.FileCount)
			}
			label := strings.ReplaceAll(cp.Label, "\n", " ")
			fmt.Fprintf(&b, "  %2d  %s%s\n      %s\n", i+1, cp.CreatedAt.Format("15:04:05"), files, label)
		}
		b.WriteString("\nUse /rewind <n> [code|conversation|both] to restore.")
		return SlashResult{Output: b.String()}
	}

	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > len(cps) {
		return SlashResult{Output: fmt.Sprintf("Invalid checkpoint number %q. Use /rewind to see the list (1–%d).", args[0], len(cps)), IsError: true}
	}
	scope := "both"
	if len(args) > 1 {
		scope = strings.ToLower(args[1])
		if scope != "code" && scope != "conversation" && scope != "both" {
			return SlashResult{Output: "Scope must be 'code', 'conversation', or 'both'.", IsError: true}
		}
	}

	cp := cps[n-1]
	resp, err := d.client.Rewind(ctx, d.sessionID, cp.ID, scope)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Rewind failed: %v", err), IsError: true}
	}

	summary := fmt.Sprintf("Rewound to checkpoint %d (%s)", n, scope)
	switch scope {
	case "code":
		summary += fmt.Sprintf(": restored %d file(s).", resp.FilesRestored)
	case "conversation":
		summary += fmt.Sprintf(": kept %d message(s).", resp.MessagesKept)
	default:
		summary += fmt.Sprintf(": restored %d file(s), kept %d message(s).", resp.FilesRestored, resp.MessagesKept)
	}

	// A code-only rewind leaves the transcript intact; otherwise the
	// conversation changed and the TUI must reload it.
	if scope == "code" {
		return SlashResult{Output: summary}
	}
	return SlashResult{Output: summary, ReloadSession: true}
}

func (d *SlashDispatcher) cmdShare(args []string) SlashResult {
	format := share.FormatHTML
	if len(args) > 0 {
		f, err := share.ParseFormat(args[0])
		if err != nil {
			return SlashResult{Output: err.Error(), IsError: true}
		}
		format = f
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := d.client.GetSession(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load session: %v", err), IsError: true}
	}
	data, err := share.Render(sess, format)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Export failed: %v", err), IsError: true}
	}

	id := d.sessionID
	if len(id) > 8 {
		id = id[:8]
	}
	dir, _ := os.Getwd()
	path := filepath.Join(dir, fmt.Sprintf("aegis-session-%s.%s", id, format.Ext()))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return SlashResult{Output: fmt.Sprintf("Write failed: %v", err), IsError: true}
	}
	return SlashResult{Output: fmt.Sprintf("Exported session → %s", path)}
}

func (d *SlashDispatcher) cmdConfig(_ []string) SlashResult {
	return SlashResult{Output: "\x00wizard"}
}

func (d *SlashDispatcher) cmdQuit(_ []string) SlashResult {
	return SlashResult{Quit: true}
}
